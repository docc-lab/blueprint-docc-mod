// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/rand"
    "os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom"
	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Constants for baggage keys
const (
	BAG_BLOOM_FILTER = "__bag.bloom_filter"
)

// Ancestry tagging keys
const (
    AncestryModeKey = "ancestry_mode"
    AncestryKey     = "ancestry"
)

// AncestryMode selects the ancestry encoding strategy
type AncestryMode string

const (
    AncestryModeBloom  AncestryMode = "bloom"
    AncestryModeHash   AncestryMode = "hash"
    AncestryModeHybrid AncestryMode = "hybrid"
)

// Manual toggle: when true, both high and low priority spans are exported
// via the high-priority OTLP client (single channel). When false, low
// priority spans use a separate OTLP client/endpoint.
const singleOTLPClient = true

type PriorityProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	highPrioClient otlptrace.Client
	lowPrioClient  otlptrace.Client

	// Configuration
	agentEndpoint string
    ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Metrics for monitoring
	highPrioEventsSent int64
	lowPrioEventsSent  int64

	// Buffers for batch export
	highPrioEventsBuf []*tracepb.ResourceSpans
	hepLock           sync.Mutex
	lowPrioEventsBuf  []*tracepb.ResourceSpans
	lepLock           sync.Mutex
}

// Darby: this gets run once per service (when initialized)
func NewPriorityProcessor(ctx context.Context, agentEndpoint string) (*PriorityProcessor, error) {
	slog.Info("🔵 Creating new PriorityProcessor", "agentEndpoint", agentEndpoint)

	bloomFilter := bloom.New(10, 7)

	// Extract host from agent endpoint
	var host string
	if strings.Contains(agentEndpoint, ":") {
		parts := strings.Split(agentEndpoint, ":")
		if len(parts) >= 2 {
			host = parts[0]
		}
	}
	if host == "" {
		host = "localhost" // Fallback to localhost
	}

	// Create endpoints with different ports
	highPrioEndpoint := fmt.Sprintf("%s:4317", host)
	lowPrioEndpoint := fmt.Sprintf("%s:4319", host)

	slog.Info("🔵 Using priority endpoints", "highPrio", highPrioEndpoint, "lowPrio", lowPrioEndpoint)

	// Create high priority OTLP gRPC client
	highPrioClient := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(highPrioEndpoint),
		otlptracegrpc.WithInsecure(),
	)

	// Optionally create a separate low-priority client
	var lowPrioClient otlptrace.Client
	if !singleOTLPClient {
		lowPrioClient = otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(lowPrioEndpoint),
			otlptracegrpc.WithInsecure(),
		)
	}

	slog.Info("🔵 OTLP clients created, starting connections", "singleOTLPClient", singleOTLPClient)

	// Start the low priority client if using separate client
	if !singleOTLPClient {
		if err := lowPrioClient.Start(ctx); err != nil {
			slog.Error("❌ Failed to start low priority OTLP client", "error", err)
			return nil, fmt.Errorf("failed to start low priority OTLP client: %w", err)
		}
	}

	// Start the high priority client (always)
	if err := highPrioClient.Start(ctx); err != nil {
		slog.Error("❌ Failed to start high priority OTLP client", "error", err)
		// Clean up the low priority client if high priority fails
		if !singleOTLPClient && lowPrioClient != nil {
			lowPrioClient.Stop(ctx)
		}
		return nil, fmt.Errorf("failed to start high priority OTLP client: %w", err)
	}

	slog.Info("✅ PriorityProcessor created successfully")

    // Resolve ancestry mode from environment (default: bloom)
    mode := AncestryMode(os.Getenv("ANCESTRY_MODE"))
    if mode == "" {
        mode = AncestryModeBloom
    }

    processor := &PriorityProcessor{
		lowPrioClient:  lowPrioClient,
		highPrioClient: highPrioClient,
		agentEndpoint:  agentEndpoint,
		bloomFilter:    bloomFilter,
		stopChan:       make(chan struct{}),
        ancestryMode:   mode,
	}

    slog.Info("🔵 Ancestry mode configured", "mode", mode)

	// Start background workers for batch export
	processor.wg.Add(2)
	go processor.processHighPriorityEvents()
	go processor.processLowPriorityEvents()

	return processor, nil
}

