// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CallGraphBridgeProcessor mirrors StructuralBridgeProcessor's
// export/metrics architecture exactly — dual HP/LP buffers,
// priority-homogeneous batched export with the "bridges-priority: hp|lp"
// gRPC metadata header, retry-off client, and the full per-priority counter
// set logged as a `cgpb_processor_metrics` line. The ONLY thing that
// differs from SB is the breadcrumb composition in OnStart (CGPRB =
// ckpt4-anchored PCRB + a window-local hash array; mirrors
// bridges/bridge/cgprb.go) and the `_d`/`_br` wire emission in
// convertAttributes.
type CallGraphBridgeProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string
	ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Tracks in-flight per-batch export goroutines so Shutdown can wait
	// for them to drain before closing the gRPC client.
	exportWG sync.WaitGroup

	// Two physical buffers, separated by CP/LP classification at OnEnd.
	hpBuf      []sbBufEntry
	lpBuf      []sbBufEntry
	eventsLock sync.Mutex

	// Shared Resource proto for every export, captured lazily on first OnEnd.
	resource     *resourcepb.Resource
	resourceOnce sync.Once

	// Per-batch / per-priority counters (all atomic). Mirrors SB exactly.
	spansReceived   int64
	spansFlushed    int64
	spansSent       int64
	batchesSent     int64
	spansDropped    int64
	batchesDropped  int64
	sendDeadline    int64
	sendUnavailable int64
	sendExhausted   int64
	sendCanceled    int64
	sendOther       int64
	cpReceived      int64
	lpReceived      int64
	cpSent          int64
	lpSent          int64
	cpDropped       int64
	lpDropped       int64

	// Config discovery
	configDiscoveryPort int
	httpClient          *http.Client
	configMap           map[string]interface{}
	configLock          sync.RWMutex

	// Checkpoint distance (parsed from config, default: 1)
	checkpointDistance int64
}

// Darby: this gets run once per service (when initialized)
func NewCallGraphBridgeProcessor(ctx context.Context, agentEndpoint string, configDiscoveryPort string) (*CallGraphBridgeProcessor, error) {
	slog.Info("🔵 Creating new CallGraphBridgeProcessor", "agentEndpoint", agentEndpoint)

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

	// Create endpoint
	endpoint := fmt.Sprintf("%s:4317", host)

	slog.Info("🔵 Using endpoint", "endpoint", endpoint)

	// Create OTLP gRPC client. Retry governed by shared grpcRetryEnabled
	// (OTLP_RETRY env) — deployments bake OTLP_RETRY=off.
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: grpcRetryEnabled}),
	)

	slog.Info("🔵 OTLP client created, starting connection")

	// Start the client
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}

	slog.Info("✅ CallGraphBridgeProcessor created successfully")

	// Parse config discovery port
	configDiscoveryPortInt, err := strconv.Atoi(configDiscoveryPort)
	if err != nil {
		slog.Error("❌ Failed to convert configDiscoveryPort to int", "error", err)
		return nil, fmt.Errorf("failed to convert configDiscoveryPort to int: %w", err)
	}

	// Create HTTP client for config discovery
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	processor := &CallGraphBridgeProcessor{
		client:              client,
		agentEndpoint:       agentEndpoint,
		bloomFilter:         bloom.New(BloomFilterM, BloomFilterK),
		stopChan:            make(chan struct{}),
		ancestryMode:        AncestryModePB,
		configDiscoveryPort: configDiscoveryPortInt,
		httpClient:          httpClient,
		configMap:           make(map[string]interface{}),
		checkpointDistance:  1, // Default: every span is a checkpoint
	}

	slog.Info("🔵 Ancestry mode configured", "mode", AncestryModePB)

	// CGPRB bloom geometry: capacity = cpd-1 (PCRB window), fp =
	// DefaultBloomFPRate. checkpointDistance is the default (1) here; re-sized
	// in fetchFullConfig once the real cpd is discovered.
	expectedElements := uint(pbBloomCapacity(int(processor.checkpointDistance)))
	if expectedElements == 0 {
		expectedElements = 1 // Ensure at least 1 element
	}
	calculatedM, calculatedK := bloom.EstimateParameters(expectedElements, DefaultBloomFPRate)
	BloomFilterM = calculatedM
	BloomFilterK = calculatedK

	// Fetch full config from config discovery endpoint
	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
		slog.Error("❌ Failed to fetch full config", "error", err)
		slog.Warn("⚠️ Continuing with empty config map")
	} else {
		slog.Info("🟢 Successfully fetched full config", "config_keys", len(processor.configMap))
	}

	// Start background worker for batch export
	processor.wg.Add(1)
	go processor.processEvents()

	// Start the metrics logger goroutine.
	processor.wg.Add(1)
	go processor.logMetricsLoop()

	return processor, nil
}

