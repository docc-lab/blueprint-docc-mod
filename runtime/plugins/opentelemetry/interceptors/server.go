package interceptors

import (
	"context"
	"encoding/binary"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// serverInterceptor is the callee side: it does what the server wrapper templates
// (serverTemplate{Vanilla,Path,CGPB,SBridge} in plugins/opentelemetry/ir_ot_server.go) do around
// the service call, in the same order.  Called by NewServerInterceptor (interceptors.go).
func serverInterceptor(tp trace.TracerProvider, v variant, attrBaggage bool) grpc.UnaryServerInterceptor {
	s := &serverSide{
		tp:          tp,
		v:           v,
		attrBaggage: attrBaggage,
		// REVERSE_TRUSS is fixed for the life of the process, so read it once here rather than on
		// every call (the wrappers call backend.ReverseTrussEnabled, an os.Getenv, per call).
		reverse: backend.ReverseTrussEnabled(),               // runtime/core/backend/reversetruss.go
		methods: &methodCache{tp: tp, side: serverSpanNames}, // interceptors.go
	}
	// The per-call path.  The whole service call runs under its stack frame, including
	// every client call the service makes on the same goroutine, so the frame is kept small: the work
	// before and after the span start and the service happens in helpers whose frames are gone by then
	// (see the client interceptor in client.go for why frame size matters here).  Now it is 256 bytes;
	// one function doing everything was 752, and the generated server wrapper's frame is 888.
	// (The body is in this closure itself, not in a method it calls: a method call, or a method
	// value like c.intercept, would add one more frame under everything the call does.)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, state, sampled := s.prepare(ctx) // below: steps 1-3
		if !sampled {
			// Unsampled upstream: ctx only carries the remote IDs; no span, no tracing work.
			return handler(ctx, req)
		}

		// 4. The server span (started right here, so no helper frame sits under the SDK's work).
		call := serverCall{state: state}
		m := s.methods.get(info.FullMethod) // interceptors.go: span name and tracer, cached per method
		ctx, call.span = m.tracer.Start(ctx, m.spanName, trace.WithSpanKind(trace.SpanKindServer))
		defer call.span.End()

		s.setup(&call, ctx) // below: steps 5-6

		// 7. The service itself.
		resp, err := handler(call.ctx, req)
		s.finish(&call, err) // below: steps 8-9
		return resp, err
	}
}

// serverSide is one server interceptor (one per traced service and process).
type serverSide struct {
	tp          trace.TracerProvider
	v           variant
	attrBaggage bool // the SDK may add "__bag." attributes at span start (mergeSpanBaggage)
	reverse     bool
	methods     *methodCache
}

// serverCall is one traced call: what the steps before the service set up for it and after it.
type serverCall struct {
	ctx   context.Context // the context the service runs with
	span  trace.Span
	state *requestState // bridges: per-request state (child counter, SB end events)
}

// prepare does steps 1-3 for a gRPC call: the carrier comes from the request metadata.
func (s *serverSide) prepare(ctx context.Context) (context.Context, *requestState, bool) {
	return s.prepareCarrier(ctx, incomingTraceCtx(ctx)) // below
}

// prepareCarrier does steps 1-3: decode the caller's carrier ("" if it sent none) and build the
// per-request state the span starts with.  It returns sampled=false for an unsampled trace, in
// which case ctx is all the service needs and state is nil.  Used by prepare (gRPC) and the HTTP
// server middleware (http_server.go).
func (s *serverSide) prepareCarrier(ctx context.Context, carrier string) (context.Context, *requestState, bool) {
	state := &requestState{children: s.v.countChildren, hasEndEvents: s.v.endEvents}

	// 1. The caller's carrier.  An unsampled trace (head sampling) only needs its IDs passed on,
	//    so downstream hops stay in the same trace; no tracing work at all.
	if carrier != "" {
		cfg, upstream, full, err := decodeCarrier(carrier) // carrier.go
		if err == nil && !full {
			cfg.Remote = true
			return trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(cfg)), nil, false
		}
		// 2. The caller's span becomes our remote parent; its baggage comes along (for the span
		//    processor to read when the server span starts).  A carrier that does not decode
		//    leaves cfg empty, so the span starts a new trace, which is what the wrappers do with
		//    JSON that does not parse.
		ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(cfg))
		state.baggage = upstream

		// 3. The caller's child ordinal ("__seq" in the baggage) must be on ctx as "seqNum"
		//    before our span starts, so the bridge processor's OnStart reads the right ordinal.
		if sv, ok := upstream["__seq"]; ok {
			if n, err := strconv.Atoi(sv); err == nil {
				state.seqNum = n // boxed once here, not on every lookup
			}
		}
	}
	state.Context = ctx
	return state, state, true
}

