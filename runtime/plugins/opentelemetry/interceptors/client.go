package interceptors

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// clientInterceptor is the caller side: it does what the client wrapper templates
// (clientSideTemplate{Vanilla,Path,CGPB,SBridge} in plugins/opentelemetry/ir_ot_client.go) do around
// the remote call, in the same order.  Called by NewClientInterceptor (interceptors.go).
func clientInterceptor(tp trace.TracerProvider, v variant, attrBaggage bool) grpc.UnaryClientInterceptor {
	c := &clientSide{
		tp:          tp,
		v:           v,
		attrBaggage: attrBaggage,
		// Only servers with REVERSE_TRUSS=on send a reverse context back (server.go), and the
		// setting is the same for every process of a deployment and fixed while it runs.  So read
		// it once, and only ask gRPC for the response trailer when there can be something in it:
		// collecting the trailer costs an extra call option and a copy of the trailer on every call.
		reverse: backend.ReverseTrussEnabled(),               // runtime/core/backend/reversetruss.go
		methods: &methodCache{tp: tp, side: clientSpanNames}, // interceptors.go
	}
	// The per-call path.
	//
	// Its stack frame stays on the stack for the whole downstream call, and while the OTel SDK and the
	// bridge processor start the span.  So it is kept small and does little itself: the work before and
	// after the span start and the call happens in helpers whose frames are gone by then.  Why it
	// matters: with interceptors the span starts below three gRPC/Blueprint frames that sit above it with
	// wrappers (Blueprint's client, the gRPC stub, ClientConn.Invoke: about 400 bytes).  When everything
	// was in one function, this frame was 688 bytes and SB's goroutines grew (copied) their stacks
	// noticeably more often than with wrappers (README, "Profiling").  Now it is 288 bytes; the
	// generated client wrapper's frame is 760.  (go build -gcflags=-S prints frame sizes as "locals".)
	// (The body is in this closure itself, not in a method it calls: a method call, or a method
	// value like c.intercept, would add one more frame under everything the call does.)
	return func(ctx context.Context, fullMethod string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 1. Unsampled parent: forward only the compact carrier so downstream stays unsampled.
		if unsampledParent(ctx) { // below
			return invokeUnsampled(ctx, fullMethod, req, reply, cc, invoker, opts) // below
		}

		var call clientCall
		ctx, baggage := c.prepare(&call, ctx) // below: steps 2-3

		// 4. The client span (started right here, so no helper frame sits under the SDK's work).
		m := c.methods.get(fullMethod) // interceptors.go: span name and tracer, cached per method
		ctx, call.span = m.tracer.Start(ctx, m.spanName, trace.WithSpanKind(trace.SpanKindClient))
		defer call.span.End()

		call.ctx = carry(ctx, call.span, baggage, c.attrBaggage) // below: step 5
		putBaggage(baggage)                                      // below: the map was only needed for the carrier

		// 6. The call.
		var err error
		if c.reverse {
			err = c.invokeWithTrailer(&call, fullMethod, req, reply, cc, invoker, opts) // below: steps 6-7
		} else {
			err = invoker(call.ctx, fullMethod, req, reply, cc, opts...)
		}
		c.finish(&call, err) // below: step 8
		return err
	}
}

// clientSide is one client interceptor (one per traced service and process).
type clientSide struct {
	tp          trace.TracerProvider
	v           variant
	attrBaggage bool // the SDK may add "__bag." attributes at span start (mergeSpanBaggage)
	reverse     bool
	methods     *methodCache
}

// clientCall is one traced call: what the steps before the call set up for the call and after it.
type clientCall struct {
	ctx           context.Context // the context the call is made with (client span, carrier)
	span          trace.Span
	seqNum        int         // bridges: this call's child ordinal
	endEvents     *[]int      // SB: the enclosing server's end-event list ...
	childrenMutex *sync.Mutex // ... and its mutex
}

