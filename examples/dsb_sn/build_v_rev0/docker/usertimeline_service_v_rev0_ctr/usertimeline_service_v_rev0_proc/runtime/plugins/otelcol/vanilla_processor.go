// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"
	"log/slog"
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
)

type VanillaProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Metrics for monitoring
	eventsSent int64

	// Buffer for batch export
	eventsBuf  []*tracepb.ResourceSpans
	eventsLock sync.Mutex
}

// Darby: this gets run once per service (when initialized)
func NewVanillaProcessor(ctx context.Context, agentEndpoint string, additionalPort string) (*VanillaProcessor, error) {
	slog.Info("🔵 Creating new VanillaProcessor", "agentEndpoint", agentEndpoint)

	// Create OTLP gRPC client
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(agentEndpoint),
		otlptracegrpc.WithInsecure(),
	)

	slog.Info("🔵 OTLP client created, starting connection")

	// Start the client
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}

	slog.Info("✅ VanillaProcessor created successfully")

	processor := &VanillaProcessor{
		client:        client,
		agentEndpoint: agentEndpoint,
		stopChan:      make(chan struct{}),
	}

	// Start background worker for batch export
	processor.wg.Add(1)
	go processor.processEvents()

	return processor, nil
}

// processEvents runs in the background to periodically send events
func (p *VanillaProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Send any remaining buffered events before shutting down
			p.flushBuffer()
			return
		case <-ticker.C:
			// Send buffered events
			go p.flushBuffer()
		}
	}
}

// flushBuffer sends all buffered events
func (p *VanillaProcessor) flushBuffer() {
	// Get events from buffer and reset the buffer
	p.eventsLock.Lock()
	events := p.eventsBuf
	p.eventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.eventsBuf)) // Reset length, keep capacity
	p.eventsLock.Unlock()

	if len(events) > 0 {
		if err := p.sendData(events); err != nil {
			slog.Error("Failed to send events", "error", err, "count", len(events))
		} else {
			slog.Debug("Successfully sent events", "count", len(events))
			p.eventsSent += int64(len(events))
		}
	}
}

// sendData sends data to the OTLP endpoint
func (p *VanillaProcessor) sendData(events []*tracepb.ResourceSpans) error {
	if len(events) == 0 {
		return nil
	}

	slog.Debug("🔵 Sending data", "count", len(events))

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := p.client.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ Failed to send data", "error", err, "count", len(events))
		return fmt.Errorf("failed to send data: %w", err)
	}

	slog.Debug("✅ Data sent successfully", "count", len(events))
	return nil
}

// OnStart implements SpanProcessor.OnStart
func (p *VanillaProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// Do nothing
}

// OnEnd implements SpanProcessor.OnEnd
func (p *VanillaProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// Simply enqueue the span to the buffer
	p.routeToPipeline(s)
}

// routeToPipeline adds the span to the buffer
func (p *VanillaProcessor) routeToPipeline(s sdktrace.ReadOnlySpan) {
	// Convert span to ResourceSpans and add to buffer
	resourceSpans := p.createResourceSpans(s)

	p.eventsLock.Lock()
	p.eventsBuf = append(p.eventsBuf, resourceSpans)
	p.eventsLock.Unlock()
}

// createResourceSpans converts a ReadOnlySpan to ResourceSpans protobuf format
func (p *VanillaProcessor) createResourceSpans(s sdktrace.ReadOnlySpan) *tracepb.ResourceSpans {
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
func (p *VanillaProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
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
func (p *VanillaProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
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
func (p *VanillaProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *VanillaProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
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
func (p *VanillaProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: v},
		}
	}
	return values
}

func (p *VanillaProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: v},
		}
	}
	return values
}

func (p *VanillaProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v},
		}
	}
	return values
}

func (p *VanillaProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		}
	}
	return values
}

// convertSpanKind converts OpenTelemetry span kind to protobuf format
func (p *VanillaProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
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
func (p *VanillaProcessor) convertStatus(status sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{
		Code:    tracepb.Status_StatusCode(status.Code),
		Message: status.Description,
	}
}

// convertAttributes converts span attributes to protobuf format
func (p *VanillaProcessor) convertAttributes(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	protoAttrs := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		protoAttrs = append(protoAttrs, p.convertAttribute(attr))
	}
	return protoAttrs
}

// convertEvents converts span events to protobuf format
func (p *VanillaProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	protoEvents := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		protoEvents[i] = &tracepb.Span_Event{
			TimeUnixNano: uint64(event.Time.UnixNano()),
			Name:         event.Name,
			Attributes:   p.convertAttributes(event.Attributes),
		}
	}
	return protoEvents
}

// convertLinks converts span links to protobuf format
func (p *VanillaProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
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
			Attributes: p.convertAttributes(link.Attributes),
		}
	}
	return protoLinks
}

// Shutdown implements SpanProcessor.Shutdown
func (p *VanillaProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 VanillaProcessor shutting down")

	// Stop the background workers
	close(p.stopChan)
	p.wg.Wait()

	// Stop the client
	if err := p.client.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	slog.Info("✅ VanillaProcessor shutdown complete",
		"eventsSent", p.eventsSent)
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *VanillaProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
