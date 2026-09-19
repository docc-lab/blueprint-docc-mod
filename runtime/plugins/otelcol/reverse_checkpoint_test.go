package otelcol

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Run real span processing and serialization with no exporter or background
// workers. The tests inspect exactly what would enter each export buffer.
func checkpointTestProvider(kind string) (trace.TracerProvider, func() (hp, lp []*tracepb.Span)) {
	buffered := func(entries []sbBufEntry) []*tracepb.Span {
		var spans []*tracepb.Span
		for _, entry := range entries {
			spans = append(spans, entry.span)
		}
		return spans
	}
	var next sdktrace.SpanProcessor
	var snapshot func() (hp, lp []*tracepb.Span)
	switch kind {
	case "pb":
		p := &PathBridgeProcessor{checkpointDistance: 3}
		next, snapshot = p, func() ([]*tracepb.Span, []*tracepb.Span) { return buffered(p.hpBuf), buffered(p.lpBuf) }
	case "cgpb":
		p := &CallGraphBridgeProcessor{checkpointDistance: 3}
		next, snapshot = p, func() ([]*tracepb.Span, []*tracepb.Span) { return buffered(p.hpBuf), buffered(p.lpBuf) }
	case "sb":
		p := &StructuralBridgeProcessor{checkpointDistance: 3}
		next, snapshot = p, func() ([]*tracepb.Span, []*tracepb.Span) { return buffered(p.hpBuf), buffered(p.lpBuf) }
	default:
		p := &VanillaProcessor{}
		next, snapshot = p, func() ([]*tracepb.Span, []*tracepb.Span) {
			var spans []*tracepb.Span
			for _, entry := range p.spanBuf {
				spans = append(spans, entry.span)
			}
			return spans, nil
		}
	}
	processor := wrapReverseCheckpointProcessor(next)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(headSampler()), sdktrace.WithSpanProcessor(processor))
	if preparer, ok := processor.(backend.CheckpointPreparer); ok {
		return &checkpointTracerProvider{tp, preparer}, snapshot
	}
	return tp, snapshot
}

func checkpointParentContext(kind string, depth int) context.Context {
	var br []byte
	bloomBytes := make([]byte, (BloomFilterM+7)/8)
	switch kind {
	case "pb":
		br = packPathBridgeBR(depth, [8]byte{}, bloomBytes)
	case "cgpb":
		br = packCGPRBBR(depth, [8]byte{}, bloomBytes, nil)
	case "sb":
		br = packStructuralBR(depth, [8]byte{}, 3, checkpointBlooms[2].emptyBytes(), nil, structuralTail{})
	}
	return backend.SetBaggageInContext(context.Background(), map[string]string{
		reverseDepthBaggageKey: strconv.Itoa(depth), BaggageBRKey: encodeBR(br),
	})
}

func TestSDKReturnsCheckpointWithoutDroppingSpan(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			provider, buffered := checkpointTestProvider(kind)
			_, span := provider.Tracer("test").Start(checkpointParentContext(kind, 129), "leaf", trace.WithSpanKind(trace.SpanKindServer))
			var expectedTruss []byte
			for _, attr := range span.(sdktrace.ReadWriteSpan).Attributes() {
				if attr.Key == AttrBREmit {
					expectedTruss, _ = decodeBR(attr.Value.AsString())
				}
			}
			if backend.ReadReverseBaggage(span) != "" {
				t.Fatal("checkpoint decided before final span attributes were available")
			}
			backend.PrepareCheckpoint(provider, span)
			retCtx := backend.ReadReverseBaggage(span)
			if retCtx == "" || !span.IsRecording() {
				t.Fatal("SDK must publish __bag.rev while the span is still mutable")
			}
			checkpoints, err := backend.DecodeReturnedCheckpoints(retCtx)
			if err != nil || len(checkpoints) != 1 {
				t.Fatalf("invalid reverse payload: %v, %v", checkpoints, err)
			}
			cp := checkpoints[0]
			if cp.SpanID != span.SpanContext().SpanID() || cp.Depth != 130 || !bytes.Equal(cp.Truss, expectedTruss) || cp.ReverseTTL == nil || *cp.ReverseTTL != 2 {
				t.Fatalf("wrong checkpoint location or truss: %+v", cp)
			}
			backend.PrepareCheckpoint(provider, span)
			if backend.ReadReverseBaggage(span) != retCtx {
				t.Fatal("repeated preparation changed the result")
			}
			span.End()
			hp, lp := buffered()
			all := append(hp, lp...)
			if len(all) != 1 {
				t.Fatalf("checkpoint refusal dropped or duplicated the span: %d exported", len(all))
			}
			sid := span.SpanContext().SpanID()
			if !bytes.Equal(all[0].SpanId, sid[:]) {
				t.Fatal("exported span lost its own ID")
			}
			if kind != "v" && (len(hp) != 0 || len(lp) != 1) {
				t.Fatalf("declined checkpoint must use the ordinary span pipeline: HP=%d LP=%d", len(hp), len(lp))
			}
			for _, attr := range all[0].Attributes {
				if attr.Key == AttrBREmit || strings.HasPrefix(attr.Key, "__bag.") {
					t.Fatalf("exported declined checkpoint/internal baggage: %s", attr.Key)
				}
			}
		})
	}
}