// setup does steps 5-6 after the span has started, and sets call.ctx for the service.
func (s *serverSide) setup(call *serverCall, ctx context.Context) {
	state := call.state
	// 5. Baggage the processor attached to the new span; the merged result must be visible on
	//    ctx BEFORE the service runs, so nested calls carry it.  When the caller sent baggage,
	//    the state already holds that very map and merging updates it in place.
	if state.baggage == nil {
		state.baggage = make(map[string]string)
	}
	mergeSpanBaggage(call.span, state.baggage, s.attrBaggage) // interceptors.go

	// 6. From here on the state also answers the keys the wrappers add after the span started
	//    (child counter, SB end events, return accumulator).
	state.started = true
	call.ctx = ctx
}

// finish does what comes after the service for a gRPC call: steps 8-9, the reverse context going
// back in the response trailer.  The span itself is ended by the interceptor's defer.
func (s *serverSide) finish(call *serverCall, err error) {
	if retCtx := s.finishCall(call, err); retCtx != "" {
		setRetCtxTrailer(call.ctx, retCtx) // below
	}
}

// finishCall does what comes after the service on any transport: record an error, steps 8-9, and
// return the reverse context to send back to the caller ("" for none).  Used by finish (gRPC) and
// the HTTP server middleware (http_server.go).
func (s *serverSide) finishCall(call *serverCall, err error) string {
	if err != nil {
		call.span.RecordError(err)
	}

	// 8. Report the children on the span.
	switch s.v.childAttr {
	case hasChildrenAttr:
		call.span.SetAttributes(attribute.Bool("hasChildren", call.state.childCount.Load() > 0))
	case childCountAttr:
		call.span.SetAttributes(attribute.Int("childCount", int(call.state.childCount.Load())))
	}
	if s.v.endEvents {
		// The wrapper reads the list without the lock (all child calls have returned by now);
		// taking it costs nothing and keeps the race detector quiet.
		call.state.childrenMutex.Lock()
		setRemEndEvents(call.span, int(call.state.childCount.Load()), call.state.endEvents) // below
		call.state.childrenMutex.Unlock()
	}

	// 9. Reverse context back to the caller (bridges with REVERSE_TRUSS=on).
	if s.reverse {
		return serverRetCtx(call.ctx, s.tp, call.span, err) // below
	}
	return ""
}

// requestState is everything a traced server keeps per request, as ONE context value.
//
// The wrappers put each value in its own context.WithValue layer (and the OTel SDK and the bridge
// processors then walk past all of them on every lookup).  This type answers every one of those
// keys itself, with the same values, so the server adds three context layers per call (the remote
// parent, this state, and the span) instead of six:
//
//	key                    value             set by the wrappers   read by
//	backend baggage key    map[string]string before the span       client side, span processors
//	"seqNum"               int               before the span       bridge processors' OnStart
//	                                         (bridges)             (runtime/plugins/otelcol)
//	"childCount"           *atomic.Uint64    after the span        client side (client.go, wrappers)
//	                                         (bridges)
//	"endEvents"            *[]int            after the span (SB)   client side, SB processor's
//	"childrenMutex"        *sync.Mutex       after the span (SB)   OnStart (sb_processor.go)
//	backend retMerge key   *backend.RetMerge after the span        backend.AddToMerge, MergedChildren
//
// The keys the wrappers add after the span started are only answered once started is set (in
// setup), so the span processors see exactly the same context at span start as with wrappers.  The
// two backend keys are unexported; backend.IsBaggageKey and backend.IsRetMergeKey recognise them
// (runtime/core/backend/trace.go, reversetruss.go).
type requestState struct {
	context.Context
	baggage       map[string]string
	seqNum        any // the caller's child ordinal as an int, nil if none
	childCount    atomic.Uint64
	endEvents     []int
	childrenMutex sync.Mutex
	merge         backend.RetMerge
	children      bool // bridges: answer "childCount"
	hasEndEvents  bool // SB: answer "endEvents" and "childrenMutex"
	started       bool // the server span has started (set in setup)
}

