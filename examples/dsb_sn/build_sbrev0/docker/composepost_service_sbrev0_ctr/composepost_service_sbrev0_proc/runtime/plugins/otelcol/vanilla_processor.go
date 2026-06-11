// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Tunables for the export path. ExportInterval bounds the latency between
// a span ending and its OTLP RPC; BatchSize bounds the per-RPC payload so
// we don't crash into the default 4 MiB gRPC message ceiling on the
// receiver side (one ResourceSpans-per-span at ~300–1000 B mean lets a
// 15K-span tick exceed 4 MiB easily).
const (
	ExportInterval = 100 * time.Millisecond
	BatchSize      = 512
	// MetricsInterval bounds how often the processor logs its counter
	// snapshot to slog. 1s matches the ramp's per-step granularity so
	// you can correlate counter deltas with rps steps in real time.
	MetricsInterval = 1 * time.Second
)

// bufferedSpan is one buffered span pending export. The Span proto is kept
// mutable so downstream processors (e.g. bridge variants) can append
// breadcrumb attributes or events between OnEnd and the next flush. The
// scope is carried alongside so flushBuffer can group spans into
// per-InstrumentationScope ScopeSpans blocks at export time without
// duplicating the Resource envelope per span.
type bufferedSpan struct {
	span  *tracepb.Span
	scope instrumentation.Scope
}

type VanillaProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Tracks in-flight per-batch export goroutines so Shutdown can wait for
	// them to drain before closing the gRPC client.
	exportWG sync.WaitGroup

	// Counters for drop monitoring. All atomic so the metrics goroutine
	// can read them concurrently with OnEnd / flushBuffer.
	// Invariants:
	//   spansReceived = OnEnd entries (input to processor)
	//   spansFlushed  = spans drained from the buffer in flushBuffer ticks
	//                 = spansSent + spansDropped (every flushed span
	//                   either uploads successfully or is counted dropped)
	//   batchesSent + batchesDropped = batches dispatched
	// The send_* counters bucket dropped batches by gRPC status code so
	// you can tell whether drops are deadline / unavailable / queue
	// overflow / other.
	spansReceived   int64
	spansFlushed    int64
	spansSent       int64
	batchesSent     int64
	spansDropped    int64
	batchesDropped  int64
	sendDeadline    int64 // gRPC code = DeadlineExceeded
	sendUnavailable int64 // gRPC code = Unavailable
	sendExhausted   int64 // gRPC code = ResourceExhausted
	sendCanceled    int64 // gRPC code = Canceled
	sendOther       int64 // any other error class

	// Shared Resource proto for every export. A TracerProvider has exactly
	// one Resource, so we capture it lazily on the first OnEnd and reuse
	// the same pointer for every flush — eliminates the per-span Resource
	// duplication that would otherwise inflate the wire payload ~3×.
	resource     *resourcepb.Resource
	resourceOnce sync.Once

	// Buffer of pending spans plus their InstrumentationScope. flushBuffer
	// groups by scope at export time to produce one ResourceSpans per chunk
	// (shared Resource, one ScopeSpans per distinct scope present in the
	// chunk).
	spanBuf  []bufferedSpan
	spanLock sync.Mutex
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

	// Start the metrics logger goroutine. Shares stopChan with
	// processEvents — Shutdown closes once and both unwind.
	processor.wg.Add(1)
	go processor.logMetricsLoop()

	return processor, nil
}

// logMetricsLoop emits one slog.Info line per MetricsInterval with the
// full counter snapshot, until stopChan is closed. Picked up by anything
// tailing kubectl logs.
func (p *VanillaProcessor) logMetricsLoop() {
	defer p.wg.Done()
	t := time.NewTicker(MetricsInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stopChan:
			return
		case <-t.C:
			p.logMetrics()
		}
	}
}

// logMetrics emits one snapshot of every counter plus the current
// in-memory buffer depth. Counters are loaded atomically; buffer depth
// is a brief lock for an O(1) read.
func (p *VanillaProcessor) logMetrics() {
	p.spanLock.Lock()
	bufDepth := len(p.spanBuf)
	p.spanLock.Unlock()
	slog.Info("vanilla_processor_metrics",
		"spans_received", atomic.LoadInt64(&p.spansReceived),
		"spans_flushed", atomic.LoadInt64(&p.spansFlushed),
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"batches_sent", atomic.LoadInt64(&p.batchesSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped),
		"batches_dropped", atomic.LoadInt64(&p.batchesDropped),
		"send_deadline", atomic.LoadInt64(&p.sendDeadline),
		"send_unavailable", atomic.LoadInt64(&p.sendUnavailable),
		"send_exhausted", atomic.LoadInt64(&p.sendExhausted),
		"send_canceled", atomic.LoadInt64(&p.sendCanceled),
		"send_other", atomic.LoadInt64(&p.sendOther),
		"buffer_depth", bufDepth,
	)
}

// categorizeSendError increments the appropriate per-code counter for a
// failed UploadTraces. Uses status.Code which unwraps the gRPC status
// out of fmt.Errorf %w wrappers.
func (p *VanillaProcessor) categorizeSendError(err error) {
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		atomic.AddInt64(&p.sendDeadline, 1)
	case codes.Unavailable:
		atomic.AddInt64(&p.sendUnavailable, 1)
	case codes.ResourceExhausted:
		atomic.AddInt64(&p.sendExhausted, 1)
	case codes.Canceled:
		atomic.AddInt64(&p.sendCanceled, 1)
	default:
		atomic.AddInt64(&p.sendOther, 1)
	}
}

