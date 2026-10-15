package interceptors

import (
	"context"
	"net/http"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/httpmiddleware"
	"go.opentelemetry.io/otel/trace"
)

// Tomislav-RetCtx: the tracing middleware for services deployed with the HTTP plugin
// (runtime/plugins/httpmiddleware), the HTTP counterpart of the gRPC interceptors.  The steps are the
// gRPC ones (server.go, client.go); only where the two values travel differs:
//
//	forward context (span context + baggage):  request header   "Bp-Trace-Ctx"
//	reverse context (bridges' retCtx):          response header  "Bp-Ret-Ctx"
//
// The reverse context is only known once the service has returned, and a response header has to be
// set before the response starts.  The generated server writes its response after the service
// returned, so the server middleware computes the reverse context when the handler starts the
// response (the first WriteHeader or Write), or when it returns without writing anything.

// HTTP header names, in net/http's canonical form (so maps can be indexed directly).
var (
	traceCtxHeader = http.CanonicalHeaderKey(TraceCtxKey) // "Bp-Trace-Ctx"; TraceCtxKey: interceptors.go
	retCtxHTTPKey  = http.CanonicalHeaderKey(RetCtxKey)   // "Bp-Ret-Ctx"
)

// NewHTTPServerMiddleware returns the tracing server middleware for the given bridge kind.  See
// [NewServerInterceptor] for the arguments.
func NewHTTPServerMiddleware(ctx context.Context, tracer backend.Tracer, kind string) (httpmiddleware.ServerMiddleware, error) {
	tp, err := tracer.GetTracerProvider(ctx)
	if err != nil {
		return nil, err
	}
	s := &serverSide{
		tp:          tp,
		v:           variantFor(kind),
		attrBaggage: startAttributeBaggage(tracer),
		reverse:     backend.ReverseTrussEnabled(),
	}
	return func(service, method string, next http.Handler) http.Handler {
		return &httpServerHandler{s: s, m: newMethodInfo(tp, serverSpanNames, service, method), next: next}
	}, nil
}

// httpServerHandler traces the requests of one method.
type httpServerHandler struct {
	s    *serverSide
	m    *methodInfo // span name and tracer of this method
	next http.Handler
}

func (h *httpServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, state, sampled := h.s.prepare(r.Context(), headerValue(r.Header, traceCtxHeader)) // server.go: steps 1-3; below
	if !sampled {
		// Unsampled upstream: ctx only carries the remote IDs; no span, no tracing work.
		h.next.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	// 4. The server span.
	call := serverCall{state: state}
	ctx, call.span = h.m.tracer.Start(ctx, h.m.spanName, trace.WithSpanKind(trace.SpanKindServer))
	defer call.span.End()

	h.s.setup(&call, ctx) // server.go: steps 5-6

	// 7. The service, through the generated handler.  Steps 8-9 run when it starts the response.
	resp := &tracedResponse{ResponseWriter: w, s: h.s, call: &call}
	h.next.ServeHTTP(resp, r.WithContext(call.ctx))
	resp.finish(http.StatusOK) // the handler wrote nothing: net/http sends an empty 200
}

// tracedResponse is the ResponseWriter the generated handler writes to.  When the response starts it
// finishes the call (steps 8-9) and puts the reverse context in the response header.
type tracedResponse struct {
	http.ResponseWriter
	s        *serverSide
	call     *serverCall
	err      error // the error the service returned (RecordError)
	finished bool
}

// RecordError implements httpmiddleware.ErrorRecorder: the generated handler passes the service's
// error here before it writes the error response.
func (r *tracedResponse) RecordError(err error) { r.err = err }

func (r *tracedResponse) WriteHeader(status int) {
	r.finish(status)
	r.ResponseWriter.WriteHeader(status)
}

func (r *tracedResponse) Write(b []byte) (int, error) {
	r.finish(http.StatusOK) // a Write without WriteHeader sends 200
	return r.ResponseWriter.Write(b)
}

// Unwrap gives http.ResponseController the underlying writer (Flush, deadlines, ...).
func (r *tracedResponse) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// finish does steps 8-9 once, before the response with the given status starts.  The reverse context
// goes back only on success: a failed call (an error from the service, or an error status, which the
// generated handler also writes for arguments it cannot decode) sends none, as with wrappers, where
// the failed call's response is dropped and its retCtx with it.
func (r *tracedResponse) finish(status int) {
	if r.finished {
		return
	}
	r.finished = true
	retCtx := r.s.finishCall(r.call, r.err) // server.go
	if retCtx != "" && status < 300 {
		r.ResponseWriter.Header()[retCtxHTTPKey] = []string{retCtxHeader(retCtx)} // header.go
	}
}

// headerValue returns the first value of an HTTP header given in canonical form ("" if none),
// without Header.Get's canonicalisation on every call.
func headerValue(h http.Header, canonicalKey string) string {
	if v := h[canonicalKey]; len(v) > 0 {
		return v[0]
	}
	return ""
}
