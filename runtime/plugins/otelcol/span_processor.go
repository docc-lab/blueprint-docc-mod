// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// RealTimeSpanProcessor implements OpenTelemetry's SpanProcessor interface
// to send span data to the agent in real-time as spans start and end
// (incomplete spans, not just completed ones)
type RealTimeSpanProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string

	// Agent IP discovery
	aipLock sync.Mutex
	agentIP string

	// HTTP client for IP discovery
	httpClient *http.Client

	// Metrics for monitoring
	startEventsSent   int64
	endEventsSent     int64
	completeSpansSent int64

	startEventsBuf []*tracepb.ResourceSpans
	sebLock        sync.Mutex
	endEventsBuf   []*tracepb.ResourceSpans
	eebLock        sync.Mutex

	// IP Discovery Port
	ipDiscoveryPort int

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// IPResponse represents the response from the IP discovery endpoint
type IPResponse struct {
	IP string `json:"ip"`
}

// NewRealTimeSpanProcessor creates a new span processor that sends span data in real-time
func NewRealTimeSpanProcessor(ctx context.Context, agentEndpoint string, ipDiscoveryPort string) (*RealTimeSpanProcessor, error) {
	slog.Info("🔵 Creating new RealTimeSpanProcessor", "agentEndpoint", agentEndpoint, "ipDiscoveryPort", ipDiscoveryPort)

	// Create OTLP gRPC client using the official method
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(agentEndpoint),
		otlptracegrpc.WithInsecure(),
	)

	slog.Info("🔵 OTLP client created, starting connection")

	// Start the client to establish the connection
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}

	slog.Info("🟢 OTLP client started successfully")

	// Create HTTP client for IP discovery
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	ipDiscoveryPortInt, err := strconv.Atoi(ipDiscoveryPort)
	if err != nil {
		slog.Error("❌ Failed to convert ipDiscoveryPort to int", "error", err)
		return nil, fmt.Errorf("failed to convert ipDiscoveryPort to int: %w", err)
	}

	processor := &RealTimeSpanProcessor{
		client:          client,
		agentEndpoint:   agentEndpoint,
		httpClient:      httpClient,
		ipDiscoveryPort: ipDiscoveryPortInt,
		stopChan:        make(chan struct{}),
	}

	processor.aipLock.Lock()

	slog.Info("🔵 About to fetch agent IP")

	// Fetch agent IP - this is required before the processor can function
	if err := processor.fetchAgentIP(ctx); err != nil {
		slog.Error("❌ Failed to fetch agent IP", "error", err)
		client.Stop(ctx)
		return nil, fmt.Errorf("failed to fetch agent IP: %w", err)
	}

	// Start background goroutine for processing buffered events
	processor.wg.Add(1)
	go processor.processBufferedEvents()

	slog.Info("🟢 RealTimeSpanProcessor created successfully", "agentIP", processor.agentIP)
	return processor, nil
}

// getIPDiscoveryEndpoint converts the agent endpoint to the IP discovery endpoint
func (p *RealTimeSpanProcessor) getIPDiscoveryEndpoint() string {
	// Extract host from agent endpoint
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			host := parts[0]
			// Use configurable port for IP discovery (same host, different port)
			return fmt.Sprintf("%s:%d", host, p.ipDiscoveryPort)
		}
	}
	// Fallback to localhost with configurable port
	return fmt.Sprintf("localhost:%d", p.ipDiscoveryPort)
}

// fetchAgentIP fetches the agent's IP address from the IP discovery endpoint
func (p *RealTimeSpanProcessor) fetchAgentIP(ctx context.Context) error {
	// Try to fetch IP from the discovery endpoint with retries
	ip, err := p.fetchIPFromEndpointWithRetries(ctx)
	if err != nil {
		slog.Warn("Failed to fetch IP from discovery endpoint after retries, trying fallback methods", "error", err)

		// Try fallback methods
		ip, err = p.discoverIPFallback()
		if err != nil {
			return fmt.Errorf("failed to discover agent IP using all methods: %w", err)
		}
	}

	p.agentIP = ip

	slog.Info("Successfully discovered agent IP", "ip", ip)

	p.aipLock.Unlock()

	return nil
}

