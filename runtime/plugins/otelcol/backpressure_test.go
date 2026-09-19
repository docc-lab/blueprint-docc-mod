package otelcol

// Tomislav-RetCtx: does an export that never gets written cost less CPU than one
// that completes?
//
// export_cost_test.go stalled by holding the handler after gRPC had already READ
// and unmarshalled the whole request, so the client had paid the full transport
// cost before failing. That is NOT what a saturated collector does. A saturated
// collector stops reading, its HTTP/2 flow-control window drains, and the client
// blocks partway through writing the request body.
//
// This server completes the HTTP/2 handshake, advertises a 1 KiB per-stream
// window, and then never reads another byte or issues a WINDOW_UPDATE. The
// client therefore marshals the payload, writes ~1 KiB, and blocks until its
// deadline. Comparing that against a server that acknowledges normally isolates
// the cost of actually pushing a batch through the transport.
//
// Run: EXPORT_COST=1 OTLP_DEADLINE_MS=100 go test ./runtime/plugins/otelcol/ \
//          -run TestBackpressureExportCost -v -count=1

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"golang.org/x/net/http2"
	"google.golang.org/grpc/status"
)

// starvedServer accepts gRPC connections, finishes the handshake, then stops
// reading. Flow control blocks the client mid-write, exactly as a collector
// that is too busy to drain its socket does.
func starvedServer(t *testing.T) (addr string, stop func()) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				pre := make([]byte, len(http2.ClientPreface))
				if _, err := io.ReadFull(c, pre); err != nil {
					return
				}
				fr := http2.NewFramer(c, c)
				// 1 KiB per-stream window: the client can push almost nothing.
				if err := fr.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 1024}); err != nil {
					return
				}
				for {
					f, err := fr.ReadFrame()
					if err != nil {
						return
					}
					if sf, ok := f.(*http2.SettingsFrame); ok && !sf.IsAck() {
						_ = fr.WriteSettingsAck()
						break
					}
				}
				<-done // stop reading; never send WINDOW_UPDATE
			}(c)
		}
	}()
	return lis.Addr().String(), func() { close(done); lis.Close() }
}

func measure(t *testing.T, addr string, batches, perBatch, inFlight int) (cpuPerSpan, wallPerBatch float64, failed int64, codes2 map[string]int) {
	ctx := context.Background()
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(addr),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}),
	)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client start: %v", err)
	}
	defer client.Stop(context.Background())
	payload := costSpans(perBatch)
	for i := 0; i < 3; i++ { // warm
		c, cancel := context.WithTimeout(ctx, grpcDeadline)
		_ = client.UploadTraces(c, payload)
		cancel()
	}
	runtime.GC()
	cpu0 := cpuSeconds(t)
	wall0 := time.Now()
	var mu sync.Mutex
	codes2 = map[string]int{}
	sem := make(chan struct{}, inFlight)
	var wg sync.WaitGroup
	for i := 0; i < batches; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			c, cancel := context.WithTimeout(ctx, grpcDeadline)
			err := client.UploadTraces(c, payload)
			cancel()
			mu.Lock()
			if err != nil {
				failed++
				codes2[status.Code(err).String()]++
			} else {
				codes2["OK"]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	wall := time.Since(wall0)
	cpu := cpuSeconds(t) - cpu0
	spans := float64(batches * perBatch)
	return cpu / spans * 1e6, wall.Seconds() / float64(batches) * 1e3, failed, codes2
}

func TestBackpressureExportCost(t *testing.T) {
	if os.Getenv("EXPORT_COST") == "" {
		t.Skip("set EXPORT_COST=1 (and OTLP_DEADLINE_MS=100) to run the export-cost diagnostic")
	}
	const batches, perBatch, inFlight = 256, 512, 64
	t.Logf("deadline=%v batches=%d spans/batch=%d in-flight=%d", grpcDeadline, batches, perBatch, inFlight)

	// completes normally
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	okAddr, stopOK := serveOK(t, lis)
	cpuOK, wallOK, failOK, codesOK := measure(t, okAddr, batches, perBatch, inFlight)
	stopOK()

	// blocked by flow control, never drains
	stallAddr, stopStall := starvedServer(t)
	cpuStall, wallStall, failStall, codesStall := measure(t, stallAddr, batches, perBatch, inFlight)
	stopStall()

	fmt.Printf("completes normally   cpu=%7.3f us/span  wall=%7.2f ms/batch  failed=%d/%d codes=%v\n", cpuOK, wallOK, failOK, batches, codesOK)
	fmt.Printf("blocked by backpressure cpu=%7.3f us/span  wall=%7.2f ms/batch  failed=%d/%d codes=%v\n", cpuStall, wallStall, failStall, batches, codesStall)
	fmt.Printf("DELTA (cost of completing an export) = %7.3f us/span\n", cpuOK-cpuStall)
}
