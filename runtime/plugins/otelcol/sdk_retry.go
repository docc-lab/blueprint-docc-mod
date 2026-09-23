package otelcol

// Tomislav-RetCtx: one-shot SDK-side retry of refused export batches.
//
// BRIDGES_RETRY selects the policy (default "off" = every campaign so far: a refused batch is
// dropped at once and recorded in the refused-trace census):
//
//	off  no retry.
//	hp   bridges: a refused HIGH-PRIORITY (checkpoint) batch is retried exactly ONCE after
//	     BRIDGES_RETRY_DELAY_MS (+ up to 50 % jitter). While any HP batch awaits its retry,
//	     LOW-PRIORITY batches are not sent at all: they are dropped locally and recorded as
//	     refused (the collector is refusing anyway; this spares it the receive/decode and the
//	     application the marshal/send).
//	all  vanilla's matching budget: every refused batch is retried exactly once; nothing is
//	     suppressed.
//
// Memory is bounded by construction: a batch is held only between its refusal and its single
// retry, so the pending set is at most (refusal rate x delay). The pending depth and its
// high-water marks (spans and protobuf bytes, since the last metrics line and since start) are
// logged on every *_processor_metrics line so the application-side cost is measured, not argued.

import (
	"log/slog"
	"math/rand"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	retryOff = "off"
	retryHP  = "hp"
	retryAll = "all"
)

type retryGate struct {
	mode  string
	delay time.Duration

	pendingBatches atomic.Int64
	pendingSpans   atomic.Int64
	pendingBytes   atomic.Int64

	maxSpans    atomic.Int64 // high-water since start
	maxBytes    atomic.Int64
	winMaxSpans atomic.Int64 // high-water since the last metrics line
	winMaxBytes atomic.Int64

	retriedSpans   atomic.Int64 // spans whose batch got its one retry
	retryOKSpans   atomic.Int64 // ... and was accepted on it
	retryFailSpans atomic.Int64 // ... and was refused again (then dropped + recorded)
	lpSuppressed   atomic.Int64 // LP spans dropped locally while HP awaited a retry
}

func newRetryGate(mode string, delayMS string) *retryGate {
	switch mode {
	case retryHP, retryAll:
	default:
		mode = retryOff
	}
	ms, err := strconv.Atoi(delayMS)
	if err != nil || ms <= 0 {
		ms = 300
	}
	return &retryGate{mode: mode, delay: time.Duration(ms) * time.Millisecond}
}

// errLPSuppressed marks an LP batch dropped locally while an HP batch awaited its retry.
var errLPSuppressed = status.Error(codes.ResourceExhausted, "sdk: LP batch suppressed while an HP batch awaits its retry")

// sdkRetry is per process: one bridge processor per service.
var sdkRetry = newRetryGate(os.Getenv("BRIDGES_RETRY"), os.Getenv("BRIDGES_RETRY_DELAY_MS"))

// retryable: the collector's refusals (memory_limiter / priority processor: Unavailable; LP
// shedding may use ResourceExhausted) and an export that timed out under load.
func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
		return true
	}
	return false
}

// wants reports whether a refused batch of this priority gets its one retry.
func (g *retryGate) wants(isHP bool, err error) bool {
	switch g.mode {
	case retryHP:
		return isHP && retryable(err)
	case retryAll:
		return retryable(err)
	}
	return false
}

// suppressLP reports whether an LP batch must be dropped locally instead of sent.
func (g *retryGate) suppressLP() bool {
	return g.mode == retryHP && g.pendingBatches.Load() > 0
}

func bumpMax(v *atomic.Int64, x int64) {
	for {
		cur := v.Load()
		if x <= cur || v.CompareAndSwap(cur, x) {
			return
		}
	}
}

// schedule holds one refused batch and retries it once after delay (+jitter). resend performs
// the send; done receives its result (nil = accepted). Exactly one call to done per schedule.
func (g *retryGate) schedule(events []*tracepb.ResourceSpans, n int64, resend func() error, done func(error)) {
	var size int64
	for _, rs := range events {
		size += int64(proto.Size(rs))
	}
	g.pendingBatches.Add(1)
	spans := g.pendingSpans.Add(n)
	bytes := g.pendingBytes.Add(size)
	bumpMax(&g.maxSpans, spans)
	bumpMax(&g.maxBytes, bytes)
	bumpMax(&g.winMaxSpans, spans)
	bumpMax(&g.winMaxBytes, bytes)
	g.retriedSpans.Add(n)
	wait := g.delay + time.Duration(rand.Int63n(int64(g.delay)/2+1))
	go func() {
		time.Sleep(wait)
		err := resend()
		g.pendingSpans.Add(-n)
		g.pendingBytes.Add(-size)
		g.pendingBatches.Add(-1)
		if err == nil {
			g.retryOKSpans.Add(n)
		} else {
			g.retryFailSpans.Add(n)
		}
		done(err)
	}()
}

// logMetrics emits the retry gauges as one BRIDGES_RETRY_metrics line (the runner's log parser
// keys on BRIDGES_RETRY); a no-op when the policy is off so existing logs are unchanged.
func (g *retryGate) logMetrics() {
	if g.mode == retryOff {
		return
	}
	slog.Info("BRIDGES_RETRY_metrics", g.fields()...)
}

// fields returns the retry gauges and restarts the high-water window.
func (g *retryGate) fields() []any {
	return []any{
		"retry_mode_" + g.mode, 1,
		"retry_delay_ms", g.delay.Milliseconds(),
		"retry_pending_spans", g.pendingSpans.Load(),
		"retry_pending_bytes", g.pendingBytes.Load(),
		"retry_window_max_spans", g.winMaxSpans.Swap(g.pendingSpans.Load()),
		"retry_window_max_bytes", g.winMaxBytes.Swap(g.pendingBytes.Load()),
		"retry_max_spans", g.maxSpans.Load(),
		"retry_max_bytes", g.maxBytes.Load(),
		"retried_spans", g.retriedSpans.Load(),
		"retry_ok_spans", g.retryOKSpans.Load(),
		"retry_fail_spans", g.retryFailSpans.Load(),
		"lp_suppressed_spans", g.lpSuppressed.Load(),
	}
}
