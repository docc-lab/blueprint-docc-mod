package otelcol

// Tomislav-RetCtx: what does an OTLP export actually cost the application, and
// does a timed-out export cost less than one that completes?
//
// The no-work campaigns produced a paradox: the "passthrough" collectors (real
// store behind them) gave HIGHER application throughput than "sink" collectors
// (nothing downstream, every export acknowledged instantly), even though
// passthrough silently lost ~30 % of its spans to gRPC deadline expiry. The
// deployed measurement said composepost spends ~20 % more CPU per request when
// every export lands. This isolates that claim from the cluster.
//
// Three server behaviours, one real otlptracegrpc client, identical payloads:
//   ok      - respond immediately            (sink regime)
//   refuse  - respond Unavailable immediately (admission regime)
//   stall   - never respond; client hits its deadline (passthrough regime)
//
// The gRPC server runs in-process, so its CPU is included in the totals. That is
// deliberate and harmless: gRPC reads and unmarshals the request before invoking
// the handler in ALL three modes, so server cost is common and cancels in the
// differences, which is what is being compared.
//
// Run: OTLP_DEADLINE_MS=100 go test ./runtime/plugins/otelcol/ -run TestExportCost -v

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type exportMode int

const (
	modeOK exportMode = iota
	modeRefuse
	modeStall
)

type costServer struct {
	coltracepb.UnimplementedTraceServiceServer
	mode exportMode
}

func (s *costServer) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (
	*coltracepb.ExportTraceServiceResponse, error) {
	switch s.mode {
	case modeRefuse:
		return nil, status.Error(codes.Unavailable, "refused by priority processor")
	case modeStall:
		<-ctx.Done() // hold until the client's deadline fires, as a backed-up collector does
		return nil, status.Error(codes.DeadlineExceeded, "stalled")
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// costSpans builds a batch shaped like a real bridge span: six attributes, one
// of them the base64 `_br` breadcrumb, which dominates the payload.
func costSpans(n int) []*tracepb.ResourceSpans {
	attrs := func() []*commonpb.KeyValue {
		br := make([]byte, 96)
		for i := range br {
			br[i] = byte('A' + i%26)
		}
		kv := func(k, v string) *commonpb.KeyValue {
			return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
		}
		return []*commonpb.KeyValue{
			kv("_br", string(br)), kv("span.kind", "server"),
			kv("otel.scope.name", "bridges"), kv("_o", "17"),
			kv("_d", "4"),
		}
	}
	spans := make([]*tracepb.Span, n)
	for i := range spans {
		spans[i] = &tracepb.Span{
			TraceId:           []byte("0123456789abcdef"),
			SpanId:            []byte("01234567"),
			Name:              "ComposePost",
			Kind:              tracepb.Span_SPAN_KIND_SERVER,
			StartTimeUnixNano: uint64(time.Now().UnixNano()),
			EndTimeUnixNano:   uint64(time.Now().UnixNano()) + 1000,
			Attributes:        attrs(),
		}
	}
	return []*tracepb.ResourceSpans{{
		Resource:   &resourcepb.Resource{Attributes: attrs()[:2]},
		ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: "bridges"}, Spans: spans}},
	}}
}

// serveOK runs a normal gRPC OTLP server on an existing listener.
func serveOK(t *testing.T, lis net.Listener) (addr string, stop func()) {
	srv := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(srv, &costServer{mode: modeOK})
	go srv.Serve(lis)
	return lis.Addr().String(), srv.Stop
}

func cpuSeconds(t *testing.T) float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	sec := func(tv syscall.Timeval) float64 { return float64(tv.Sec) + float64(tv.Usec)/1e6 }
	return sec(ru.Utime) + sec(ru.Stime)
}

func runMode(t *testing.T, mode exportMode, batches, perBatch, inFlight int) (cpuPerSpan, wallPerBatch float64, allocMiB float64) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(srv, &costServer{mode: mode})
	go srv.Serve(lis)
	defer srv.Stop()

	ctx := context.Background()
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(lis.Addr().String()),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}),
	)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client start: %v", err)
	}
	defer client.Stop(ctx)

	payload := costSpans(perBatch)
	// warm the connection and the marshal paths
	for i := 0; i < 3; i++ {
		c, cancel := context.WithTimeout(ctx, grpcDeadline)
		_ = client.UploadTraces(c, payload)
		cancel()
	}
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)

	cpu0 := cpuSeconds(t)
	wall0 := time.Now()
	// The real processor spawns one goroutine per chunk from a 100 ms ticker, so
	// many exports are in flight at once. Sequential sending never stresses the
	// connection's stream concurrency, which is exactly what a stalled collector
	// exhausts. inFlight reproduces that.
	if inFlight <= 1 {
		for i := 0; i < batches; i++ {
			c, cancel := context.WithTimeout(ctx, grpcDeadline)
			_ = client.UploadTraces(c, payload)
			cancel()
		}
	} else {
		sem := make(chan struct{}, inFlight)
		var wg sync.WaitGroup
		for i := 0; i < batches; i++ {
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				c, cancel := context.WithTimeout(ctx, grpcDeadline)
				_ = client.UploadTraces(c, payload)
				cancel()
			}()
		}
		wg.Wait()
	}
	wall := time.Since(wall0)
	cpu := cpuSeconds(t) - cpu0
	runtime.ReadMemStats(&m1)

	spans := float64(batches * perBatch)
	return cpu / spans * 1e6, wall.Seconds() / float64(batches) * 1e3, float64(m1.TotalAlloc-m0.TotalAlloc) / 1024 / 1024
}

func TestExportCost(t *testing.T) {
	// Opt-in: this takes ~35 s and is a diagnostic, not a regression check.
	if os.Getenv("EXPORT_COST") == "" {
		t.Skip("set EXPORT_COST=1 (and OTLP_DEADLINE_MS=100) to run the export-cost diagnostic")
	}
	const batches, perBatch = 256, 512
	t.Logf("deadline=%v  batches=%d  spans/batch=%d", grpcDeadline, batches, perBatch)
	names := map[exportMode]string{modeOK: "ok (sink)", modeRefuse: "refuse (admission)", modeStall: "stall (passthrough)"}
	for _, inFlight := range []int{1, 32, 128, 256} {
		fmt.Printf("--- in-flight exports = %d ---\n", inFlight)
		for _, mode := range []exportMode{modeOK, modeRefuse, modeStall} {
			cpu, wall, alloc := runMode(t, mode, batches, perBatch, inFlight)
			fmt.Printf("%-22s cpu=%7.3f us/span   wall=%8.2f ms/batch   alloc=%7.1f MiB\n", names[mode], cpu, wall, alloc)
		}
	}
}
