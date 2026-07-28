// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// RandomCheckpointProcessor is a CONTROL processor for isolating the effect
// of a target checkpoint *fraction* on the collector, decoupled from trace
// shape/size. It behaves like the vanilla processor on the SDK side — it adds
// NO baggage, NO breadcrumbs (_br/_d), and does NO structural ancestry
// tracking — but it looks up the deployment's checkpoint distance (cpd) and
// uses 1/cpd as an independent per-span Bernoulli probability of marking the
// span high-priority ("checkpoint"). Every span is an independent coin flip,
// so the HP fraction converges to 1/cpd regardless of how deep or wide any
// individual trace is.
//
// The export side is identical to the real bridge processors (SB/PB/CGPB):
// separate HP/LP buffers, priority-homogeneous batches tagged with the
// "bridges-priority: hp|lp" gRPC metadata header, retry-off client, and the
// full per-priority counter set logged as rc_processor_metrics. That keeps
// the collector-side priority processor seeing the exact same admit/refuse
// signal it would from a real bridge — only the checkpoint *labeling* differs
// (random vs structural). Reuses the package-level SB tunables and the shared
// grpcDeadline / grpcRetryEnabled knobs.
type RandomCheckpointProcessor struct {
	mu sync.RWMutex

	client        otlptrace.Client
	agentEndpoint string

	stopChan chan struct{}
	wg       sync.WaitGroup
	exportWG sync.WaitGroup

	hpBuf      []sbBufEntry
	lpBuf      []sbBufEntry
	eventsLock sync.Mutex

	resource     *resourcepb.Resource
	resourceOnce sync.Once

	// Per-batch / per-priority counters (mirror SB exactly).
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

	// Checkpoint distance (parsed from config, default: 1). The HP probability
	// is 1/checkpointDistance. Set once at startup (fetchFullConfig) before any
	// traffic, so OnEnd reads it without locking.
	checkpointDistance int64

	// Synthetic breadcrumb payload. brBase/brSlope come from the env vars
	// RC_BR_BASE / RC_BR_SLOPE (bytes). At startup we precompute one
	// random-filled buffer of length max(0, brBase + cpd*brSlope) and attach it
	// as a "_br" BYTES attribute to every checkpoint (HP) span — and only
	// checkpoints, mirroring how the real PCRB/CGPRB breadcrumb rides on
	// checkpoints and grows with cpd. Random fill keeps it incompressible so the
	// on-wire size is honest under any gRPC compression. The buffer is shared
	// read-only across all spans (set before any OnEnd runs), so no per-span
	// allocation and no data race. Both env vars default to 0 → no _br is added
	// and the processor stays a pure passthrough.
	brBase    int
	brSlope   int
	brPayload []byte
}