// logMetricsLoop emits one slog.Info line per SBMetricsInterval with the
// full per-priority counter snapshot. Tail via
// `kubectl logs deploy/<svc> | grep cgpb_processor_metrics`.
func (p *CallGraphBridgeProcessor) logMetricsLoop() {
	defer p.wg.Done()
	t := time.NewTicker(SBMetricsInterval)
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
// per-priority in-memory buffer depths.
func (p *CallGraphBridgeProcessor) logMetrics() {
	p.eventsLock.Lock()
	hpDepth := len(p.hpBuf)
	lpDepth := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Info("cgpb_processor_metrics",
		"spans_received", atomic.LoadInt64(&p.spansReceived),
		"spans_flushed", atomic.LoadInt64(&p.spansFlushed),
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"batches_sent", atomic.LoadInt64(&p.batchesSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped),
		"batches_dropped", atomic.LoadInt64(&p.batchesDropped),
		"cp_received", atomic.LoadInt64(&p.cpReceived),
		"lp_received", atomic.LoadInt64(&p.lpReceived),
		"cp_sent", atomic.LoadInt64(&p.cpSent),
		"lp_sent", atomic.LoadInt64(&p.lpSent),
		"cp_dropped", atomic.LoadInt64(&p.cpDropped),
		"lp_dropped", atomic.LoadInt64(&p.lpDropped),
		"send_deadline", atomic.LoadInt64(&p.sendDeadline),
		"send_unavailable", atomic.LoadInt64(&p.sendUnavailable),
		"send_exhausted", atomic.LoadInt64(&p.sendExhausted),
		"send_canceled", atomic.LoadInt64(&p.sendCanceled),
		"send_other", atomic.LoadInt64(&p.sendOther),
		"hp_buffer_depth", hpDepth,
		"lp_buffer_depth", lpDepth,
	)
}

// categorizeSendError increments the appropriate per-code counter for a
// failed UploadTraces.
func (p *CallGraphBridgeProcessor) categorizeSendError(err error) {
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
// buffer and dispatch batches for export.
func (p *CallGraphBridgeProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(SBExportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			p.flushBuffer()
			return
		case <-ticker.C:
			go p.flushBuffer()
		}
	}
}

// flushBuffer atomically snaps both priority buffers, then dispatches HP
// chunks first, then LP chunks. Each chunk carries one priority class
// (never mixed) tagged with a "bridges-priority: hp|lp" gRPC metadata header.
func (p *CallGraphBridgeProcessor) flushBuffer() {
	p.eventsLock.Lock()
	hpSnap := p.hpBuf
	lpSnap := p.lpBuf
	p.hpBuf = make([]sbBufEntry, 0, cap(p.hpBuf))
	p.lpBuf = make([]sbBufEntry, 0, cap(p.lpBuf))
	p.eventsLock.Unlock()

	if len(hpSnap) == 0 && len(lpSnap) == 0 {
		return
	}

	atomic.AddInt64(&p.spansFlushed, int64(len(hpSnap)+len(lpSnap)))

	p.dispatchPriority(hpSnap, true)
	p.dispatchPriority(lpSnap, false)
}

// dispatchPriority groups one priority class's snapshot by scope, chunks
// each scope group at SBBatchSize, and spawns one goroutine per chunk.
func (p *CallGraphBridgeProcessor) dispatchPriority(snap []sbBufEntry, isHP bool) {
	if len(snap) == 0 {
		return
	}
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
	for _, g := range groups {
		scopeProto := &commonpb.InstrumentationScope{
			Name:    g.scope.Name,
			Version: g.scope.Version,
		}
		for start := 0; start < len(g.spans); start += SBBatchSize {
			end := start + SBBatchSize
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
			p.exportWG.Add(1)
			go p.exportBatch([]*tracepb.ResourceSpans{rs}, int64(len(chunk)), isHP)
		}
	}
}

// exportBatch sends one priority-homogeneous batch and updates counters
// based on outcome. On failure the spans are permanently dropped (no retry).
func (p *CallGraphBridgeProcessor) exportBatch(events []*tracepb.ResourceSpans, n int64, isHP bool) {
	defer p.exportWG.Done()
	err := p.sendData(events, isHP)
	if err != nil {
		atomic.AddInt64(&p.batchesDropped, 1)
		atomic.AddInt64(&p.spansDropped, n)
		if isHP {
			atomic.AddInt64(&p.cpDropped, n)
		} else {
			atomic.AddInt64(&p.lpDropped, n)
		}
		p.categorizeSendError(err)
		slog.Error("Failed to send batch", "error", err, "count", n, "hp", isHP)
		return
	}
	atomic.AddInt64(&p.batchesSent, 1)
	atomic.AddInt64(&p.spansSent, n)
	if isHP {
		atomic.AddInt64(&p.cpSent, n)
	} else {
		atomic.AddInt64(&p.lpSent, n)
	}
	slog.Debug("Sent batch", "count", n, "hp", isHP)
}

// sendData sends one priority-homogeneous batch with the
// "bridges-priority: hp|lp" gRPC metadata header attached.
func (p *CallGraphBridgeProcessor) sendData(events []*tracepb.ResourceSpans, isHP bool) error {
	if len(events) == 0 {
		return nil
	}

	slog.Debug("🔵 Sending data", "count", len(events), "hp", isHP)

	ctx, cancel := context.WithTimeout(context.Background(), grpcDeadline)
	defer cancel()

	priorityVal := "lp"
	if isHP {
		priorityVal = "hp"
	}
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataPriorityKey, priorityVal)

	err := p.client.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ Failed to send data", "error", err, "count", len(events), "hp", isHP)
		return fmt.Errorf("failed to send data: %w", err)
	}

	slog.Debug("✅ Data sent successfully", "count", len(events), "hp", isHP)
	return nil
}

