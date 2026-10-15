package interceptors

// Microbenchmarks of one traced gRPC call (client interceptor -> real gRPC over an in-memory
// connection -> server interceptor -> handler), to compare per-call cost of interceptor variants in
// seconds instead of running the whole app.  They use the real OTel SDK without span processors, so
// they measure the interceptors and gRPC, not the bridges.
//
// Like Blueprint's generated client, every call has its own 1 s deadline, so gRPC sends a
// grpc-timeout header whose value changes on every call (as in the real app).
//
// Run with:
//
//	cd runtime && go test -run '^$' -bench . -benchmem ./plugins/opentelemetry/interceptors/
//
// Variants:
//
//	NoTracing        plain gRPC, no interceptors: the floor
//	Vanilla          vanilla interceptors (no reverse context)
//	Bridge           path-bridge interceptors with REVERSE_TRUSS=on; the server returns a reverse
//	                 context, so it travels back in a trailer on every call
//	.../HeaderTable0 the same with the server option grpc.HeaderTableSize(0), which tells clients
//	                 not to index request headers in HTTP/2's header table
//	.../Workers      the same with the server option grpc.NumStreamWorkers(4) (what the generated
//	                 server sets from GRPC_STREAM_WORKERS): calls run on reused goroutines instead
//	                 of a new one each, so their stacks do not have to grow again on every call
//	.../GoPerCall    every call made on a new goroutine, as the SN workflow does (one goroutine per
//	                 downstream call): the client side's stack then grows on every call too
//	.../GoPerCall+Workers  both

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// benchClient starts a health server and returns a client for it.  kind "" means no interceptors.
func benchClient(b *testing.B, kind string, serverOpts []grpc.ServerOption, handle func(ctx context.Context) error) healthpb.HealthClient {
	b.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	tracer := staticTracer{tp}
	var dialOpts []grpc.DialOption
	if kind != "" {
		si, _ := NewServerInterceptor(context.Background(), tracer, kind)
		ci, _ := NewClientInterceptor(context.Background(), tracer, kind)
		serverOpts = append(serverOpts, grpc.ChainUnaryInterceptor(si))
		dialOpts = append(dialOpts, grpc.WithChainUnaryInterceptor(ci))
	}
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer(serverOpts...)
	healthpb.RegisterHealthServer(s, &healthServer{handle: handle})
	go s.Serve(lis)
	b.Cleanup(s.Stop)
	dialOpts = append(dialOpts,
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("passthrough:///bufnet", dialOpts...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	return healthpb.NewHealthClient(conn)
}

// benchCalls makes b.N calls from inside a traced "server" context (like a service calling another).
// goPerCall makes each call on a new goroutine (and waits for it).
func benchCalls(b *testing.B, client healthpb.HealthClient, kind string, goPerCall bool) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, root := tp.Tracer("bench").Start(context.Background(), "root")
	defer root.End()
	if kind != "" {
		ctx, _ = rootCtx(staticTracer{tp}, kind) // interceptors_test.go: per-request state like a traced server
	}
	req := &healthpb.HealthCheckRequest{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh return accumulator per call, as each request has its own: otherwise every
		// returned reverse context is merged into one ever-growing value.
		callCtx, cancel := context.WithTimeout(backend.WithRetMerge(ctx), time.Second) // timeout as Blueprint's generated client does
		var err error
		if goPerCall {
			done := make(chan struct{})
			go func() { _, err = client.Check(callCtx, req); close(done) }()
			<-done
		} else {
			_, err = client.Check(callCtx, req)
		}
		if err != nil {
			b.Fatal(err)
		}
		cancel()
	}
}

func BenchmarkCall(b *testing.B) {
	ret := backend.EncodeCheckpointRetCtx(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3, "pb", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	leaf := func(context.Context) error { return nil }
	leafWithRet := func(ctx context.Context) error { backend.AddToMerge(ctx, ret); return nil }

	for _, tc := range []struct {
		name   string
		kind   string
		env    string
		handle func(context.Context) error
	}{
		{"NoTracing", "", "off", leaf},
		{"Vanilla", "v", "off", leaf},
		{"Bridge", "pb", "on", leafWithRet},
	} {
		workers := grpc.NumStreamWorkers(4)
		for _, v := range []struct {
			suffix    string
			opts      []grpc.ServerOption
			goPerCall bool
		}{
			{"", nil, false},
			{"/HeaderTable0", []grpc.ServerOption{grpc.HeaderTableSize(0)}, false},
			{"/Workers", []grpc.ServerOption{workers}, false},
			{"/GoPerCall", nil, true},
			{"/GoPerCall+Workers", []grpc.ServerOption{workers}, true},
		} {
			b.Run(tc.name+v.suffix, func(b *testing.B) {
				b.Setenv("REVERSE_TRUSS", tc.env)
				client := benchClient(b, tc.kind, v.opts, tc.handle)
				benchCalls(b, client, tc.kind, v.goPerCall)
			})
		}
	}
}