func TestSDKCheckpointFallbacks(t *testing.T) {
	for _, tc := range []struct{ name, enabled, root, rate, sample string }{
		{"disabled", "off", "off", "1", "1"},
		{"root", "on", "on", "1", "1"},
		{"accept", "on", "off", "0", "1"},
		{"unsampled", "on", "off", "1", "0"},
		{"invalid_rate", "on", "off", "invalid", "1"},
		{"nan_rate", "on", "off", "NaN", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REVERSE_TRUSS", tc.enabled)
			t.Setenv("RT_ROOT", tc.root)
			t.Setenv("RT_LEAF_REJECT", tc.rate)
			t.Setenv(SampleRatioEnv, tc.sample)
			provider, buffered := checkpointTestProvider("pb")
			_, span := provider.Tracer("test").Start(context.Background(), "leaf", trace.WithSpanKind(trace.SpanKindServer))
			backend.PrepareCheckpoint(provider, span)
			if backend.ReadReverseBaggage(span) != "" {
				t.Fatal("unexpected reverse checkpoint")
			}
			span.End()
			hp, lp := buffered()
			want := 1
			if tc.sample == "0" {
				want = 0
			}
			if len(hp) != want || len(lp) != 0 {
				t.Fatalf("unexpected export classification: HP=%d LP=%d", len(hp), len(lp))
			}
		})
	}
}

func TestSDKCheckpointDepthWithoutAuxiliaryBaggage(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb"} {
		t.Run(kind, func(t *testing.T) {
			ctx := checkpointParentContext(kind, 129)
			delete(backend.GetBaggageFromContext(ctx), reverseDepthBaggageKey)
			provider, _ := checkpointTestProvider(kind)
			_, span := provider.Tracer("test").Start(ctx, "leaf", trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()
			backend.PrepareCheckpoint(provider, span)
			cps, err := backend.DecodeReturnedCheckpoints(backend.ReadReverseBaggage(span))
			if err != nil || len(cps) != 1 || cps[0].Depth != 130 {
				t.Fatalf("checkpoint location differs from the SDK's absolute depth: %+v, %v", cps, err)
			}
		})
	}
}

func TestSDKDoesNotRejectOrdinarySpanCheckpoint(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			provider, buffered := checkpointTestProvider(kind)
			_, span := provider.Tracer("test").Start(checkpointParentContext(kind, 0), "interior", trace.WithSpanKind(trace.SpanKindServer))
			span.SetAttributes(attribute.Bool("hasChildren", true), attribute.Int("childCount", 1), attribute.Int("eventCount", 2))
			backend.PrepareCheckpoint(provider, span)
			if backend.ReadReverseBaggage(span) != "" {
				t.Fatal("created a reverse checkpoint for an ordinary span")
			}
			span.End()
			hp, lp := buffered()
			if len(hp) != 0 || len(lp) != 1 {
				t.Fatalf("ordinary span changed: HP=%d LP=%d", len(hp), len(lp))
			}
		})
	}
}

func TestSDKReverseCheckpointConcurrentRequests(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	provider, buffered := checkpointTestProvider("pb")
	const requests = 64
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, span := provider.Tracer("test").Start(checkpointParentContext("pb", 3*i), "leaf", trace.WithSpanKind(trace.SpanKindServer))
			backend.PrepareCheckpoint(provider, span)
			cps, err := backend.DecodeReturnedCheckpoints(backend.ReadReverseBaggage(span))
			if err != nil || len(cps) != 1 || cps[0].SpanID != span.SpanContext().SpanID() || cps[0].Depth != uint64(3*i+1) {
				t.Errorf("request %d received the wrong checkpoint: %+v, %v", i, cps, err)
			}
			span.End()
		}(i)
	}
	wg.Wait()
	hp, lp := buffered()
	if len(hp) != 0 || len(lp) != requests {
		t.Fatalf("concurrent requests lost spans: HP=%d LP=%d", len(hp), len(lp))
	}
}

func TestSDKUpstreamReverseCheckpointPriority(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			provider, buffered := checkpointTestProvider(kind)
			_, span := provider.Tracer("test").Start(checkpointParentContext(kind, 0), "caller", trace.WithSpanKind(trace.SpanKindClient))
			retCtx := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 130, backend.SegPathCheckpoint, []byte{9, 8, 7}, 0)
			span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, retCtx))
			if !backend.PrepareCheckpoint(provider, span) || backend.ReadReverseBaggage(span) != "" {
				t.Fatal("SDK did not consume the received trusses before span end")
			}
			span.End()
			hp, lp := buffered()
			if len(hp) != 1 || len(lp) != 0 {
				t.Fatalf("upstream reverse checkpoint lacks checkpoint priority: HP=%d LP=%d", len(hp), len(lp))
			}
			var exported string
			for _, attr := range hp[0].Attributes {
				if attr.Key == backend.ReverseTrussCheckpointKey {
					exported = attr.Value.GetStringValue()
				}
			}
			if exported != retCtx {
				t.Fatal("upstream checkpoint export lost the returned truss")
			}
		})
	}
}