// Value answers the keys above and passes every other key on to the parent context.
func (s *requestState) Value(key any) any {
	if k, ok := key.(string); ok {
		switch {
		case k == "seqNum":
			if s.seqNum != nil {
				return s.seqNum
			}
		case !s.started:
			// the remaining string keys only exist once the span has started
		case k == "childCount" && s.children:
			return &s.childCount
		case k == "endEvents" && s.hasEndEvents:
			return &s.endEvents
		case k == "childrenMutex" && s.hasEndEvents:
			return &s.childrenMutex
		}
		return s.Context.Value(key)
	}
	if s.baggage != nil && backend.IsBaggageKey(key) { // runtime/core/backend/trace.go
		return s.baggage
	}
	if s.started && backend.IsRetMergeKey(key) { // runtime/core/backend/reversetruss.go
		return &s.merge
	}
	return s.Context.Value(key)
}

// incomingTraceCtx reads the forward carrier from the request metadata ("" if there is none).
// metadata.ValueFromIncomingContext copies only this one key; metadata.FromIncomingContext would
// copy every request header into a new map on every call.
func incomingTraceCtx(ctx context.Context) string {
	if v := metadata.ValueFromIncomingContext(ctx, TraceCtxKey); len(v) > 0 { // TraceCtxKey: interceptors.go
		return v[0]
	}
	return ""
}

// setRemEndEvents writes the SB "delayed orthogonal truss" summary that the SB processor reads at
// span end: varint(children) || varint(count) || count x varint(start ordinal), the ordinals in the
// order the children ENDED, dropping the last one (it is implied at reconstruction).  The raw bytes
// in a string (OTel attributes have no []byte type): the processor reads them in this process and
// never exports the attribute.  Same encoding as serverTemplateSBridge.
func setRemEndEvents(span trace.Span, children int, endEvents []int) {
	n := len(endEvents)
	if n == 0 {
		return
	}
	kept := endEvents[:n-1]
	buf := make([]byte, 0, 8+5*len(kept))
	buf = binary.AppendUvarint(buf, uint64(children))
	buf = binary.AppendUvarint(buf, uint64(len(kept)))
	for _, s := range kept {
		buf = binary.AppendUvarint(buf, uint64(s))
	}
	span.SetAttributes(attribute.String("remEndEvents", string(buf)))
}

// serverRetCtx computes this server's reverse context after the handler returned, exactly as the
// wrappers do: merged children, then the SDK's checkpoint preparation on the live server span.  It
// returns it only if the call succeeded ("" otherwise): with wrappers, a failed call returns no
// response message, so the caller never sees retCtx either.
func serverRetCtx(ctx context.Context, tp trace.TracerProvider, span trace.Span, callErr error) string {
	retCtx := backend.MergedChildren(ctx)                                       // runtime/core/backend/reversetruss.go
	if out, prepared := backend.PrepareCheckpoint(tp, span, retCtx); prepared { // runtime/core/backend/checkpoint.go
		retCtx = out
	}
	if callErr != nil {
		return ""
	}
	return retCtx
}

// setRetCtxTrailer puts the reverse context in the gRPC response trailer.
func setRetCtxTrailer(ctx context.Context, retCtx string) {
	// The trailer map comes from a pool: grpc.SetTrailer copies it into the stream's trailer
	// (metadata.Join), so it can be reused as soon as SetTrailer returns.  (A map literal instead of
	// metadata.Pairs: Pairs lower-cases the key on every call; RetCtxKey already is.)
	md := trailerMDs.Get().(metadata.MD)
	md[RetCtxKey] = append(md[RetCtxKey][:0], retCtxHeader(retCtx)) // RetCtxKey: interceptors.go; retCtxHeader: header.go
	grpc.SetTrailer(ctx, md)
	md[RetCtxKey][0] = "" // do not keep the string alive in the pool
	trailerMDs.Put(md)
}

// trailerMDs holds reusable trailer maps for setRetCtxTrailer.
var trailerMDs = sync.Pool{New: func() any { return metadata.MD{} }}