// processHighPriorityEvents runs in the background to periodically send high priority events
func (p *PriorityProcessor) processHighPriorityEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Send every 50ms for high priority
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Send any remaining buffered events before shutting down
			p.flushHighPriorityBuffer()
			return
		case <-ticker.C:
			// Send buffered events
			go p.flushHighPriorityBuffer()
		}
	}
}

// processLowPriorityEvents runs in the background to periodically send low priority events
func (p *PriorityProcessor) processLowPriorityEvents() {
	defer p.wg.Done()

	slog.Info("🔴 Low priority worker started")
	ticker := time.NewTicker(100 * time.Millisecond) // Send every 100ms for low priority
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			slog.Info("🔴 Low priority worker stopping, flushing remaining events")
			// Send any remaining buffered events before shutting down
			p.flushLowPriorityBuffer()
			slog.Info("🔴 Low priority worker stopped")
			return
		case <-ticker.C:
			// Send buffered events
			go p.flushLowPriorityBuffer()
		}
	}
}

// flushHighPriorityBuffer sends all buffered high priority events
func (p *PriorityProcessor) flushHighPriorityBuffer() {
	// Get events from buffer and reset the buffer
	p.hepLock.Lock()
	events := p.highPrioEventsBuf
	p.highPrioEventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.highPrioEventsBuf)) // Reset length, keep capacity
	p.hepLock.Unlock()

	if len(events) > 0 {
		if err := p.sendHighPriorityData(events); err != nil {
			slog.Error("Failed to send high priority events", "error", err, "count", len(events))
		} else {
			slog.Debug("Successfully sent high priority events", "count", len(events))
			p.highPrioEventsSent += int64(len(events))
		}
	}
}

// flushLowPriorityBuffer sends all buffered low priority events
func (p *PriorityProcessor) flushLowPriorityBuffer() {
	// Get events from buffer and reset the buffer
	p.lepLock.Lock()
	events := p.lowPrioEventsBuf
	p.lowPrioEventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.lowPrioEventsBuf)) // Reset length, keep capacity
	p.lepLock.Unlock()

	if len(events) > 0 {
		slog.Info("🔴 Flushing low priority buffer", "count", len(events))
		if err := p.sendLowPriorityData(events); err != nil {
			slog.Error("❌ Failed to send low priority events", "error", err, "count", len(events))
		} else {
			slog.Info("✅ Successfully sent low priority events", "count", len(events))
			p.lowPrioEventsSent += int64(len(events))
		}
	} else {
		slog.Debug("🔴 Low priority buffer empty, nothing to flush")
	}
}

// sendHighPriorityData sends data to the high priority endpoint
func (p *PriorityProcessor) sendHighPriorityData(events []*tracepb.ResourceSpans) error {
	if len(events) == 0 {
		return nil
	}

	slog.Debug("🔵 Sending high priority data", "count", len(events))

	// Create a context with timeout for high priority (shorter timeout for faster processing)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := p.highPrioClient.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ Failed to send high priority data", "error", err, "count", len(events))
		return fmt.Errorf("failed to send high priority data: %w", err)
	}

	slog.Debug("✅ High priority data sent successfully", "count", len(events))
	return nil
}

// sendLowPriorityData sends data to the low priority endpoint
func (p *PriorityProcessor) sendLowPriorityData(events []*tracepb.ResourceSpans) error {
	if len(events) == 0 {
		slog.Debug("🔴 No low priority events to send")
		return nil
	}

	slog.Info("🔴 Sending low priority data", "count", len(events))

	// Create a context with timeout for low priority (longer timeout for reliability)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Choose client based on mode
	client := p.lowPrioClient
	endpointLabel := "4319"
	if singleOTLPClient {
		client = p.highPrioClient
		endpointLabel = "4317"
	}

	slog.Debug("🔴 Calling UploadTraces for low priority", "count", len(events), "endpoint", endpointLabel)
	err := client.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ UploadTraces failed for low priority data", "error", err, "count", len(events))
		return fmt.Errorf("failed to send low priority data: %w", err)
	}

	slog.Info("✅ Low priority data sent successfully", "count", len(events))
	return nil
}

