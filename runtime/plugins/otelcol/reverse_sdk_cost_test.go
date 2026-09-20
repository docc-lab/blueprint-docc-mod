package otelcol

// Tomislav-RetCtx: where does the response path's application CPU actually go?
//
// Measured on the cluster, PB at matched pre-knee load, application CPU per
// completed request: 19.14 cores with REVERSE_TRUSS=off, 22.57 with it on (old
// encoding), 21.50 with the packed encoding. So the wire format accounts for
// roughly a quarter to a third of the overhead and something else carries the
// rest. Collector CPU is ~1.2 cores across all eight in every mode, so the cost
// is in the application, not downstream.
//
// The suspect is attribute bookkeeping rather than anything on the wire. In the
// OTel SDK, ReadWriteSpan.Attributes() is not a cheap read:
//
//	func (s *recordingSpan) Attributes() []attribute.KeyValue {
//	    s.mu.Lock(); defer s.mu.Unlock()
//	    s.dedupeAttrs()
//	    return s.attributes
//	}
//
// every call takes the span mutex and runs a dedupe pass. The generated client
// wrapper's per-span reverse sequence calls it several times over: OnStart scans
// once for AttrBREmit and again inside isScheduledPathCheckpoint, then on the
// response PrepareCheckpoint scans once more and spanHasChildren again, and
// ReadReverseBaggage scans a final time looking for a value PrepareCheckpoint
// itself just wrote. Interleaved are three to five locked SetAttributes calls.
//
// This benchmark replays that exact sequence against a real recording span, with
// the reverse wrapper installed and without it, so the per-span delta is
// measured rather than argued. It is the SDK-side analogue of the wire-format
// benchmark in runtime/core/backend/retctx_cost_test.go.
//
// Run: RETCTX_COST=1 go test ./runtime/plugins/otelcol -run XXX \
//        -bench BenchmarkReverseSpan -benchmem -benchtime 200000x

import (
	"context"
	"os"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func skipUnlessSDKCost(tb testing.TB) {
	if os.Getenv("RETCTX_COST") == "" {
		tb.Skip("set RETCTX_COST=1")
	}
}

// reverseSpanHarness builds a provider whose PB processor is optionally wrapped
// by the reverse checkpoint processor, exactly as wrapReverseCheckpointProcessor
// decides at startup from REVERSE_TRUSS.
func reverseSpanHarness(reverse bool) (trace.TracerProvider, context.Context) {
	var processor sdktrace.SpanProcessor = &PathBridgeProcessor{checkpointDistance: 3}
	if reverse {
		processor = wrapReverseCheckpointProcessor(processor)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(processor))
	var provider trace.TracerProvider = tp
	if preparer, ok := processor.(backend.CheckpointPreparer); ok {
		provider = &checkpointTracerProvider{tp, preparer}
	}
	return provider, checkpointParentContext("pb", 3)
}

// oneClientSpan replays what the generated client wrapper does per span.
func oneClientSpan(tp trace.TracerProvider, parent context.Context, reverse bool, retCtx string) {
	ctx, span := tp.Tracer("bench").Start(parent, "ClientCall", trace.WithSpanKind(trace.SpanKindClient))
	_ = ctx
	if reverse && retCtx != "" {
		// plugins/opentelemetry/ir_ot_client.go: hand the response to the live span,
		// let the SDK route it, then read back whatever was not consumed.
		span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, retCtx))
		// The wrapper now takes the carrier from the return value instead of
		// re-scanning the span for it.
		if out, prepared := backend.PrepareCheckpoint(tp, span, retCtx); prepared {
			_ = out
		}
	}
	span.End()
}

func benchReverseSpan(b *testing.B, reverse bool) {
	skipUnlessSDKCost(b)
	tp, parent := reverseSpanHarness(reverse)
	// A returned truss of the size the cluster actually carries: one packed
	// checkpoint segment.
	retCtx := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3,
		backend.SegPathCheckpoint, make([]byte, 19), 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		oneClientSpan(tp, parent, reverse, retCtx)
	}
}

func BenchmarkReverseSpanOff(b *testing.B) { benchReverseSpan(b, false) }
func BenchmarkReverseSpanOn(b *testing.B)  { benchReverseSpan(b, true) }

// BenchmarkReverseSpanAttributesOnly isolates the attribute-access cost alone:
// how much does one extra Attributes() scan cost on a span of realistic width?
func BenchmarkReverseSpanAttributesOnly(b *testing.B) {
	skipUnlessSDKCost(b)
	tp, parent := reverseSpanHarness(true)
	_, span := tp.Tracer("bench").Start(parent, "ClientCall", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	readable, ok := span.(interface {
		Attributes() []attribute.KeyValue
	})
	if !ok {
		b.Skip("span is not readable")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = readable.Attributes()
	}
}

// Tomislav-RetCtx: the deployed profile showed OnStart RISING on composepost
// (11.10s -> 14.59s) after per-span reverse state moved off the span and into a
// sync.Map, while the other three services were flat or better. composepost is the
// busiest and most concurrent service, so the suspicion is contention on the map
// rather than the map lookup itself. This benchmark reproduces that shape locally:
// many goroutines each starting and preparing a span, which is exactly the fan-out
// pattern composepost runs.
//
//	Run: RETCTX_COST=1 go test ./runtime/plugins/otelcol -run XXX \
//	       -bench BenchmarkReverseSpanParallel -benchmem -cpu 1,8,40
func BenchmarkReverseSpanParallel(b *testing.B) {
	skipUnlessSDKCost(b)
	tp, parent := reverseSpanHarness(true)
	retCtx := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3,
		backend.SegPathCheckpoint, make([]byte, 19), 2)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			oneClientSpan(tp, parent, true, retCtx)
		}
	})
}

func BenchmarkReverseSpanParallelOff(b *testing.B) {
	skipUnlessSDKCost(b)
	tp, parent := reverseSpanHarness(false)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			oneClientSpan(tp, parent, false, "")
		}
	})
}