// fetchIPFromEndpoint fetches IP from the configured discovery endpoint
func (p *RealTimeSpanProcessor) fetchIPFromEndpoint(ctx context.Context) (string, error) {
	ipDiscoveryEndpoint := p.getIPDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getIP", ipDiscoveryEndpoint)

	slog.Info("🔵 Attempting IP discovery", "url", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Error("❌ Failed to create HTTP request", "error", err)
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		slog.Error("❌ Failed to make HTTP request", "error", err)
		return "", fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	slog.Info("🔵 IP discovery response received", "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		slog.Warn("⚠️ IP discovery endpoint returned non-OK status", "status", resp.StatusCode)
		return "", fmt.Errorf("IP discovery endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("❌ Failed to read response body", "error", err)
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var ipResp IPResponse
	if err := json.Unmarshal(body, &ipResp); err != nil {
		slog.Error("❌ Failed to parse IP response", "error", err, "body", string(body))
		return "", fmt.Errorf("failed to parse IP response: %w", err)
	}

	if ipResp.IP == "" {
		slog.Warn("⚠️ Empty IP address in response")
		return "", fmt.Errorf("empty IP address in response")
	}

	slog.Info("🟢 IP discovery successful", "ip", ipResp.IP)
	return ipResp.IP, nil
}

// fetchIPFromEndpointWithRetries fetches IP from the discovery endpoint with retries
func (p *RealTimeSpanProcessor) fetchIPFromEndpointWithRetries(ctx context.Context) (string, error) {
	ipDiscoveryEndpoint := p.getIPDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getIP", ipDiscoveryEndpoint)

	// Retry loop with 1-second intervals
	for attempt := 1; attempt <= 60; attempt++ { // Max 60 attempts (60 seconds)
		slog.Debug("Attempting IP discovery", "attempt", attempt, "endpoint", url)

		// Create a new request for each attempt
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create HTTP request: %w", err)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			slog.Debug("IP discovery attempt failed, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return "", fmt.Errorf("failed to make HTTP request after %d attempts: %w", attempt, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Debug("IP discovery endpoint returned non-OK status, will retry", "attempt", attempt, "status", resp.StatusCode)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return "", fmt.Errorf("IP discovery endpoint returned status %d after %d attempts", resp.StatusCode, attempt)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Debug("Failed to read response body, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return "", fmt.Errorf("failed to read response body after %d attempts: %w", attempt, err)
		}

		var ipResp IPResponse
		if err := json.Unmarshal(body, &ipResp); err != nil {
			slog.Debug("Failed to parse IP response, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return "", fmt.Errorf("failed to parse IP response after %d attempts: %w", attempt, err)
		}

		if ipResp.IP == "" {
			slog.Debug("Empty IP address in response, will retry", "attempt", attempt)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return "", fmt.Errorf("empty IP address in response after %d attempts", attempt)
		}

		// Success! Return the IP
		slog.Info("IP discovery successful", "attempt", attempt, "ip", ipResp.IP)
		return ipResp.IP, nil
	}

	return "", fmt.Errorf("IP discovery failed after 60 attempts")
}

// discoverIPFallback uses fallback methods to discover IP when HTTP endpoint fails
func (p *RealTimeSpanProcessor) discoverIPFallback() (string, error) {
	slog.Info("🔵 Using fallback IP discovery methods")

	// Try Kubernetes environment first
	if podIP := os.Getenv("POD_IP"); podIP != "" {
		slog.Info("🟢 Found POD_IP from environment", "ip", podIP)
		return podIP, nil
	}

	// Try other environment variables
	if hostIP := os.Getenv("HOST_IP"); hostIP != "" {
		slog.Info("🟢 Found HOST_IP from environment", "ip", hostIP)
		return hostIP, nil
	}

	slog.Info("🔵 Trying network interface discovery")

	// Fall back to Go's standard library for Docker/other environments
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		slog.Error("❌ Failed to get interface addresses", "error", err)
		return "", fmt.Errorf("failed to get interface addresses: %w", err)
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				slog.Info("🟢 Found local IP from network interface", "ip", ipnet.IP.String())
				return ipnet.IP.String(), nil
			}
		}
	}

	slog.Error("❌ No local IP found from any method")
	return "", fmt.Errorf("no local IP found")
}

// getAgentIP returns the agent's IP address, with thread-safe access
func (p *RealTimeSpanProcessor) getAgentIP() string {
	p.aipLock.Lock()
	defer p.aipLock.Unlock()

	return p.agentIP
}

// OnStart implements SpanProcessor.OnStart
func (p *RealTimeSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// p.mu.Lock()
	// defer p.mu.Unlock()

	slog.Info("🔵 OnStart called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID(), "span_kind", s.SpanKind())

	// Handle upstream.ip attribute based on span kind
	if s.SpanKind() == trace.SpanKindClient {
		// For client spans: add our collector's IP as upstream.ip for propagation
		s.SetAttributes(attribute.String("__bag.upstream.ip", p.agentIP))
		slog.Info("🔵 Added __bag.upstream.ip to client span", "upstream_ip", p.agentIP, "span_name", s.Name())
	} else if s.SpanKind() == trace.SpanKindServer {
		// For server spans: read upstream.ip from baggage in context
		slog.Info("🔵 Processing upstream.ip insertion for server span", "span_name", s.Name(), "parent_nil", parent == nil)

		if parent != nil {
			slog.Info("🔵 Parent context is not nil, checking for baggage", "span_name", s.Name())

			baggage := backend.GetBaggageFromContext(parent)
			slog.Info("🔵 Retrieved baggage from context", "span_name", s.Name(), "baggage", baggage, "baggage_nil", baggage == nil)

			if baggage != nil {
				if upstreamIP, exists := baggage["upstream.ip"]; exists {
					s.SetAttributes(attribute.String("upstream.ip", upstreamIP))
					slog.Info("🔵 Added upstream.ip to server span from baggage", "upstream_ip", upstreamIP, "span_name", s.Name())
				} else {
					slog.Debug("🔵 No upstream.ip found in baggage for server span", "span_name", s.Name(), "baggage_keys", fmt.Sprintf("%v", baggage))
				}
			} else {
				slog.Debug("🔵 No baggage found in context for server span", "span_name", s.Name())
			}
		} else {
			slog.Debug("🔵 Parent context is nil for server span", "span_name", s.Name())
		}
	}

	// Create a START event span with only start time (no end time)
	startSpan := p.createStartEventSpan(s)

	slog.Info("🔵 About to buffer START event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Buffer the START event instead of sending it directly
	p.sebLock.Lock()
	p.startEventsBuf = append(p.startEventsBuf, startSpan)
	p.sebLock.Unlock()

	slog.Info("🟢 Successfully buffered START event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	p.startEventsSent++
}

// OnEnd implements SpanProcessor.OnEnd
func (p *RealTimeSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// p.mu.Lock()
	// defer p.mu.Unlock()

	slog.Info("🔴 OnEnd called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Create an END event span with only trace ID, span ID, and end time
	endSpan := p.createEndEventSpan(s)

	slog.Info("🔴 About to buffer END event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Buffer the END event instead of sending it directly
	p.eebLock.Lock()
	p.endEventsBuf = append(p.endEventsBuf, endSpan)
	p.eebLock.Unlock()

	slog.Info("🟢 Successfully buffered END event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	p.endEventsSent++
}

// createStartEventSpan creates a protobuf span for START events
// Includes all span details but with zero end time
func (p *RealTimeSpanProcessor) createStartEventSpan(s sdktrace.ReadWriteSpan) *tracepb.ResourceSpans {
	// Convert span attributes to protobuf
	spanAttrs := s.Attributes()
	attrsProto := make([]*commonpb.KeyValue, len(spanAttrs))
	for i, attr := range spanAttrs {
		attrsProto[i] = &commonpb.KeyValue{
			Key:   string(attr.Key),
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: attr.Value.AsString()}},
		}
	}

	// upstream.ip attribute is now handled in OnStart() method based on span kind

	// Get trace and span IDs as byte arrays
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

	// Create span with start time but no end time (zero time)
	spanProto := &tracepb.Span{
		TraceId:           traceID[:], // Convert [16]byte to []byte
		SpanId:            spanID[:],  // Convert [8]byte to []byte
		Name:              s.Name(),
		Kind:              tracepb.Span_SpanKind(s.SpanKind()),
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   0, // Zero time for START event
		Attributes:        attrsProto,
		Status: &tracepb.Status{
			Code:    tracepb.Status_StatusCode(s.Status().Code),
			Message: s.Status().Description,
		},
	}

	// Add parent span ID if exists
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:] // Convert [8]byte to []byte
	}

	// Get resource from the span and convert to protobuf format
	resourceProto := p.convertResourceToProto(s.Resource())

	return &tracepb.ResourceSpans{
		Resource: resourceProto,
		ScopeSpans: []*tracepb.ScopeSpans{
			{
				Scope: &commonpb.InstrumentationScope{
					Name:    s.InstrumentationScope().Name,
					Version: s.InstrumentationScope().Version,
				},
				Spans: []*tracepb.Span{spanProto},
			},
		},
	}
}

// createEndEventSpan creates a protobuf span for END events
// Only includes trace ID, span ID, and end time as per AI summary
func (p *RealTimeSpanProcessor) createEndEventSpan(s sdktrace.ReadOnlySpan) *tracepb.ResourceSpans {
	// Get trace and span IDs as byte arrays
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

	// Create span with ONLY trace ID, span ID, and end time (nothing else)
	spanProto := &tracepb.Span{
		TraceId:         traceID[:], // Convert [16]byte to []byte
		SpanId:          spanID[:],  // Convert [8]byte to []byte
		EndTimeUnixNano: uint64(s.EndTime().UnixNano()),
		// No other fields - only trace ID, span ID, and end time as per AI summary
	}

	// Get resource from the span and convert to protobuf format
	resourceProto := p.convertResourceToProto(s.Resource())

	return &tracepb.ResourceSpans{
		Resource: resourceProto,
		ScopeSpans: []*tracepb.ScopeSpans{
			{
				Scope: &commonpb.InstrumentationScope{
					Name:    s.InstrumentationScope().Name,
					Version: s.InstrumentationScope().Version,
				},
				Spans: []*tracepb.Span{spanProto},
			},
		},
	}
}

// convertResourceToProto converts an OpenTelemetry resource to protobuf format
// using the same approach as the official OTLP exporter, but with service name fixing
func (p *RealTimeSpanProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
	if resource == nil {
		return &resourcepb.Resource{}
	}

	// Try to get the resource's iterator
	var iter attribute.Iterator
	if r, ok := resource.(interface{ Iter() attribute.Iterator }); ok {
		iter = r.Iter()
	} else {
		// Fallback to empty resource
		return &resourcepb.Resource{}
	}

	// Convert attributes using the iterator
	attrs := p.convertAttributeIterator(iter)

	// Fix service name if it contains "unknown_service:" prefix
	attrs = p.fixServiceName(attrs)

	return &resourcepb.Resource{
		Attributes: attrs,
	}
}

// fixServiceName replaces "unknown_service:" prefix with proper service names
func (p *RealTimeSpanProcessor) fixServiceName(attrs []*commonpb.KeyValue) []*commonpb.KeyValue {
	for i, attr := range attrs {
		if attr.Key == "service.name" {
			serviceName := attr.Value.GetStringValue()
			if strings.HasPrefix(serviceName, "unknown_service:") {
				// Extract the actual service name from the instrumentation scope
				// The format is typically "unknown_service:service_name_proc"
				parts := strings.SplitN(serviceName, ":", 2)
				if len(parts) == 2 {
					// Remove "_proc" suffix if present (Blueprint convention)
					cleanName := strings.TrimSuffix(parts[1], "_proc")
					attrs[i] = &commonpb.KeyValue{
						Key:   "service.name",
						Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: cleanName}},
					}
				}
			}
		}
	}
	return attrs
}

// convertAttributeIterator converts an attribute iterator to protobuf format
func (p *RealTimeSpanProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
	if iter.Len() == 0 {
		return nil
	}

	attrs := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attr := iter.Attribute()
		attrs = append(attrs, p.convertAttribute(attr))
	}
	return attrs
}

// convertAttribute converts a single attribute to protobuf format
func (p *RealTimeSpanProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *RealTimeSpanProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
	av := new(commonpb.AnyValue)
	switch v.Type() {
	case attribute.STRING:
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: v.AsString(),
		}
	case attribute.INT64:
		av.Value = &commonpb.AnyValue_IntValue{
			IntValue: v.AsInt64(),
		}
	case attribute.FLOAT64:
		av.Value = &commonpb.AnyValue_DoubleValue{
			DoubleValue: v.AsFloat64(),
		}
	case attribute.BOOL:
		av.Value = &commonpb.AnyValue_BoolValue{
			BoolValue: v.AsBool(),
		}
	default:
		// For any other type, convert to string
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: v.AsString(),
		}
	}
	return av
}