// OnStart implements SpanProcessor.OnStart
func (p *PriorityProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔵 PriorityProcessor OnStart called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Handle depth tracking in baggage
	depth := 0
	baggage := backend.GetBaggageFromContext(parent)
	if baggage != nil {
		if depthStr, exists := baggage["depth"]; exists {
			if depthInt, err := strconv.Atoi(depthStr); err == nil {
				depth = depthInt + 1
			}
		}
	}
	s.SetAttributes(attribute.Int("__bag.depth", depth))

	// Handle bloom filter in baggage (same pattern as depth)
	var bloomFilter *bloom.BloomFilter
	var err error

	// Check if bloom filter exists in baggage (same as depth)
	if baggage != nil {
		if bfStr, exists := baggage["bloom_filter"]; exists {
			// Deserialize existing bloom filter (like depth parsing)
			bloomFilter, err = deserializeBloomFilter(bfStr)
			if err != nil {
				slog.Warn("Failed to deserialize existing bloom filter, creating new one", "error", err)
				bloomFilter = createEmptyBloomFilter()
			}
		} else {
			// Create new bloom filter if none exists (like depth = 0)
			bloomFilter = createEmptyBloomFilter()
		}
	} else {
		// Create new bloom filter if no baggage (like depth = 0)
		bloomFilter = createEmptyBloomFilter()
	}

	// Add current span ID to bloom filter (like depth + 1)
	spanID := s.SpanContext().SpanID().String()
	bloomFilter.Add([]byte(spanID))

	parentSpan := trace.SpanFromContext(parent)
	isRoot := !parentSpan.SpanContext().IsValid()
	slog.Debug("🔵 Updated bloom filter for span", "span_id", spanID, "is_root", isRoot)

    // Serialize updated bloom filter and set baggage attribute for propagation
    bfStr, err := serializeBloomFilter(bloomFilter)
    if err != nil {
        slog.Error("Failed to serialize bloom filter", "error", err)
        return
    }
    s.SetAttributes(attribute.String("__bag.bloom_filter", bfStr))

	// Randomly assign priority (high=1, low=0) for now
	priority := rand.Intn(2) // 0 or 1

	// Set priority as baggage attribute for propagation
	s.SetAttributes(attribute.Int("__bag.prio", priority))

    // Write ancestry tags (only ancestry_mode and ancestry payload)
    ancestryPayload := ""
    switch p.ancestryMode {
    case AncestryModeBloom:
        ancestryPayload = bfStr
    case AncestryModeHash:
        // TODO: implement hash array encoder later
        ancestryPayload = ""
    case AncestryModeHybrid:
        // TODO: implement hybrid encoder later
        ancestryPayload = ""
    default:
        ancestryPayload = bfStr
    }

	// Add priority attribute for verification (not baggage)
	// only add attributes to span for high priority spans.
    if priority == 1 {
        s.SetAttributes(attribute.String("prio", "high"))
        s.SetAttributes(attribute.String(AncestryModeKey, string(p.ancestryMode)))
        s.SetAttributes(attribute.String(AncestryKey, ancestryPayload))
    } else {
        s.SetAttributes(attribute.String("prio", "low"))
    }

    // Add depth attribute for verification (not baggage)
    s.SetAttributes(attribute.Int("depth", depth))


	slog.Info("🔵 Set priority baggage and attribute", "priority", priority, "depth", depth, "span_name", s.Name())
}