func NewRandomCheckpointProcessor(ctx context.Context, agentEndpoint string, configDiscoveryPort string) (*RandomCheckpointProcessor, error) {
	slog.Info("🔵 Creating new RandomCheckpointProcessor", "agentEndpoint", agentEndpoint)

	var host string
	if strings.Contains(agentEndpoint, ":") {
		parts := strings.Split(agentEndpoint, ":")
		if len(parts) >= 2 {
			host = parts[0]
		}
	}
	if host == "" {
		host = "localhost"
	}
	endpoint := fmt.Sprintf("%s:4317", host)
	slog.Info("🔵 Using endpoint", "endpoint", endpoint)

	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: grpcRetryEnabled}),
	)
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}
	slog.Info("✅ RandomCheckpointProcessor created successfully")

	configDiscoveryPortInt, err := strconv.Atoi(configDiscoveryPort)
	if err != nil {
		slog.Error("❌ Failed to convert configDiscoveryPort to int", "error", err)
		return nil, fmt.Errorf("failed to convert configDiscoveryPort to int: %w", err)
	}

	// Synthetic breadcrumb sizing from env (bytes). Default 0/0 → no _br.
	brBase := envIntDefault("RC_BR_BASE", 0)
	brSlope := envIntDefault("RC_BR_SLOPE", 0)

	processor := &RandomCheckpointProcessor{
		client:              client,
		agentEndpoint:       agentEndpoint,
		stopChan:            make(chan struct{}),
		configDiscoveryPort: configDiscoveryPortInt,
		httpClient:          &http.Client{Timeout: 10 * time.Second},
		configMap:           make(map[string]interface{}),
		checkpointDistance:  1, // Default: every span is a checkpoint (HP prob = 1.0)
		brBase:              brBase,
		brSlope:             brSlope,
	}

	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
		slog.Error("❌ Failed to fetch full config", "error", err)
		slog.Warn("⚠️ Continuing with empty config map (cpd defaults to 1)")
	} else {
		slog.Info("🟢 Successfully fetched full config", "config_keys", len(processor.configMap))
	}

	// Precompute the checkpoint breadcrumb buffer now that cpd is known and
	// BEFORE any OnEnd can run (background goroutines start below; the SDK only
	// calls OnStart/OnEnd after this constructor returns).
	processor.computeBRPayload()

	processor.wg.Add(1)
	go processor.processEvents()
	processor.wg.Add(1)
	go processor.logMetricsLoop()

	return processor, nil
}

// envIntDefault reads an integer env var, returning def if unset/unparseable.
func envIntDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		slog.Warn("⚠️ Failed to parse env int, using default", "key", key, "value", v, "default", def)
	}
	return def
}

// computeBRPayload (re)builds the shared checkpoint breadcrumb buffer:
// length = max(0, brBase + cpd*brSlope), filled with incompressible random
// bytes. Called once at startup before traffic.
func (p *RandomCheckpointProcessor) computeBRPayload() {
	cpd := int(atomic.LoadInt64(&p.checkpointDistance))
	if cpd < 1 {
		cpd = 1
	}
	n := p.brBase + cpd*p.brSlope
	if n <= 0 {
		p.brPayload = nil
		slog.Info("🔵 _br checkpoint payload disabled", "br_base", p.brBase, "br_slope", p.brSlope, "cpd", cpd)
		return
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(mathrand.UintN(256))
	}
	p.brPayload = buf
	slog.Info("🔵 _br checkpoint payload configured",
		"br_base", p.brBase, "br_slope", p.brSlope, "cpd", cpd, "br_payload_len", n)
}

