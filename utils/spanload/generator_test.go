package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type captureServer struct {
	collector.UnimplementedTraceServiceServer
	mu       sync.Mutex
	requests []*collector.ExportTraceServiceRequest
	callback func(context.Context, *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error)
}

func (s *captureServer) Export(ctx context.Context, r *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, proto.Clone(r).(*collector.ExportTraceServiceRequest))
	s.mu.Unlock()
	if s.callback != nil {
		return s.callback(ctx, r)
	}
	return &collector.ExportTraceServiceResponse{}, nil
}
func grpcFixture(t *testing.T, s *captureServer) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer()
	collector.RegisterTraceServiceServer(g, s)
	go g.Serve(l)
	t.Cleanup(g.Stop)
	return l.Addr().String()
}
func clientsFor(t *testing.T, c config) []sender {
	t.Helper()
	list := []sender{}
	for _, ep := range c.Endpoints {
		client, err := newSender(c, ep)
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, client)
		t.Cleanup(func() { client.Close() })
	}
	return list
}

func TestAggregateRateAcrossEndpointsAndWorkers(t *testing.T) {
	a, b := &captureServer{}, &captureServer{}
	c := testConfig(t, "--endpoint", grpcFixture(t, a), "--endpoint", grpcFixture(t, b), "--insecure", "--workers", "4", "--rates", "240", "--duration", "250ms", "--batch-size", "7", "--queue", "32", "--bridge", "pb", "--bridge-bytes", "25", "--cpd", "6", "--report-interval", "0s")
	var seq atomic.Uint64
	r := runPhase(t.Context(), c, testProfile(t, c), clientsFor(t, c), [8]byte{1}, &seq, 1, "measure", 240, c.Duration, nil)
	if err := r.counts.validate(); err != nil {
		t.Fatal(err)
	}
	if r.Offered != 60 || r.Acknowledged != 60 || r.Checkpoints != 10 || r.counts.problems() {
		t.Fatalf("wrong aggregate rate/cadence: %+v", r)
	}
	if r.GenerationSeconds < 0.2 || r.ElapsedSeconds > 3 {
		t.Fatalf("bad pacing duration %.3f", r.ElapsedSeconds)
	}
	seen := map[string]bool{}
	for _, server := range []*captureServer{a, b} {
		server.mu.Lock()
		if len(server.requests) == 0 {
			t.Fatal("endpoint got no load")
		}
		for _, req := range server.requests {
			for _, s := range req.ResourceSpans[0].ScopeSpans[0].Spans {
				key := hex.EncodeToString(s.TraceId) + hex.EncodeToString(s.SpanId)
				if seen[key] {
					t.Fatal("duplicate span")
				}
				seen[key] = true
			}
		}
		server.mu.Unlock()
	}
	if len(seen) != 60 {
		t.Fatalf("server received %d spans", len(seen))
	}
}

func TestPartialSuccessAndRPCFailuresAreNotRetried(t *testing.T) {
	for _, mode := range []string{"partial", "failure", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			s := &captureServer{callback: func(context.Context, *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
				if mode == "failure" {
					return nil, status.Error(codes.ResourceExhausted, "full")
				}
				n := int64(2)
				if mode == "invalid" {
					n = 11
				}
				return &collector.ExportTraceServiceResponse{PartialSuccess: &collector.ExportTracePartialSuccess{RejectedSpans: n, ErrorMessage: "test"}}, nil
			}}
			c := testConfig(t, "--endpoint", grpcFixture(t, s), "--insecure", "--rates", "0", "--spans", "100", "--batch-size", "10", "--workers", "3", "--report-interval", "0s")
			var seq atomic.Uint64
			r := runPhase(t.Context(), c, testProfile(t, c), clientsFor(t, c), [8]byte{1}, &seq, 1, "measure", 0, time.Second, nil)
			if err := r.counts.validate(); err != nil {
				t.Fatal(err)
			}
			if r.Attempted != 100 || r.Requests != 10 || !r.counts.problems() {
				t.Fatalf("bad failure counts: %+v", r)
			}
			if mode == "partial" {
				if r.Rejected != 20 || r.Acknowledged != 80 {
					t.Fatalf("partial: %+v", r)
				}
			} else if r.Failed != 100 {
				t.Fatalf("failure: %+v", r)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.requests) != 10 {
				t.Fatal("RPC retried")
			}
		})
	}
}