// processEvents runs in the background to periodically drain the event
// buffer and dispatch batches for export. Each ticker fire snapshots the
// buffer once and spawns one goroutine per BatchSize-sized chunk; export
// I/O is therefore fully off the OnEnd hot path and parallel across chunks.
func (p *VanillaProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(ExportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Final drain: spawns chunk goroutines and returns; Shutdown
			// waits on exportWG after wg.Wait().
			p.flushBuffer()
			return
		case <-ticker.C:
			go p.flushBuffer()
		}
	}
}

// flushBuffer atomically swaps out the current span buffer, groups spans by
// InstrumentationScope, chunks each scope group at BatchSize, and exports
// each chunk in its own goroutine. Each chunk becomes a single
// ResourceSpans wrapping a single ScopeSpans wrapping ≤BatchSize Spans —
// so the shared Resource envelope is serialized exactly once per chunk
// rather than once per span (~3× smaller wire payload than the
// per-span-ResourceSpans shape it replaced). Chunking keeps every OTLP
// RPC under the receiver-side 4 MiB gRPC limit regardless of arrival rate.
func (p *VanillaProcessor) flushBuffer() {
	p.spanLock.Lock()
	snap := p.spanBuf
	p.spanBuf = make([]bufferedSpan, 0, cap(p.spanBuf))
	p.spanLock.Unlock()

	if len(snap) == 0 {
		return
	}

	// Group spans by scope. Key is Name|Version — both are part of the
	// InstrumentationScope identity at the proto level. In practice nearly
	// all spans in a service share one scope, so this map almost always
	// has exactly one entry.
	type scopeKey struct{ name, version string }
	type scopeGroup struct {
		scope instrumentation.Scope
		spans []*tracepb.Span
	}
	groups := make(map[scopeKey]*scopeGroup, 1)
	for _, e := range snap {
		k := scopeKey{e.scope.Name, e.scope.Version}
		g, ok := groups[k]
		if !ok {
			g = &scopeGroup{scope: e.scope}
			groups[k] = g
		}
		g.spans = append(g.spans, e.span)
	}

	// Chunk each scope group at BatchSize and dispatch each chunk in its
	// own goroutine. One ResourceSpans per chunk; Resource envelope shared.
	for _, g := range groups {
		scopeProto := &commonpb.InstrumentationScope{
			Name:    g.scope.Name,
			Version: g.scope.Version,
		}
		for start := 0; start < len(g.spans); start += BatchSize {
			end := start + BatchSize
			if end > len(g.spans) {
				end = len(g.spans)
			}
			chunk := g.spans[start:end]
			rs := &tracepb.ResourceSpans{
				Resource: p.resource,
				ScopeSpans: []*tracepb.ScopeSpans{{
					Scope: scopeProto,
					Spans: chunk,
				}},
			}
			// Count the chunk as flushed BEFORE we dispatch — at this point
			// the spans have permanently left the buffer, so whatever
			// happens to the upload they'll be accounted for under one of
			// {sent, dropped}.
			atomic.AddInt64(&p.spansFlushed, int64(len(chunk)))
			p.exportWG.Add(1)
			go func(rs *tracepb.ResourceSpans, n int) {
				defer p.exportWG.Done()
				if err := p.sendData([]*tracepb.ResourceSpans{rs}); err != nil {
					slog.Error("Failed to send batch", "error", err, "count", n)
					atomic.AddInt64(&p.batchesDropped, 1)
					atomic.AddInt64(&p.spansDropped, int64(n))
					p.categorizeSendError(err)
					return
				}
				slog.Debug("Sent batch", "count", n)
				atomic.AddInt64(&p.batchesSent, 1)
				atomic.AddInt64(&p.spansSent, int64(n))
			}(rs, len(chunk))
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
	atomic.AddInt64(&p.spansReceived, 1)

	// Capture the (single, immutable) Resource on the first OnEnd. A
	// TracerProvider has exactly one Resource for its lifetime, so this
	// runs at most once.
	p.resourceOnce.Do(func() {
		p.resource = p.convertResourceToProto(s.Resource())
	})

	p.routeToPipeline(s)
}

// routeToPipeline builds the mutable Span proto for s and appends it to
// the buffer alongside its InstrumentationScope. The proto's Attributes /
// Events slices stay mutable in the buffer so subsequent stages (e.g.
// bridge processors that need to append breadcrumb attributes between
// OnEnd and the next flush) can extend them in place.
func (p *VanillaProcessor) routeToPipeline(s sdktrace.ReadOnlySpan) {
	spanProto := p.buildSpanProto(s)
	p.spanLock.Lock()
	p.spanBuf = append(p.spanBuf, bufferedSpan{span: spanProto, scope: s.InstrumentationScope()})
	p.spanLock.Unlock()
}

// buildSpanProto materializes a single *tracepb.Span from a ReadOnlySpan.
// The Resource and ScopeSpans envelopes are NOT built here — they're
// constructed at flush time, where many spans can share one Resource and
// be grouped by Scope, so the envelopes are serialized once per chunk
// instead of once per span.
func (p *VanillaProcessor) buildSpanProto(s sdktrace.ReadOnlySpan) *tracepb.Span {
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

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

	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:] // Convert [8]byte to []byte
	}

	return spanProto
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

	// Stop the ticker goroutine (which runs one final flushBuffer to drain
	// the in-memory buffer into chunk goroutines tracked by exportWG).
	close(p.stopChan)
	p.wg.Wait()

	// Wait for all in-flight per-batch export goroutines to finish before
	// closing the gRPC client — otherwise their UploadTraces calls would
	// race the client.Stop and lose the final tail of spans.
	p.exportWG.Wait()

	// Stop the client
	if err := p.client.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	// Emit one final counter snapshot before returning so tail-end stats
	// land in the logs even if the shutdown is the last thing the process
	// does.
	slog.Info("🔴 VanillaProcessor shutdown complete — final metrics:")
	p.logMetrics()
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *VanillaProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