func (p *RandomCheckpointProcessor) logMetricsLoop() {
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

func (p *RandomCheckpointProcessor) logMetrics() {
	p.eventsLock.Lock()
	hpDepth := len(p.hpBuf)
	lpDepth := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Info("rc_processor_metrics",
		"checkpoint_distance", atomic.LoadInt64(&p.checkpointDistance),
		"br_base", p.brBase,
		"br_slope", p.brSlope,
		"br_payload_len", len(p.brPayload),
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

func (p *RandomCheckpointProcessor) categorizeSendError(err error) {
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

func (p *RandomCheckpointProcessor) processEvents() {
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

func (p *RandomCheckpointProcessor) flushBuffer() {
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

func (p *RandomCheckpointProcessor) dispatchPriority(snap []sbBufEntry, isHP bool) {
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
		scopeProto := &commonpb.InstrumentationScope{Name: g.scope.Name, Version: g.scope.Version}
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

func (p *RandomCheckpointProcessor) exportBatch(events []*tracepb.ResourceSpans, n int64, isHP bool) {
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
}

func (p *RandomCheckpointProcessor) sendData(events []*tracepb.ResourceSpans, isHP bool) error {
	if len(events) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), grpcDeadline)
	defer cancel()
	priorityVal := "lp"
	if isHP {
		priorityVal = "hp"
	}
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataPriorityKey, priorityVal)
	if err := p.client.UploadTraces(ctx, events); err != nil {
		return fmt.Errorf("failed to send data: %w", err)
	}
	return nil
}

// OnStart is intentionally a no-op: this processor adds no baggage and does no
// structural tracking — it is a pure SDK passthrough plus a probabilistic
// priority label applied at OnEnd.
func (p *RandomCheckpointProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {}

// OnEnd classifies each span independently: high-priority ("checkpoint") with
// probability 1/cpd, else low-priority. No baggage, no propagation — every
// span is its own Bernoulli trial, so the realized HP fraction tends to 1/cpd
// regardless of trace depth/width.
func (p *RandomCheckpointProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	cpd := p.checkpointDistance
	if cpd < 1 {
		cpd = 1
	}
	hpProb := 1.0 / float64(cpd)
	isHP := mathrand.Float64() < hpProb
	p.routeToPipeline(s, isHP)
}

func (p *RandomCheckpointProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, highPriority bool) {
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
	entry := sbBufEntry{span: spanProto, scope: s.InstrumentationScope(), isCheckpoint: highPriority}
	p.eventsLock.Lock()
	if highPriority {
		p.hpBuf = append(p.hpBuf, entry)
	} else {
		p.lpBuf = append(p.lpBuf, entry)
	}
	p.eventsLock.Unlock()
}

func (p *RandomCheckpointProcessor) buildSpanProto(s sdktrace.ReadOnlySpan, highPriority bool) *tracepb.Span {
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
		Attributes:        p.convertAttributes(s.Attributes()),
		Events:            p.convertEvents(s.Events()),
		Links:             p.convertLinks(s.Links()),
	}
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:]
	}
	// Checkpoints (HP) carry a synthetic "_br" breadcrumb of base+cpd*slope
	// bytes (raw OTLP BYTES, like the real bridges); LP spans carry none. The
	// shared read-only buffer is attached by reference — proto marshaling reads
	// it concurrently without copying.
	if highPriority && len(p.brPayload) > 0 {
		spanProto.Attributes = append(spanProto.Attributes, &commonpb.KeyValue{
			Key:   string(AttrBREmit),
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: p.brPayload}},
		})
	}
	return spanProto
}

func (p *RandomCheckpointProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
	if resource == nil {
		return &resourcepb.Resource{}
	}
	var iter attribute.Iterator
	if r, ok := resource.(interface{ Iter() attribute.Iterator }); ok {
		iter = r.Iter()
	} else {
		return &resourcepb.Resource{}
	}
	return &resourcepb.Resource{Attributes: p.convertAttributeIterator(iter)}
}

func (p *RandomCheckpointProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
	if iter.Len() == 0 {
		return nil
	}
	attrs := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attrs = append(attrs, p.convertAttribute(iter.Attribute()))
	}
	return attrs
}

func (p *RandomCheckpointProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: string(kv.Key), Value: p.convertAttributeValue(kv.Value)}
}

func (p *RandomCheckpointProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
	av := new(commonpb.AnyValue)
	switch v.Type() {
	case attribute.STRING:
		av.Value = &commonpb.AnyValue_StringValue{StringValue: v.AsString()}
	case attribute.INT64:
		av.Value = &commonpb.AnyValue_IntValue{IntValue: v.AsInt64()}
	case attribute.FLOAT64:
		av.Value = &commonpb.AnyValue_DoubleValue{DoubleValue: v.AsFloat64()}
	case attribute.BOOL:
		av.Value = &commonpb.AnyValue_BoolValue{BoolValue: v.AsBool()}
	case attribute.BOOLSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: p.convertBoolSlice(v.AsBoolSlice())}}
	case attribute.INT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: p.convertInt64Slice(v.AsInt64Slice())}}
	case attribute.FLOAT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: p.convertFloat64Slice(v.AsFloat64Slice())}}
	case attribute.STRINGSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: p.convertStringSlice(v.AsStringSlice())}}
	default:
		av.Value = &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("%v", v.AsInterface())}
	}
	return av
}

func (p *RandomCheckpointProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v}}
	}
	return values
}