// prepare does steps 2-3 and returns the context to start the span with and the baggage to send.
func (c *clientSide) prepare(call *clientCall, ctx context.Context) (context.Context, map[string]string) {
	// 2. Copy so we never change the parent's baggage map.
	baggage := copyBaggage(ctx) // below

	// 3. Bridges: take the next child ordinal from the enclosing server's counter.  Like the
	//    wrappers, this requires the caller to run inside a traced server (which sets the counter).
	if c.v.countChildren {
		// Tomislav-RetCtx: checked type assertions, so a call from outside a traced request (no
		// server put the counter on ctx: BUG_REPORT.md F-08) panics naming the rule it broke, in
		// both versions, instead of with a bare type-assertion error.
		childCount, ok := ctx.Value("childCount").(*atomic.Uint64)
		if !ok {
			brokenValue("a bridge client call runs inside a traced request (child counter on the context)", "kind", c.v.name) // invariants.go
		}
		if c.v.endEvents {
			if call.endEvents, ok = ctx.Value("endEvents").(*[]int); !ok {
				backend.InvariantBroken("an SB client call runs inside a traced request (end-event list on the context)")
			}
			if call.childrenMutex, ok = ctx.Value("childrenMutex").(*sync.Mutex); !ok {
				backend.InvariantBroken("an SB client call runs inside a traced request (end-event mutex on the context)")
			}
		}
		call.seqNum = int(childCount.Add(1))
		if call.seqNum < 1 { // Tomislav-RetCtx: the first child is 1; int(uint64) wraps past MaxInt
			brokenCount("a child ordinal is >= 1", "seqNum", call.seqNum, 1)
		}
		ctx = context.WithValue(ctx, "seqNum", call.seqNum) // read by the processor when our span starts
		baggage["__seq"] = strconv.Itoa(call.seqNum)        // read by the server on the other side
	}
	return ctx, baggage
}

// carry does step 5 for a gRPC call: the carrier goes in the request metadata of the returned context.
func carry(ctx context.Context, span trace.Span, baggage map[string]string, attrBaggage bool) context.Context {
	return withOutgoingTraceCtx(ctx, carrierFor(span, baggage, attrBaggage)) // below
}

// carrierFor does step 5 on any transport: merge the baggage the processor attached to the new span
// and encode the carrier for the callee.  Used by carry (gRPC) and the HTTP client middleware
// (http_client.go).
func carrierFor(span trace.Span, baggage map[string]string, attrBaggage bool) string {
	mergeSpanBaggage(span, baggage, attrBaggage)      // interceptors.go
	checkClientSpan(span)                             // Tomislav-RetCtx: invariants.go
	return encodeCarrier(span.SpanContext(), baggage) // carrier.go
}

// invokeWithTrailer makes the call collecting the server's trailer, which carries the reverse
// context (step 6), and handles the reverse context (step 7).
func (c *clientSide) invokeWithTrailer(call *clientCall, fullMethod string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts []grpc.CallOption) error {
	t := trailerCatchers.Get().(*trailerCatcher) // below
	t.opts = append(append(t.opts[:0], opts...), t.opt)
	err := invoker(call.ctx, fullMethod, req, reply, cc, t.opts...)
	retCtx := ""
	if err == nil {
		retCtx = retCtxFromTrailer(t.md) // below
	}
	// gRPC is done with the options and the trailer once the call returned: put them back.
	t.md = nil
	clear(t.opts)
	trailerCatchers.Put(t)

	// 7. With wrappers a failed call returns no retCtx; the server does not send one then either,
	//    so only look on success.
	if err == nil {
		finishClientRetCtx(call.ctx, c.tp, call.span, retCtx) // below
	}
	return err
}

// A trailerCatcher collects one call's response trailer.  They are reused through a pool: building
// a grpc.Trailer call option, the variable it points to, and a call-option slice one longer than the
// caller's on every call costs three allocations per call.
type trailerCatcher struct {
	md   metadata.MD
	opt  grpc.CallOption   // grpc.TrailerCallOption{TrailerAddr: &md}, made once per catcher
	opts []grpc.CallOption // scratch: the call's options plus opt
}