// OnStart implements SpanProcessor.OnStart
func (p *CallGraphBridgeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// Canonical call-graph path bridge (CGPRB = ckpt4-anchored PCRB + a window-
	// local hash array). Decode inbound baggage `_br` =
	// varint(absolute depth) || ckpt4 || propagated bloom (fixed ceil(m/8)) || HA.
	bloomLen := int((BloomFilterM + 7) / 8)
	var (
		hasParent        bool
		parentDepth      int
		parentCkpt4      [4]byte
		parentBloomBytes []byte
		parentHA         []byte
	)
	if baggage := backend.GetBaggageFromContext(parent); baggage != nil {
		if br, ok := baggage[BaggageBRKey]; ok && br != "" {
			if raw, okB := decodeBR(br); okB {
				if d, c4, bb, hb, okU := unpackCGPRBBR(raw, bloomLen); okU {
					hasParent = true
					parentDepth = d
					parentCkpt4 = c4
					parentBloomBytes = bb
					parentHA = hb
				}
			}
		}
	}

	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}

	// Absolute depth (root=0); inherited ckpt4 anchor; INHERITED (pre-self)
	// window bloom.
	var depth int
	var inheritedCkpt4 [4]byte
	var bf *bloom.BloomFilter
	if hasParent {
		depth = parentDepth + 1
		inheritedCkpt4 = parentCkpt4
		bf = bloom.NewFromBytes(parentBloomBytes, BloomFilterM, BloomFilterK)
	} else {
		depth = 0
		bf = bloom.New(BloomFilterM, BloomFilterK)
	}

	// HA (CGPRB rules): the 1st-started child inherits the parent's window HA;
	// the 2nd-started child starts FRESH and appends one branch record
	// (parent_span_id(8) || varint(absolute depth)); 3rd+ carry none. seqNum is
	// set by the OT client wrapper (per-parent atomic child counter). An entry
	// is thus never duplicated across a fan-out — one record per branching
	// parent asserts the branch exists.
	seqNum, _ := parent.Value("seqNum").(int)
	var ha []byte
	if hasParent && seqNum == 1 {
		ha = append([]byte(nil), parentHA...)
	} else if hasParent && seqNum == 2 {
		parentSpanID := trace.SpanFromContext(parent).SpanContext().SpanID().String()
		ha = haAppendEntry(nil, parentSpanID, depth)
	}

	isCheckpoint := depth%cpd == 0

	spanID := s.SpanContext().SpanID().String()
	sid8, _ := spanIDHexTo8Bytes(spanID)
	var ownCkpt4 [4]byte
	copy(ownCkpt4[:], sid8[:4])

	// Emit payload (checkpoints + leaves): names the PREVIOUS checkpoint
	// (inheritedCkpt4), the INHERITED pre-self window bloom, and the window HA.
	emitPayload := packCGPRBBR(depth, inheritedCkpt4, bf.Bytes(), ha)

	// Propagation + priority. Checkpoint re-roots ckpt4→own and resets BOTH the
	// bloom AND the HA to empty (window-scoped). Non-checkpoint keeps anchor,
	// adds self to bloom, carries the HA forward (only the 1st child inherits it).
	priority := 0
	var propCkpt4 [4]byte
	var propBloomBytes []byte
	propHA := ha
	if isCheckpoint {
		priority = 1
		propCkpt4 = ownCkpt4
		propBloomBytes = bloom.New(BloomFilterM, BloomFilterK).Bytes()
		propHA = nil
	} else {
		propCkpt4 = inheritedCkpt4
		bf.Add([]byte(spanID))
		propBloomBytes = bf.Bytes()
	}
	propagationPacked := packCGPRBBR(depth, propCkpt4, propBloomBytes, propHA)

	s.SetAttributes(
		attribute.Int(AttrBagPrio, priority),
		attribute.String(AttrBR, encodeBR(propagationPacked)),  // __bag._br: outgoing propagation baggage
		attribute.String(AttrBREmit, encodeBR(emitPayload)),    // "_br": wire payload, kept iff checkpoint/leaf
		attribute.String(AttrD, encodeBR(varintEncode(depth))), // "_d": wire depth, kept iff interior non-checkpoint
		attribute.Int("depth", depth),
	)
}