func (p *RandomCheckpointProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}
	}
	return values
}

func (p *RandomCheckpointProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v}}
	}
	return values
}

func (p *RandomCheckpointProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}
	}
	return values
}

func (p *RandomCheckpointProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
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

func (p *RandomCheckpointProcessor) convertStatus(st sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{Code: tracepb.Status_StatusCode(st.Code), Message: st.Description}
}

// convertAttributes is a plain passthrough (like the vanilla processor): it
// strips only the intra-process baggage/helper keys that must never reach the
// wire. There are no bridge breadcrumbs (_br/_d) to emit — this processor adds
// none.
func (p *RandomCheckpointProcessor) convertAttributes(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if strings.HasPrefix(string(attr.Key), "__bag.") {
			continue
		}
		switch attr.Key {
		case "depth", "hasChildren", "childCount", AncestryKey, AncestryModeKey, AncestryExtraKey:
			continue
		}
		out = append(out, p.convertAttribute(attr))
	}
	return out
}

func (p *RandomCheckpointProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
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

func (p *RandomCheckpointProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
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

func (p *RandomCheckpointProcessor) getConfigDiscoveryEndpoint() string {
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s:%d", parts[0], p.configDiscoveryPort)
		}
	}
	return fmt.Sprintf("localhost:%d", p.configDiscoveryPort)
}

func (p *RandomCheckpointProcessor) parseCheckpointDistance(config map[string]interface{}) int64 {
	const defaultCPD = int64(1)
	if config == nil {
		return defaultCPD
	}
	if cpdVal, exists := config["cpd"]; exists {
		switch v := cpdVal.(type) {
		case int64:
			if v > 0 {
				return v
			}
		case int:
			if v > 0 {
				return int64(v)
			}
		case float64:
			cpd := int64(v)
			if cpd > 0 && float64(cpd) == v {
				return cpd
			}
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err == nil && parsed > 0 {
				return parsed
			}
		}
		slog.Warn("cpd invalid, using default", "cpd", cpdVal)
	}
	return defaultCPD
}

func (p *RandomCheckpointProcessor) fetchFullConfig(ctx context.Context) error {
	config, err := p.fetchFullConfigFromEndpointWithRetries(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch full config: %w", err)
	}
	cpd := p.parseCheckpointDistance(config)
	p.configLock.Lock()
	p.configMap = config
	p.configLock.Unlock()
	atomic.StoreInt64(&p.checkpointDistance, cpd)
	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd,
		"hp_probability", 1.0/float64(cpd))
	return nil
}

func (p *RandomCheckpointProcessor) fetchFullConfigFromEndpointWithRetries(ctx context.Context) (map[string]interface{}, error) {
	url := fmt.Sprintf("http://%s/getFullConfig", p.getConfigDiscoveryEndpoint())
	for attempt := 1; attempt <= 60; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to make HTTP request after %d attempts: %w", attempt, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("config discovery returned status %d after %d attempts", resp.StatusCode, attempt)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to read response body after %d attempts: %w", attempt, err)
		}
		var configResp ConfigResponse
		if err := json.Unmarshal(body, &configResp); err != nil {
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to parse config response after %d attempts: %w", attempt, err)
		}
		if configResp.Config == nil {
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

func (p *RandomCheckpointProcessor) getConfigMap() map[string]interface{} {
	p.configLock.RLock()
	defer p.configLock.RUnlock()
	result := make(map[string]interface{})
	for k, v := range p.configMap {
		result[k] = v
	}
	return result
}

func (p *RandomCheckpointProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	slog.Info("🔴 RandomCheckpointProcessor shutting down")
	close(p.stopChan)
	p.wg.Wait()
	p.exportWG.Wait()
	if err := p.client.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}
	slog.Info("✅ RandomCheckpointProcessor shutdown complete",
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped))
	return nil
}

func (p *RandomCheckpointProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil
}