// OnEnd implements SpanProcessor.OnEnd
func (p *PriorityProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 PriorityProcessor OnEnd called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Extract priority from span attributes
	var priority int
	var hasPriority bool
	var depth int
	var hasDepth bool

	// Iterate through attributes to find __bag.prio
	for _, attr := range s.Attributes() {
		if attr.Key == "__bag.prio" {
			val := attr.Value.AsInt64()
			priority = int(val)
			hasPriority = true
		} else if attr.Key == "__bag.depth" {
			val := attr.Value.AsInt64()
			depth = int(val)
			hasDepth = true
		}
	}

	if !hasPriority {
		// Default to low priority if no priority found
		priority = 0
		slog.Warn("🔴 No priority found, defaulting to low priority", "span_name", s.Name())
	}
	if !hasDepth {
		depth = 0
		slog.Warn("🔴 No depth found, defaulting to 0", "span_name", s.Name())
	}

	slog.Info("🔴 Routing span based on priority",
		"priority", priority,
		"depth", depth,
		"span_name", s.Name(),
		"trace_id", s.SpanContext().TraceID(),
		"span_id", s.SpanContext().SpanID())

	// Route span to appropriate pipeline based on priority
	if priority == 1 {
		// High priority - add to high priority buffer
		p.routeToHighPriorityPipeline(s)
	} else {
		// Low priority - add to low priority buffer
		p.routeToLowPriorityPipeline(s)
	}

	// Note: All baggage attributes (including __bag.prio and __bag.bloom_filter)
	// are now exported as span attributes for analysis and debugging.
}

// routeToHighPriorityPipeline adds the span to the high priority buffer
func (p *PriorityProcessor) routeToHighPriorityPipeline(s sdktrace.ReadOnlySpan) {
	// Convert span to ResourceSpans and add to high priority buffer
	resourceSpans := p.createResourceSpans(s)

	p.hepLock.Lock()
	p.highPrioEventsBuf = append(p.highPrioEventsBuf, resourceSpans)
	p.hepLock.Unlock()

	slog.Debug("🔴 Routed to high priority pipeline", "span_name", s.Name(), "buffer_size", len(p.highPrioEventsBuf))
}

// routeToLowPriorityPipeline adds the span to the low priority buffer
func (p *PriorityProcessor) routeToLowPriorityPipeline(s sdktrace.ReadOnlySpan) {
	// Convert span to ResourceSpans and add to low priority buffer
	resourceSpans := p.createResourceSpans(s)

	p.lepLock.Lock()
	p.lowPrioEventsBuf = append(p.lowPrioEventsBuf, resourceSpans)
	bufferSize := len(p.lowPrioEventsBuf)
	p.lepLock.Unlock()

	slog.Info("🔴 Routed to low priority pipeline",
		"span_name", s.Name(),
		"trace_id", s.SpanContext().TraceID(),
		"span_id", s.SpanContext().SpanID(),
		"buffer_size", bufferSize)
}

// createResourceSpans converts a ReadOnlySpan to ResourceSpans protobuf format
func (p *PriorityProcessor) createResourceSpans(s sdktrace.ReadOnlySpan) *tracepb.ResourceSpans {
	// Get trace and span IDs as byte arrays
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

	// Create span with trace ID, span ID, start time, end time, and attributes
	spanProto := &tracepb.Span{
		TraceId:           traceID[:], // Convert [16]byte to []byte
		SpanId:            spanID[:],  // Convert [8]byte to []byte
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(s.EndTime().UnixNano()),
		Name:              s.Name(),
		Kind:              p.convertSpanKind(s.SpanKind()),
		Status:            p.convertStatus(s.Status()),
		Attributes:        p.convertAttributes(s.Attributes()),
		Events:            p.convertEvents(s.Events()),
		Links:             p.convertLinks(s.Links()),
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

// convertResourceToProto converts an OpenTelemetry resource to protobuf format
func (p *PriorityProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
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

	return &resourcepb.Resource{
		Attributes: attrs,
	}
}

// convertAttributeIterator converts an attribute iterator to protobuf format
func (p *PriorityProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
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
func (p *PriorityProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *PriorityProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
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
	case attribute.BOOLSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertBoolSlice(v.AsBoolSlice()),
			},
		}
	case attribute.INT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertInt64Slice(v.AsInt64Slice()),
			},
		}
	case attribute.FLOAT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertFloat64Slice(v.AsFloat64Slice()),
			},
		}
	case attribute.STRINGSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertStringSlice(v.AsStringSlice()),
			},
		}
	default:
		// Fallback to string representation
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: fmt.Sprintf("%v", v.AsInterface()),
		}
	}
	return av
}

