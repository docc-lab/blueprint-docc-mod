package interceptors

// Tomislav-RetCtx (user 2026-11-06): each tracing invariant the interceptors and the HTTP middleware
// check panics with a *backend.InvariantError when it is broken (mustBreakInvariant: header_test.go),
// and the input a traced caller really sends passes.

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

var validSC = trace.NewSpanContext(trace.SpanContextConfig{
	TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, TraceFlags: trace.FlagsSampled,
})

func TestInvariantBridgeKind(t *testing.T) {
	for _, kind := range []string{"v", "vanilla", "pb", "path", "rc", "", "cgpb", "sb"} {
		variantFor(kind) // must not panic
	}
	mustBreakInvariant(t, "unknown kind", func() { variantFor("sbridge") })
}

func TestInvariantForwardCarrier(t *testing.T) {
	pb := &serverSide{v: variantFor("pb")}
	van := &serverSide{v: variantFor("v")}
	ctx := context.Background()

	// What a bridge caller sends passes, and so does no carrier at all (a request from outside).
	pb.prepare(ctx, encodeCarrier(validSC, map[string]string{"__seq": "1"}))
	pb.prepare(ctx, "")
	van.prepare(ctx, encodeCarrier(validSC, map[string]string{})) // vanilla callers send no ordinal

	mustBreakInvariant(t, "carrier that does not decode", func() { pb.prepare(ctx, "ab cd") })
	mustBreakInvariant(t, "unsampled carrier with zero IDs", func() { pb.prepare(ctx, encodeUnsampledCarrier(trace.SpanContext{})) })
	mustBreakInvariant(t, "bridge carrier without __seq", func() { pb.prepare(ctx, encodeCarrier(validSC, map[string]string{})) })
	mustBreakInvariant(t, "__seq 0", func() { pb.prepare(ctx, encodeCarrier(validSC, map[string]string{"__seq": "0"})) })
	mustBreakInvariant(t, "__seq not a number", func() { pb.prepare(ctx, encodeCarrier(validSC, map[string]string{"__seq": "x"})) })
}

func TestInvariantClientInsideTracedRequest(t *testing.T) {
	ctx := context.Background()
	(&clientSide{v: variantFor("v")}).prepare(&clientCall{}, ctx) // vanilla counts no children: fine

	mustBreakInvariant(t, "bridge call outside a traced request (F-08)", func() {
		(&clientSide{v: variantFor("pb")}).prepare(&clientCall{}, ctx)
	})
	mustBreakInvariant(t, "SB call with a counter but no end-event list", func() {
		(&clientSide{v: variantFor("sb")}).prepare(&clientCall{}, context.WithValue(ctx, "childCount", &atomic.Uint64{}))
	})
}

func TestInvariantMethodNames(t *testing.T) {
	newMethodInfo(trace.NewNoopTracerProvider(), serverSpanNames, "Svc", "Do")
	mustBreakInvariant(t, "no method name", func() { newMethodInfo(trace.NewNoopTracerProvider(), clientSpanNames, "Svc", "") })
	mustBreakInvariant(t, "no service name", func() { newMethodInfo(trace.NewNoopTracerProvider(), serverSpanNames, "", "Do") })
}

func TestInvariantOneValuePerHeader(t *testing.T) {
	in := metadata.NewIncomingContext(context.Background(), metadata.MD{TraceCtxKey: {"a", "b"}})
	mustBreakInvariant(t, "two forward carriers", func() { incomingTraceCtx(in) })
	mustBreakInvariant(t, "two reverse contexts", func() { retCtxFromTrailer(metadata.MD{RetCtxKey: {"a", "b"}}) })
	mustBreakInvariant(t, "two HTTP header values", func() { headerValue(http.Header{traceCtxHeader: {"a", "b"}}, traceCtxHeader) })
	if got := headerValue(http.Header{traceCtxHeader: {"a"}}, traceCtxHeader); got != "a" {
		t.Errorf("headerValue = %q", got)
	}
}

func TestInvariantEndEventsBound(t *testing.T) {
	sb := &serverSide{v: variantFor("sb")}
	span := trace.SpanFromContext(context.Background()) // a no-op span
	state := &requestState{}
	state.childCount.Store(1)
	state.endEvents = []int{1}
	sb.finish(&serverCall{span: span, state: state}, nil) // one child, one end event: fine
	state.endEvents = []int{1, 1}
	mustBreakInvariant(t, "more end events than children", func() { sb.finish(&serverCall{span: span, state: state}, nil) })
}

func TestInvariantReverseContextSent(t *testing.T) {
	span := trace.SpanFromContext(context.Background())
	tp := trace.NewNoopTracerProvider() // no checkpoint preparer: the merged children go back as they are
	packed := backend.EncodeCheckpointRetCtx(trace.SpanID{9}, 1, backend.SegPathCheckpoint, nil)
	ok := backend.WithRetMerge(context.Background())
	backend.AddToMerge(ok, packed)
	if got := serverRetCtx(ok, tp, span, 0, nil); got != packed {
		t.Errorf("serverRetCtx = %q, want the merged children", got)
	}
	bad := backend.WithRetMerge(context.Background())
	backend.AddToMerge(bad, "not packed")
	mustBreakInvariant(t, "unpacked reverse context", func() { serverRetCtx(bad, tp, span, 0, nil) })
}