// OnEnd implements SpanProcessor.OnEnd
func (p *CallGraphBridgeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// Extract priority from span attributes
	var priority int
	var hasPriority bool
	var hasChildren bool

	for _, attr := range s.Attributes() {
		switch attr.Key {
		case "__bag.prio":
			val := attr.Value.AsInt64()
			priority = int(val)
			hasPriority = true
		case "childCount":
			hasChildren = attr.Value.AsInt64() > 0
		}
	}

	if s.SpanKind() == trace.SpanKindServer {
		if hasChildren {
			// Non-leaf server span - force to low priority (priority = 0)
			priority += 0
		} else {
			// Leaf server span - always checkpoint (priority = 1)
			priority = 1
		}
	}

	if !hasPriority {
		priority = 0
	}

	// Route span to pipeline
	p.routeToPipeline(s, priority == 1)
}

// routeToPipeline classifies and buffers the span, capturing the shared
// Resource on first use and counting per-priority received totals.
func (p *CallGraphBridgeProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, highPriority bool) {
	atomic.AddInt64(&p.spansReceived, 1)
	if highPriority {
		atomic.AddInt64(&p.cpReceived, 1)
	} else {
		atomic.AddInt64(&p.lpReceived, 1)
	}
	p.resourceOnce.Do(func() {
		p.resource = p.convertResourceToProto(s.Resource())
	})
	spanProto := p.buildSpanProto(s, highPriority)
	entry := sbBufEntry{
		span:         spanProto,
		scope:        s.InstrumentationScope(),
		isCheckpoint: highPriority,
	}
	p.eventsLock.Lock()
	if highPriority {
		p.hpBuf = append(p.hpBuf, entry)
	} else {
		p.lpBuf = append(p.lpBuf, entry)
	}
	hpSize := len(p.hpBuf)
	lpSize := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Debug("🔴 Routed to pipeline", "span_name", s.Name(), "hp_buffer_size", hpSize, "lp_buffer_size", lpSize, "high_prio", highPriority)
}

// buildSpanProto materializes a single *tracepb.Span from a ReadOnlySpan.
// The Resource and ScopeSpans envelopes are added at flush time so one
// ResourceSpans wraps many spans.
func (p *CallGraphBridgeProcessor) buildSpanProto(s sdktrace.ReadOnlySpan, highPriority bool) *tracepb.Span {
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

	spanProto := &tracepb.Span{
		TraceId:           traceID[:],
		SpanId:            spanID[:],
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(s.EndTime().UnixNano()),
		Name:              s.Name(),
		Kind:              p.convertSpanKind(s.SpanKind()),
		Status:            p.convertStatus(s.Status()),
		Attributes:        p.convertAttributes(s.Attributes(), highPriority),
		Events:            p.convertEvents(s.Events()),
		Links:             p.convertLinks(s.Links()),
	}
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:]
	}
	return spanProto
}

