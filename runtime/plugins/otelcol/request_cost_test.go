package otelcol

// Tomislav-RetCtx: per-request application cost of tracing, mirroring what a frontend
// SearchHandler does per request in the deployed hotel services (CPU profile 2026-09-23,
// frontend at 12k rps: PB +25 s CPU per 30 s over vanilla, ~70 us/request):
//
//   - one server span (the request root, as the frontend is for wrk2),
//   - three client spans, each doing the generated OT client wrapper's work
//     (plugins/opentelemetry/ir_ot_client.go): copy the upstream baggage, read
//     "__bag."-prefixed span attributes into it, marshal the span context and wrap it
//     with the baggage as JSON (backend.AddBaggageToTraceContext); for the bridges with
//     the response path on, hand a returned checkpoint truss to PrepareCheckpoint,
//   - span End (processor OnEnd: classify, build the span proto, buffer),
//   - the export encode: every 64 requests the buffered protos are wrapped and
//     proto-marshalled as the processor's export goroutines do (no network).
//
// Run:  go test ./runtime/plugins/otelcol/ -run '^$' -bench 'BenchmarkRequest' -benchmem -count 5

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// wrapperBaggage is the client wrapper's baggage construction.
func wrapperBaggage(ctx context.Context, span trace.Span, seq int) map[string]string {
	upstream := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	for k, v := range upstream {
		baggage[k] = v
	}
	baggage["__seq"] = strconv.Itoa(seq)
	backend.AppendSpanBaggage(span, baggage)
	if rw, ok := span.(sdktrace.ReadWriteSpan); ok {
		for _, attr := range rw.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				delete(baggage, strings.TrimPrefix(string(attr.Key), "__bagdel."))
			}
		}
	}
	return baggage
}

func oneFrontendRequest(tp trace.TracerProvider, reverse bool, retCtx string) {
	tr := tp.Tracer("frontend")
	ctx, server := tr.Start(context.Background(), "FrontEndServiceServer_SearchHandler", trace.WithSpanKind(trace.SpanKindServer))
	for i := 1; i <= 3; i++ {
		cctx, span := tr.Start(ctx, "SearchServiceClient_Nearby", trace.WithSpanKind(trace.SpanKindClient))
		baggage := wrapperBaggage(cctx, span, i)
		carrier := backend.EncodeTraceCarrier(span.SpanContext(), baggage) // the wrapper template's encoder
		_, _, _ = backend.GetSpanContext(carrier)                          // and the callee's parse of it
		if reverse && retCtx != "" {
			span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, retCtx))
			backend.PrepareCheckpoint(tp, span, retCtx)
		}
		span.End()
	}
	server.End()
}

// encodeBuffered drains a processor's buffers and proto-marshals them as export does.
func encodeBuffered(p sdktrace.SpanProcessor, resource any) {
	var spans []*tracepb.Span
	switch q := p.(type) {
	case *PathBridgeProcessor:
		q.eventsLock.Lock()
		for _, e := range append(q.hpBuf, q.lpBuf...) {
			spans = append(spans, e.span)
		}
		q.hpBuf, q.lpBuf = q.hpBuf[:0], q.lpBuf[:0]
		q.eventsLock.Unlock()
	case *CallGraphBridgeProcessor:
		q.eventsLock.Lock()
		for _, e := range append(q.hpBuf, q.lpBuf...) {
			spans = append(spans, e.span)
		}
		q.hpBuf, q.lpBuf = q.hpBuf[:0], q.lpBuf[:0]
		q.eventsLock.Unlock()
	case *StructuralBridgeProcessor:
		q.eventsLock.Lock()
		for _, e := range append(q.hpBuf, q.lpBuf...) {
			spans = append(spans, e.span)
		}
		q.hpBuf, q.lpBuf = q.hpBuf[:0], q.lpBuf[:0]
		q.eventsLock.Unlock()
	case *VanillaProcessor:
		q.spanLock.Lock()
		for _, e := range q.spanBuf {
			spans = append(spans, e.span)
		}
		q.spanBuf = q.spanBuf[:0]
		q.spanLock.Unlock()
	}
	rs := &tracepb.ResourceSpans{ScopeSpans: []*tracepb.ScopeSpans{{
		Scope: &commonpb.InstrumentationScope{Name: "frontend"}, Spans: spans}}}
	_, _ = proto.Marshal(rs)
}

