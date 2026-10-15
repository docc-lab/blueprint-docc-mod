package interceptors

// Tomislav-RetCtx: what only the HTTP middleware has to get right (the shared behaviour is tested
// over both transports in interceptors_test.go).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// httpLeaf starts an HTTP server for /Check with the tracing server middleware in front of handler.
func httpLeaf(t *testing.T, tracer backend.Tracer, kind string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	sm, err := NewHTTPServerMiddleware(context.Background(), tracer, kind)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/Check", sm("Health", "Check", handler))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// tracedClient is an HTTP client with the tracing client middleware.
func tracedClient(t *testing.T, tracer backend.Tracer, kind string) *http.Client {
	t.Helper()
	cm, err := NewHTTPClientMiddleware(context.Background(), tracer, kind)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: cm("Health", http.DefaultTransport.(*http.Transport).Clone())}
}

// The reverse context must reach the caller when the handler returns without writing anything (it is
// set when the handler returns, net/http then sends an empty 200), and must not when the handler
// writes an error status without an error from the service (the generated handler does that for
// arguments it cannot decode): then the span records no exception either.
func TestHTTPRetCtxWithoutWrites(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	rec, tracer := newRecorder(sdktrace.AlwaysSample())
	ret := backend.EncodeCheckpointRetCtx(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 1, "pb", nil)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"no writes", func(w http.ResponseWriter, r *http.Request) { backend.AddToMerge(r.Context(), ret) }, ret},
		{"error status", func(w http.ResponseWriter, r *http.Request) {
			backend.AddToMerge(r.Context(), ret)
			http.Error(w, "bad argument", 500)
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httpLeaf(t, tracer, "v", tc.handler)
			resp, err := http.Get(srv.URL + "/Check")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			got := retCtxFromHeader(resp.Header.Get("Bp-Ret-Ctx"))
			if got != tc.want {
				t.Errorf("reverse context %q, want %q", got, tc.want)
			}
			for _, s := range rec.Ended() {
				if len(s.Events()) != 0 {
					t.Errorf("%s: no exception expected without an error from the service: %v", s.Name(), s.Events())
				}
			}
		})
	}
}

// The client span lasts until the caller closes the response body (the generated client reads and
// decodes the response before that, and the wrapper's span covered it), and the caller's request is
// not changed by the middleware.
func TestHTTPClientSpanEndsAtClose(t *testing.T) {
	rec, tracer := newRecorder(sdktrace.AlwaysSample())
	srv := httpLeaf(t, tracer, "v", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
	client := tracedClient(t, tracer, "v")

	ctx, root := tracer.tp.Tracer("test").Start(context.Background(), "root")
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/Check", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Header) != 0 {
		t.Errorf("the caller's request was changed: %v", req.Header)
	}
	ended := func(name string) bool {
		for _, s := range rec.Ended() {
			if s.Name() == name {
				return true
			}
		}
		return false
	}
	if !ended("HealthServer_Check") {
		t.Fatal("the server span should have ended before the response arrived")
	}
	if ended("HealthClient_Check") {
		t.Error("the client span ended before the body was read and closed")
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body.Close() // a second Close must not end the span twice
	root.End()
	n := 0
	for _, s := range rec.Ended() {
		if s.Name() == "HealthClient_Check" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("client span ended %d times, want once", n)
	}
}

// A request that fails in the transport (nothing listening) is recorded on the client span, which
// ends right away (there is no body to close).
func TestHTTPClientTransportError(t *testing.T) {
	rec, tracer := newRecorder(sdktrace.AlwaysSample())
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	client := tracedClient(t, tracer, "v")
	ctx, root := tracer.tp.Tracer("test").Start(context.Background(), "root")
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/Check", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected a transport error")
	}
	root.End()
	for _, s := range rec.Ended() {
		if s.Name() == "HealthClient_Check" {
			if len(s.Events()) == 0 || s.Events()[0].Name != "exception" {
				t.Errorf("expected an exception event, got %v", s.Events())
			}
			return
		}
	}
	t.Error("client span missing or not ended")
}