// convertResourceToProto converts an OpenTelemetry resource to protobuf format
func (p *CallGraphBridgeProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
	if resource == nil {
		return &resourcepb.Resource{}
	}

	var iter attribute.Iterator
	if r, ok := resource.(interface{ Iter() attribute.Iterator }); ok {
		iter = r.Iter()
	} else {
		return &resourcepb.Resource{}
	}

	attrs := p.convertAttributeIterator(iter)

	return &resourcepb.Resource{
		Attributes: attrs,
	}
}

// convertAttributeIterator converts an attribute iterator to protobuf format
func (p *CallGraphBridgeProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
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
func (p *CallGraphBridgeProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *CallGraphBridgeProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
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
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: fmt.Sprintf("%v", v.AsInterface()),
		}
	}
	return av
}

// Helper functions for slice conversions
func (p *CallGraphBridgeProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: v},
		}
	}
	return values
}

func (p *CallGraphBridgeProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: v},
		}
	}
	return values
}

func (p *CallGraphBridgeProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v},
		}
	}
	return values
}

func (p *CallGraphBridgeProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		}
	}
	return values
}

// convertSpanKind converts OpenTelemetry span kind to protobuf format
func (p *CallGraphBridgeProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
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
func (p *CallGraphBridgeProcessor) convertStatus(status sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{
		Code:    tracepb.Status_StatusCode(status.Code),
		Message: status.Description,
	}
}

// convertAttributes converts span attributes to protobuf format, including all baggage attributes
func (p *CallGraphBridgeProcessor) convertAttributes(attrs []attribute.KeyValue, highPriority bool) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		// (1) intra-process baggage + (2) Blueprint helpers never reach the wire.
		if strings.HasPrefix(string(attr.Key), "__bag.") {
			continue
		}
		switch attr.Key {
		case "depth", "hasChildren", "childCount":
			continue
		}
		// (3) CGPRB breadcrumbs ride the wire as proto BYTES (base64-decoded from
		// the intra-process carrier), priority-gated: checkpoints+leaves carry
		// `_br` (depth ‖ ckpt4 ‖ bloom ‖ HA), interior non-checkpoints carry `_d`
		// (just the absolute-depth varint).
		if attr.Key == AttrBREmit {
			if !highPriority {
				continue
			}
			out = append(out, bridgeBytesKV(string(AttrBREmit), attr.Value.AsString()))
			continue
		}
		if attr.Key == AttrD {
			if highPriority {
				continue
			}
			out = append(out, bridgeBytesKV(string(AttrD), attr.Value.AsString()))
			continue
		}
		// (4) drop superseded base64 ancestry keys if any linger.
		switch attr.Key {
		case AncestryKey, AncestryModeKey, AncestryExtraKey:
			continue
		}
		// (5) everything else passes through.
		out = append(out, p.convertAttribute(attr))
	}
	return out
}

// convertEvents converts span events to protobuf format
func (p *CallGraphBridgeProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	protoEvents := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		protoEvents[i] = &tracepb.Span_Event{
			TimeUnixNano: uint64(event.Time.UnixNano()),
			Name:         event.Name,
			Attributes:   p.convertAttributes(event.Attributes, true),
		}
	}
	return protoEvents
}

// convertLinks converts span links to protobuf format
func (p *CallGraphBridgeProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
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
			Attributes: p.convertAttributes(link.Attributes, true),
		}
	}
	return protoLinks
}

// getConfigDiscoveryEndpoint converts the agent endpoint to the config discovery endpoint
func (p *CallGraphBridgeProcessor) getConfigDiscoveryEndpoint() string {
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			host := parts[0]
			return fmt.Sprintf("%s:%d", host, p.configDiscoveryPort)
		}
	}
	return fmt.Sprintf("localhost:%d", p.configDiscoveryPort)
}