// Helper functions for slice conversions
func (p *PriorityProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: v},
		}
	}
	return values
}

func (p *PriorityProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: v},
		}
	}
	return values
}

func (p *PriorityProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v},
		}
	}
	return values
}

func (p *PriorityProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		}
	}
	return values
}

// serializeBloomFilter converts a bloom filter to a base64-encoded string
func serializeBloomFilter(bf *bloom.BloomFilter) (string, error) {
	data, err := bf.GobEncode()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// deserializeBloomFilter converts a base64-encoded string back to a bloom filter
func deserializeBloomFilter(serialized string) (*bloom.BloomFilter, error) {
	data, err := base64.StdEncoding.DecodeString(serialized)
	if err != nil {
		return nil, err
	}

	bf := &bloom.BloomFilter{}
	err = bf.GobDecode(data)
	if err != nil {
		return nil, err
	}

	return bf, nil
}

// createEmptyBloomFilter creates a new empty bloom filter
func createEmptyBloomFilter() *bloom.BloomFilter {
	return bloom.New(10, 7) // Same parameters as existing
}

// getBaggageKeys returns the keys from a baggage map for logging
func getBaggageKeys(baggage map[string]string) []string {
	keys := make([]string, 0, len(baggage))
	for k := range baggage {
		keys = append(keys, k)
	}
	return keys
}

// convertSpanKind converts OpenTelemetry span kind to protobuf format
func (p *PriorityProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindInternal:
		return tracepb.Span_SPAN_KIND_INTERNAL
	case trace.SpanKindServer:
		return tracepb.Span_SPAN_KIND_SERVER
	case trace.SpanKindClient:
		return tracepb.Span_SPAN_KIND_CLIENT
	case trace.SpanKindProducer:
		return tracepb.Span_SPAN_KIND_PRODUCER
	case trace.SpanKindConsumer:
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

// convertStatus converts OpenTelemetry span status to protobuf format
func (p *PriorityProcessor) convertStatus(status sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{
		Code:    tracepb.Status_StatusCode(status.Code),
		Message: status.Description,
	}
}

// convertAttributes converts span attributes to protobuf format, including all baggage attributes
func (p *PriorityProcessor) convertAttributes(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	// Include all attributes (including baggage attributes)
	protoAttrs := make([]*commonpb.KeyValue, len(attrs))
	for i, attr := range attrs {
		protoAttrs[i] = p.convertAttribute(attr)
	}
	return protoAttrs
}

// convertEvents converts span events to protobuf format
func (p *PriorityProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	protoEvents := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		protoEvents[i] = &tracepb.Span_Event{
			TimeUnixNano: uint64(event.Time.UnixNano()),
			Name:         event.Name,
			Attributes:   p.convertAttributes(event.Attributes), // This will include all attributes
		}
	}
	return protoEvents
}

// convertLinks converts span links to protobuf format
func (p *PriorityProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
	if len(links) == 0 {
		return nil
	}

	protoLinks := make([]*tracepb.Span_Link, len(links))
	for i, link := range links {
		traceID := link.SpanContext.TraceID()
		spanID := link.SpanContext.SpanID()

		protoLinks[i] = &tracepb.Span_Link{
			TraceId:    traceID[:],
			SpanId:     spanID[:],
			Attributes: p.convertAttributes(link.Attributes), // This will include all attributes
		}
	}
	return protoLinks
}

// Shutdown implements SpanProcessor.Shutdown
func (p *PriorityProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 PriorityProcessor shutting down")

	// Stop the background workers
	close(p.stopChan)
	p.wg.Wait()

	// Stop the high priority client
	if err := p.highPrioClient.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop high priority client", "error", err)
	}

	// Stop the low priority client
	if !singleOTLPClient && p.lowPrioClient != nil {
		if err := p.lowPrioClient.Stop(ctx); err != nil {
			slog.Error("❌ Failed to stop low priority client", "error", err)
		}
	}

	slog.Info("✅ PriorityProcessor shutdown complete",
		"highPrioEventsSent", p.highPrioEventsSent,
		"lowPrioEventsSent", p.lowPrioEventsSent)
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *PriorityProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
