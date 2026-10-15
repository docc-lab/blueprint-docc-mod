package interceptors

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/httpmiddleware"
	"go.opentelemetry.io/otel/trace"
)

// Tomislav-RetCtx: the client side of the HTTP tracing middleware (see http_server.go).

// NewHTTPClientMiddleware returns the tracing client middleware for the given bridge kind.  See
// [NewServerInterceptor] for the arguments.
func NewHTTPClientMiddleware(ctx context.Context, tracer backend.Tracer, kind string) (httpmiddleware.ClientMiddleware, error) {
	tp, err := tracer.GetTracerProvider(ctx)
	if err != nil {
		return nil, err
	}
	c := &clientSide{
		tp:          tp,
		v:           variantFor(kind),
		attrBaggage: startAttributeBaggage(tracer),
		reverse:     backend.ReverseTrussEnabled(),
	}
	return func(service string, next http.RoundTripper) http.RoundTripper {
		return &tracedTransport{c: c, service: service, next: next}
	}, nil
}

// tracedTransport traces the calls of one HTTP client.  The generated client sends every call of
// method M to the path "/M".
type tracedTransport struct {
	c       *clientSide
	service string
	next    http.RoundTripper
	methods sync.Map // request path -> *methodInfo
}

func (t *tracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	// 1. Unsampled parent: forward only the compact carrier so downstream stays unsampled.
	if unsampledParent(ctx) { // client.go
		return t.next.RoundTrip(withHeader(req, traceCtxHeader, encodeUnsampledCarrier(trace.SpanContextFromContext(ctx)))) // carrier.go; below
	}

	call := &clientCall{}
	ctx, baggage := t.c.prepare(call, ctx) // client.go: steps 2-3

	// 4. The client span.  It ends when the caller closes the response body (spanBody below), i.e.
	//    once the generated client has read and decoded the response, as the wrapper's span did.
	m := t.method(req.URL.Path)
	ctx, call.span = m.tracer.Start(ctx, m.spanName, trace.WithSpanKind(trace.SpanKindClient))
	call.ctx = ctx
	carrier := carrierFor(call.span, baggage, t.c.attrBaggage) // client.go: step 5
	putBaggage(baggage)

	// 6. The call.
	resp, err := t.next.RoundTrip(withHeader(req, traceCtxHeader, carrier))
	if err != nil {
		t.c.finish(call, err) // client.go: step 8
		call.span.End()
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The generated client turns an error status into this error, which the wrapper recorded.
		t.c.finish(call, fmt.Errorf("StatusCode was %d", resp.StatusCode))
		call.span.End()
		return resp, nil
	}
	// 7. The reverse context, from the response header (bridges with REVERSE_TRUSS=on).
	if t.c.reverse {
		finishClientRetCtx(call.ctx, t.c.tp, call.span, retCtxFromHeader(headerValue(resp.Header, retCtxHTTPKey))) // client.go; header.go; http_server.go
	}
	resp.Body = &spanBody{ReadCloser: resp.Body, c: t.c, call: call}
	return resp, nil
}

// method returns the span name and tracer for a request path "/<Method>".
func (t *tracedTransport) method(path string) *methodInfo {
	if m, ok := t.methods.Load(path); ok {
		return m.(*methodInfo)
	}
	actual, _ := t.methods.LoadOrStore(path, newMethodInfo(t.c.tp, clientSpanNames, t.service, strings.TrimPrefix(path, "/"))) // interceptors.go
	return actual.(*methodInfo)
}

// spanBody ends the client span when the caller closes the response body.
type spanBody struct {
	io.ReadCloser
	c    *clientSide
	call *clientCall
	once sync.Once
}

func (b *spanBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		b.c.finish(b.call, nil) // client.go: step 8
		b.call.span.End()
	})
	return err
}

// withHeader returns a copy of req with one more header.  A RoundTripper must not change the request
// it was given, so the header map is copied too.
func withHeader(req *http.Request, canonicalKey, value string) *http.Request {
	r := *req
	r.Header = make(http.Header, len(req.Header)+1)
	for k, v := range req.Header {
		r.Header[k] = v
	}
	r.Header[canonicalKey] = []string{value}
	return &r
}
