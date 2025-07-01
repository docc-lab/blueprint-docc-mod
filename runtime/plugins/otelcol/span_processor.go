// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
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

	// Metrics for monitoring
	startEventsSent   int64
	endEventsSent     int64
	completeSpansSent int64
}

// NewRealTimeSpanProcessor creates a new span processor that sends span data in real-time
func NewRealTimeSpanProcessor(ctx context.Context, agentEndpoint string) (*RealTimeSpanProcessor, error) {
	// Create OTLP gRPC client using the official method
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(agentEndpoint),
		otlptracegrpc.WithInsecure(),
	)

	// Start the client to establish the connection
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}

	return &RealTimeSpanProcessor{
		client:        client,
		agentEndpoint: agentEndpoint,
	}, nil
}

// OnStart implements SpanProcessor.OnStart
func (p *RealTimeSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create a START event span with only start time (no end time)
	startSpan := p.createStartEventSpan(s)

	// Send the START event
	if err := p.sendSpanData([]*tracepb.ResourceSpans{startSpan}); err != nil {
		slog.Error("Failed to send START event", "error", err, "span_name", s.Name())
		return
	}

	p.startEventsSent++
	slog.Debug("🟢 Sent START event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())
}

// OnEnd implements SpanProcessor.OnEnd
func (p *RealTimeSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create an END event span with only trace ID, span ID, and end time
	endSpan := p.createEndEventSpan(s)

	// Send the END event
	if err := p.sendSpanData([]*tracepb.ResourceSpans{endSpan}); err != nil {
		slog.Error("Failed to send END event", "error", err, "span_name", s.Name())
		return
	}

	p.endEventsSent++
	slog.Debug("🟢 Sent END event", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())
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
	return p.client.UploadTraces(context.Background(), spans)
}

// Shutdown implements SpanProcessor.Shutdown
func (p *RealTimeSpanProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}

// GetStats returns statistics about the processor
func (p *RealTimeSpanProcessor) GetStats() map[string]int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]int64{
		"start_events_sent":   p.startEventsSent,
		"end_events_sent":     p.endEventsSent,
		"complete_spans_sent": p.completeSpansSent,
	}
}