func requestHarness(kind string, reverse bool) (trace.TracerProvider, sdktrace.SpanProcessor) {
	var inner sdktrace.SpanProcessor
	switch kind {
	case "vanilla":
		inner = &VanillaProcessor{}
	case "pb":
		inner = &PathBridgeProcessor{checkpointRange: checkpointRange{2, 4}}
	case "cgpb":
		inner = &CallGraphBridgeProcessor{checkpointRange: checkpointRange{2, 4}}
	case "sb":
		inner = &StructuralBridgeProcessor{checkpointRange: checkpointRange{2, 4}}
	}
	processor := inner
	if reverse {
		processor = wrapReverseCheckpointProcessor(inner)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(processor))
	var provider trace.TracerProvider = tp
	if preparer, ok := processor.(backend.CheckpointPreparer); ok {
		provider = &checkpointTracerProvider{tp, preparer}
	}
	return provider, inner
}

func benchRequest(b *testing.B, kind string, reverse bool) {
	b.Setenv("REVERSE_TRUSS", map[bool]string{true: "on", false: "off"}[reverse])
	b.Setenv("RT_ROOT", "off")
	b.Setenv("RT_LEAF_REJECT", "1")
	b.Setenv(SampleRatioEnv, "1")
	b.Setenv("RT_SAMPLE", "1000000000") // keep the sampled BRIDGES_CKPT log out of the timing
	tp, inner := requestHarness(kind, reverse)
	retCtx := ""
	if reverse {
		retCtx = backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3,
			backend.SegPathCheckpoint, make([]byte, 19), 2)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		oneFrontendRequest(tp, reverse, retCtx)
		if i%64 == 63 {
			encodeBuffered(inner, nil)
		}
	}
}

func BenchmarkRequestVanilla(b *testing.B)     { benchRequest(b, "vanilla", false) }
func BenchmarkRequestPB(b *testing.B)          { benchRequest(b, "pb", false) }
func BenchmarkRequestPBReverse(b *testing.B)   { benchRequest(b, "pb", true) }
func BenchmarkRequestCGPBReverse(b *testing.B) { benchRequest(b, "cgpb", true) }
func BenchmarkRequestSBReverse(b *testing.B)   { benchRequest(b, "sb", true) }

// Tomislav-RetCtx: arbitrary baggage still propagates on bridge spans. The bridge's own `_br`
// comes from its wire state (backend.AppendSpanBaggage); any other producer's "__bag." attribute
// still reaches the outgoing baggage and "__bagdel." still removes an upstream key.
func TestBridgeSpanKeepsArbitraryAttributeBaggage(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		tp, _ := requestHarness(kind, false)
		parent := backend.SetBaggageInContext(context.Background(), map[string]string{"upstream": "u", "gone": "g"})
		ctx, span := tp.Tracer("t").Start(parent, "client", trace.WithSpanKind(trace.SpanKindClient))
		span.SetAttributes(attribute.String("__bag.custom", "x"), attribute.Int("__bag.n", 7), attribute.Bool("__bagdel.gone", true))
		got := wrapperBaggage(ctx, span, 1)
		w := bridgeWires.load(span.SpanContext().SpanID())
		if w == nil || got[BaggageBRKey] != w.prop || got["custom"] != "x" || got["n"] != "7" || got["upstream"] != "u" || got["__seq"] != "1" {
			t.Fatalf("%s: baggage %v", kind, got)
		}
		if _, ok := got["gone"]; ok {
			t.Fatalf("%s: __bagdel ignored: %v", kind, got)
		}
		// an explicit "__bag._br" attribute still wins over the SDK's entry, as before
		span.SetAttributes(attribute.String("__bag._br", "override"))
		if got := wrapperBaggage(ctx, span, 1); got[BaggageBRKey] != "override" {
			t.Fatalf("%s: attribute baggage must take precedence: %v", kind, got)
		}
		span.End()
	}
}
