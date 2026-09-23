package otelcol

// Tomislav-RetCtx: tests for the SDK one-shot retry policy (sdk_retry.go).

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryGateModesAndCodes(t *testing.T) {
	unavailable := status.Error(codes.Unavailable, "refusing")
	exhausted := status.Error(codes.ResourceExhausted, "shedding LP")
	invalid := status.Error(codes.InvalidArgument, "bad")
	if g := newRetryGate("", ""); g.mode != retryOff || g.delay != 300*time.Millisecond {
		t.Fatalf("default gate: mode %q delay %v", g.mode, g.delay)
	}
	if g := newRetryGate("bogus", "50"); g.mode != retryOff || g.delay != 50*time.Millisecond {
		t.Fatalf("unknown mode must be off: %q %v", g.mode, g.delay)
	}
	off, hp, all := newRetryGate(retryOff, "10"), newRetryGate(retryHP, "10"), newRetryGate(retryAll, "10")
	for _, tc := range []struct {
		g    *retryGate
		isHP bool
		err  error
		want bool
	}{
		{off, true, unavailable, false},
		{hp, true, unavailable, true},
		{hp, true, exhausted, true},
		{hp, false, unavailable, false}, // bridges never retry LP
		{hp, true, invalid, false},      // non-retryable codes are not retried
		{all, false, unavailable, true}, // vanilla retries everything once
		{all, true, errors.New("plain"), false},
	} {
		if got := tc.g.wants(tc.isHP, tc.err); got != tc.want {
			t.Errorf("mode %s hp=%v err=%v: wants %v, got %v", tc.g.mode, tc.isHP, tc.err, tc.want, got)
		}
	}
}

func TestRetryGateRetriesOnceAndTracksDepth(t *testing.T) {
	g := newRetryGate(retryHP, "20")
	batch := []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "a"}, {Name: "b"}}}}}}
	var sends, dones atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	g.schedule(batch, 2, func() error { sends.Add(1); return status.Error(codes.Unavailable, "still refusing") },
		func(err error) {
			defer wg.Done()
			dones.Add(1)
			if err == nil {
				t.Error("resend error must be passed to done")
			}
		})
	if !g.suppressLP() {
		t.Error("LP must be suppressed while an HP batch awaits its retry")
	}
	if g.pendingSpans.Load() != 2 || g.pendingBytes.Load() <= 0 || g.maxSpans.Load() != 2 {
		t.Errorf("pending %d spans / %d bytes, max %d", g.pendingSpans.Load(), g.pendingBytes.Load(), g.maxSpans.Load())
	}
	wg.Wait()
	if sends.Load() != 1 || dones.Load() != 1 {
		t.Fatalf("exactly one retry and one completion: sends %d dones %d", sends.Load(), dones.Load())
	}
	if g.suppressLP() || g.pendingSpans.Load() != 0 || g.pendingBytes.Load() != 0 {
		t.Errorf("gate must drain after the retry: pending %d/%d", g.pendingSpans.Load(), g.pendingBytes.Load())
	}
	if g.retriedSpans.Load() != 2 || g.retryFailSpans.Load() != 2 || g.retryOKSpans.Load() != 0 {
		t.Errorf("retried %d ok %d fail %d", g.retriedSpans.Load(), g.retryOKSpans.Load(), g.retryFailSpans.Load())
	}
	if g.maxSpans.Load() != 2 {
		t.Errorf("high-water must persist after draining: %d", g.maxSpans.Load())
	}
	f := g.fields()
	for i := 0; i+1 < len(f); i += 2 {
		if f[i] == "retry_window_max_spans" && f[i+1].(int64) != 2 {
			t.Errorf("window max %v", f[i+1])
		}
	}
	if g.winMaxSpans.Load() != 0 {
		t.Errorf("window restarts at the current depth after a metrics line: %d", g.winMaxSpans.Load())
	}
}

func TestRetryGateAllModeDoesNotSuppress(t *testing.T) {
	g := newRetryGate(retryAll, "20")
	var wg sync.WaitGroup
	wg.Add(1)
	g.schedule(nil, 1, func() error { return nil }, func(error) { wg.Done() })
	if g.suppressLP() {
		t.Error("vanilla's retry-all mode must never suppress")
	}
	wg.Wait()
	if g.retryOKSpans.Load() != 1 {
		t.Errorf("successful retry must count: %d", g.retryOKSpans.Load())
	}
}