func TestOverloadAndDrainDeadline(t *testing.T) {
	s := &captureServer{callback: func(ctx context.Context, _ *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	c := testConfig(t, "--endpoint", grpcFixture(t, s), "--insecure", "--rates", "10000", "--duration", "70ms", "--workers", "1", "--batch-size", "10", "--queue", "1", "--timeout", "10s", "--drain-timeout", "30ms", "--report-interval", "0s")
	var seq atomic.Uint64
	r := runPhase(t.Context(), c, testProfile(t, c), clientsFor(t, c), [8]byte{1}, &seq, 1, "measure", 10000, c.Duration, nil)
	if err := r.counts.validate(); err != nil {
		t.Fatal(err)
	}
	if r.QueueDropped == 0 || r.Failed == 0 || r.Unsent == 0 || r.ElapsedSeconds > 2 {
		t.Fatalf("drain/overload not bounded: %+v", r)
	}
}

func TestCancellationStopsUnpacedLoadAndDrains(t *testing.T) {
	s := &captureServer{callback: func(ctx context.Context, _ *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	c := testConfig(t, "--endpoint", grpcFixture(t, s), "--insecure", "--rates", "0", "--workers", "1", "--timeout", "10s", "--drain-timeout", "20ms", "--report-interval", "0s")
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(40*time.Millisecond, cancel)
	defer cancel()
	var seq atomic.Uint64
	r := runPhase(ctx, c, testProfile(t, c), clientsFor(t, c), [8]byte{1}, &seq, 1, "measure", 0, 10*time.Second, nil)
	if err := r.counts.validate(); err != nil {
		t.Fatal(err)
	}
	if !r.Interrupted || r.Failed == 0 || r.ElapsedSeconds > 1 {
		t.Fatalf("cancellation did not stop load: %+v", r)
	}
}

func TestHTTPAndCLIArtifacts(t *testing.T) {
	var received atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			io.WriteString(w, "otelcol_receiver_accepted_spans 42\n")
			return
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Error("wrong OTLP HTTP request")
		}
		data, _ := io.ReadAll(r.Body)
		req := &collector.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			t.Error(err)
		}
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				received.Add(uint64(len(ss.Spans)))
			}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		data, _ = proto.Marshal(&collector.ExportTraceServiceResponse{})
		w.Write(data)
	}))
	defer server.Close()
	out := filepath.Join(t.TempDir(), "run")
	args := []string{"--protocol", "http", "--endpoint", server.URL + "/v1/traces", "--metrics-url", server.URL + "/metrics", "--out", out, "--rates", "0,0", "--spans", "40", "--batch-size", "9", "--workers", "2", "--warmup", "1s", "--report-interval", "0s"}
	var stdout, stderr bytes.Buffer
	if code := execute(t.Context(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if received.Load() != 160 {
		t.Fatalf("warmup/ramp counts %d", received.Load())
	}
	data, err := os.ReadFile(filepath.Join(out, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	phases := 0
	scrapes := 0
	for {
		var row map[string]any
		err := decoder.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if row["type"] == "phase" {
			phases++
		}
		if row["type"] == "collector_metrics" {
			scrapes++
		}
	}
	if phases != 4 || scrapes != 8 {
		t.Fatalf("phase/scrape artifacts %d/%d", phases, scrapes)
	}
	if code := execute(t.Context(), args, io.Discard, io.Discard); code != 2 {
		t.Fatal("existing run directory was overwritten")
	}
}

func TestHTTPFailureExitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", http.StatusServiceUnavailable) }))
	defer server.Close()
	args := []string{"--protocol", "http", "--endpoint", server.URL, "--rates", "0", "--spans", "10", "--workers", "1", "--batch-size", "10", "--report-interval", "0s"}
	if code := execute(t.Context(), args, io.Discard, io.Discard); code != 1 {
		t.Fatalf("failed export exited %d", code)
	}
	if code := execute(t.Context(), append(args, "--allow-errors"), io.Discard, io.Discard); code != 0 {
		t.Fatalf("allow-errors exited %d", code)
	}
}