// sendSpanData sends span data via the OTLP client
func (p *RealTimeSpanProcessor) sendSpanData(spans []*tracepb.ResourceSpans) error {
	slog.Info("🔵 About to send span data", "num_spans", len(spans))
	err := p.client.UploadTraces(context.Background(), spans)
	slog.Info("🟢 Span data sent successfully", "num_spans", len(spans))
	return err
}

// Shutdown implements SpanProcessor.Shutdown
func (p *RealTimeSpanProcessor) Shutdown(ctx context.Context) error {
	// p.mu.Lock()
	// defer p.mu.Unlock()

	// Stop the background goroutine
	close(p.stopChan)
	p.wg.Wait()

	if p.client != nil {
		if err := p.client.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop OTLP client: %w", err)
		}
	}

	slog.Info("🟢 RealTimeSpanProcessor shutdown complete",
		"start_events", p.startEventsSent,
		"end_events", p.endEventsSent,
		"complete_spans", p.completeSpansSent)
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *RealTimeSpanProcessor) ForceFlush(ctx context.Context) error {
	// p.mu.Lock()
	// defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}

// GetStats returns statistics about the processor
func (p *RealTimeSpanProcessor) GetStats() map[string]int64 {
	// p.mu.RLock()
	// defer p.mu.RUnlock()

	return map[string]int64{
		"start_events_sent":   p.startEventsSent,
		"end_events_sent":     p.endEventsSent,
		"complete_spans_sent": p.completeSpansSent,
	}
}

// processBufferedEvents runs in the background to periodically send buffered events
func (p *RealTimeSpanProcessor) processBufferedEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Send every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Send any remaining buffered events before shutting down
			p.flushAllBuffers()
			return
		case <-ticker.C:
			// Send buffered events
			go p.flushAllBuffers()
		}
	}
}

// flushAllBuffers sends all buffered start and end events
func (p *RealTimeSpanProcessor) flushAllBuffers() {
	// Get events from buffers and reset the buffers
	p.sebLock.Lock()
	p.eebLock.Lock()
	startEvents := p.startEventsBuf
	p.startEventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.startEventsBuf)) // Reset length, keep capacity
	endEvents := p.endEventsBuf
	p.endEventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.endEventsBuf)) // Reset length, keep capacity
	p.eebLock.Unlock()
	p.sebLock.Unlock()

	allEvents := append(startEvents, endEvents...)

	if len(allEvents) > 0 {
		if err := p.sendSpanData(allEvents); err != nil {
			slog.Error("Failed to send buffered events", "error", err, "count", len(allEvents))
		} else {
			slog.Debug("Successfully sent buffered events", "count", len(allEvents))
		}
	}
}