var trailerCatchers = sync.Pool{New: func() any {
	t := &trailerCatcher{}
	t.opt = grpc.TrailerCallOption{TrailerAddr: &t.md}
	return t
}}

// finish does what comes after the call (step 8); the span itself is ended by intercept's defer.
func (c *clientSide) finish(call *clientCall, err error) {
	if err != nil {
		call.span.RecordError(err)
	}
	// 8. SB: record that this child ended, by its start ordinal, in the order children end.
	if c.v.endEvents {
		call.childrenMutex.Lock()
		*call.endEvents = append(*call.endEvents, call.seqNum)
		call.childrenMutex.Unlock()
	}
}

// unsampledParent says whether ctx carries a span that head sampling dropped.
func unsampledParent(ctx context.Context) bool {
	psc := trace.SpanContextFromContext(ctx)
	return psc.IsValid() && !psc.IsSampled()
}

// invokeUnsampled makes a call under an unsampled parent: no span, only the compact carrier so the
// callee stays in the same (unsampled) trace.
func invokeUnsampled(ctx context.Context, fullMethod string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts []grpc.CallOption) error {
	carrier := encodeUnsampledCarrier(trace.SpanContextFromContext(ctx)) // carrier.go
	return invoker(withOutgoingTraceCtx(ctx, carrier), fullMethod, req, reply, cc, opts...)
}

// withOutgoingTraceCtx attaches the forward carrier to the outgoing request metadata.
func withOutgoingTraceCtx(ctx context.Context, carrier string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, TraceCtxKey, carrier) // TraceCtxKey: interceptors.go
}

// copyBaggage returns a copy of the baggage on ctx (empty map if none), so the caller can change it
// without touching the parent's map.  The copy is only used to build this call's carrier and is
// never stored anywhere (the span baggage source in runtime/plugins/otelcol/wire_state.go only adds
// entries to it), so the maps come from a pool; putBaggage returns them.
func copyBaggage(ctx context.Context) map[string]string {
	baggage := baggageMaps.Get().(map[string]string)
	for k, v := range backend.GetBaggageFromContext(ctx) { // runtime/core/backend/trace.go
		baggage[k] = v
	}
	return baggage
}

// putBaggage empties a map from copyBaggage and returns it to the pool.
func putBaggage(baggage map[string]string) {
	clear(baggage)
	baggageMaps.Put(baggage)
}

var baggageMaps = sync.Pool{New: func() any { return make(map[string]string, 8) }}

// retCtxFromTrailer reads the reverse context from the response trailer ("" if there is none).
// Indexes the map directly: gRPC stores received keys in lower case, and MD.Get would lower-case
// RetCtxKey (already lower case) on every call.
func retCtxFromTrailer(trailer metadata.MD) string {
	if v := trailer[RetCtxKey]; len(v) > 0 { // RetCtxKey: interceptors.go
		if len(v) > 1 { // Tomislav-RetCtx: the server sets exactly one (server.go, setRetCtxTrailer)
			brokenCount("one reverse context per response", "values", len(v), 1)
		}
		return retCtxFromHeader(v[0]) // header.go
	}
	return ""
}

// finishClientRetCtx handles the reverse context a call returned, exactly as the client wrappers do:
// hand it to the live client span for the SDK's checkpoint preparation, then add what is left to the
// enclosing server's fan-in.
func finishClientRetCtx(ctx context.Context, tp trace.TracerProvider, span trace.Span, retCtx string) {
	if retCtx == "" {
		return
	}
	if out, prepared := backend.PrepareCheckpoint(tp, span, retCtx); prepared { // runtime/core/backend/checkpoint.go
		retCtx = out
	}
	backend.AddToMerge(ctx, retCtx) // runtime/core/backend/reversetruss.go
}