// parseCheckpointDistance extracts and parses the checkpoint distance (cpd) from the config map
func (p *CallGraphBridgeProcessor) parseCheckpointDistance(config map[string]interface{}) int64 {
	const defaultCPD = int64(1) // Default: every span is a checkpoint

	if config == nil {
		return defaultCPD
	}

	if cpdVal, exists := config["cpd"]; exists {
		switch v := cpdVal.(type) {
		case int64:
			if v > 0 {
				return v
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case int:
			if v > 0 {
				return int64(v)
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case float64:
			cpd := int64(v)
			if cpd > 0 && float64(cpd) == v {
				return cpd
			}
			slog.Warn("cpd must be a positive integer, using default", "cpd", v)
			return defaultCPD
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				if parsed > 0 {
					return parsed
				}
				slog.Warn("cpd must be positive, using default", "cpd", v)
				return defaultCPD
			}
			slog.Warn("Failed to parse cpd as int64, using default", "cpd", v, "error", err)
			return defaultCPD
		default:
			slog.Warn("cpd has unexpected type, using default", "cpd", v, "type", fmt.Sprintf("%T", v))
			return defaultCPD
		}
	}

	return defaultCPD
}

// fetchFullConfig fetches the full config from the config discovery endpoint
func (p *CallGraphBridgeProcessor) fetchFullConfig(ctx context.Context) error {
	config, err := p.fetchFullConfigFromEndpointWithRetries(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch full config: %w", err)
	}

	cpd := p.parseCheckpointDistance(config)

	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		slog.Warn("Failed to marshal config to JSON for logging", "error", err)
	} else {
		slog.Info("Fetched config JSON", "config", string(configJSON))
	}

	p.configLock.Lock()
	p.configMap = config
	p.checkpointDistance = cpd
	p.configLock.Unlock()

	// Re-size the bloom for the discovered cpd (CGPRB/PCRB capacity = cpd-1).
	// Runs at startup before traffic, so the global is settled before OnStart.
	bm, bk := bloom.EstimateParameters(uint(pbBloomCapacity(int(cpd))), DefaultBloomFPRate)
	BloomFilterM = bm
	BloomFilterK = bk

	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd)
	return nil
}

// fetchFullConfigFromEndpointWithRetries fetches config from the discovery endpoint with retries
func (p *CallGraphBridgeProcessor) fetchFullConfigFromEndpointWithRetries(ctx context.Context) (map[string]interface{}, error) {
	configDiscoveryEndpoint := p.getConfigDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getFullConfig", configDiscoveryEndpoint)

	for attempt := 1; attempt <= 60; attempt++ {
		slog.Debug("Attempting config discovery", "attempt", attempt, "endpoint", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			slog.Debug("Config discovery attempt failed, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to make HTTP request after %d attempts: %w", attempt, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Debug("Config discovery endpoint returned non-OK status, will retry", "attempt", attempt, "status", resp.StatusCode)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("config discovery endpoint returned status %d after %d attempts", resp.StatusCode, attempt)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Debug("Failed to read response body, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to read response body after %d attempts: %w", attempt, err)
		}

		var configResp ConfigResponse
		if err := json.Unmarshal(body, &configResp); err != nil {
			slog.Debug("Failed to parse config response, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to parse config response after %d attempts: %w", attempt, err)
		}

		if configResp.Config == nil {
			slog.Debug("Empty config in response, will retry", "attempt", attempt)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("empty config in response after %d attempts", attempt)
		}

		slog.Info("Config discovery successful", "attempt", attempt, "config_keys", len(configResp.Config))
		return configResp.Config, nil
	}

	return nil, fmt.Errorf("config discovery failed after 60 attempts")
}

// getConfigMap returns the config map, with thread-safe access
func (p *CallGraphBridgeProcessor) getConfigMap() map[string]interface{} {
	p.configLock.RLock()
	defer p.configLock.RUnlock()

	result := make(map[string]interface{})
	for k, v := range p.configMap {
		result[k] = v
	}
	return result
}

// Shutdown implements SpanProcessor.Shutdown
func (p *CallGraphBridgeProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 CallGraphBridgeProcessor shutting down")

	// Stop the background workers, then wait for in-flight export goroutines
	// to drain before closing the gRPC client.
	close(p.stopChan)
	p.wg.Wait()
	p.exportWG.Wait()

	if err := p.client.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	slog.Info("✅ CallGraphBridgeProcessor shutdown complete",
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped))
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *CallGraphBridgeProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil
}
