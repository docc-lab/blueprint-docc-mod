// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// spanPadding holds a pre-allocated padding string sized by the
// SPAN_PADDING_BYTES env var. Empty when unset / zero. Used by both
// SB and vanilla processors' OnStart to artificially inflate per-span
// wire size for tracing-pressure experiments — adds a `padding` string
// attribute on every span. NOT prefixed with `__bag.` so it propagates
// to the wire (forcing otelcol to allocate the bytes during decode).
//
// Computed once at package init from the env to avoid repeated env
// reads and string allocations on the OnStart hot path.
var spanPadding = func() string {
	n, _ := strconv.Atoi(os.Getenv("SPAN_PADDING_BYTES"))
	if n <= 0 {
		return ""
	}
	return strings.Repeat("x", n)
}()

// grpcDeadline is the per-RPC context.WithTimeout applied around every
// UploadTraces call. Used by both vanilla and SB processors. Configurable
// via OTLP_DEADLINE_MS env var so deadline experiments don't require an
// image rebuild — defaults to 1s, matching the SDK's original setting.
var grpcDeadline = func() time.Duration {
	ms, _ := strconv.Atoi(os.Getenv("OTLP_DEADLINE_MS"))
	if ms <= 0 {
		return 1 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}()

// grpcRetryEnabled controls whether the underlying otlptracegrpc client
// will retry retriable errors (e.g. memlim's Unavailable refusal) with
// exponential backoff. Default true matches the SDK's original behavior.
// Set OTLP_RETRY=off to disable retry — useful for measurements where
// retry interaction with the deadline confounds drop classification.
var grpcRetryEnabled = os.Getenv("OTLP_RETRY") != "off"

// Tunables for the export path. ExportInterval bounds the latency between
// a span ending and its OTLP RPC; BatchSize bounds the per-RPC payload so
// we don't crash into the default 4 MiB gRPC message ceiling on the
// receiver side (one ResourceSpans-per-span at ~300–1000 B mean lets a
// 15K-span tick exceed 4 MiB easily).
const (
	ExportInterval = 100 * time.Millisecond
	BatchSize      = 512
	// MetricsInterval bounds how often the processor logs its counter
	// snapshot to slog. 1s matches the ramp's per-step granularity so
	// you can correlate counter deltas with rps steps in real time.
	MetricsInterval = 1 * time.Second
)

type VanillaProcessor struct {
	mu sync.RWMutex

	// Tomislav-RetCtx: the OTLP connection (otlp_wire.go); the export bytes are written here.
	conn *otlpConn

	// Configuration
	agentEndpoint string

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Tracks in-flight per-batch export goroutines so Shutdown can wait for
	// them to drain before closing the gRPC client.
	exportWG sync.WaitGroup

	// Counters for drop monitoring. All atomic so the metrics goroutine
	// can read them concurrently with OnEnd / flushBuffer.
	// Invariants:
	//   spansReceived = OnEnd entries (input to processor)
	//   spansFlushed  = spans drained from the buffer in flushBuffer ticks
	//                 = spansSent + spansDropped (every flushed span
	//                   either uploads successfully or is counted dropped)
	//   batchesSent + batchesDropped = batches dispatched
	// The send_* counters bucket dropped batches by gRPC status code so
	// you can tell whether drops are deadline / unavailable / queue
	// overflow / other.
	spansReceived   int64
	spansFlushed    int64
	spansSent       int64
	batchesSent     int64
	spansDropped    int64
	batchesDropped  int64
	sendDeadline    int64 // gRPC code = DeadlineExceeded
	sendUnavailable int64 // gRPC code = Unavailable
	sendExhausted   int64 // gRPC code = ResourceExhausted
	sendCanceled    int64 // gRPC code = Canceled
	sendOther       int64 // any other error class

	// The encoded Resource of every export (otlp_wire.go, resourceField). A TracerProvider has
	// exactly one Resource, so it is captured on the first OnEnd and every batch carries the same
	// bytes once, not once per span.
	resource     []byte
	resourceOnce sync.Once

	// Buffer of pending spans, each already encoded (otlp_wire.go, wireSpan) with its
	// InstrumentationScope. flushBuffer groups by scope at export time to produce one
	// ResourceSpans per chunk (shared Resource, one ScopeSpans per chunk).
	spanBuf  []wireSpan
	spanLock sync.Mutex
}

// Darby: this gets run once per service (when initialized)
func NewVanillaProcessor(ctx context.Context, agentEndpoint string, additionalPort string) (*VanillaProcessor, error) {
	slog.Info("🔵 Creating new VanillaProcessor", "agentEndpoint", agentEndpoint)

	// Tomislav-RetCtx: a plain gRPC connection; the processor writes the OTLP bytes itself
	// (otlp_wire.go) instead of handing protobuf objects to the exporter client.  The exporter's
	// retry (OTLP_RETRY) has no counterpart on this path: every campaign runs with it off, and the
	// 1 s export deadline never left room for its 5 s first retry anyway.
	conn, err := dialOTLP(agentEndpoint)
	if err != nil {
		slog.Error("❌ Failed to open the OTLP connection", "error", err)
		return nil, fmt.Errorf("failed to open the OTLP connection: %w", err)
	}
	if grpcRetryEnabled {
		slog.Warn("OTLP_RETRY is not off; the direct OTLP export path does not retry refused batches")
	}

	slog.Info("✅ VanillaProcessor created successfully")

	processor := &VanillaProcessor{
		conn:          conn,
		agentEndpoint: agentEndpoint,
		stopChan:      make(chan struct{}),
	}

	// Start background worker for batch export
	processor.wg.Add(1)
	go processor.processEvents()

	// Start the metrics logger goroutine. Shares stopChan with
	// processEvents — Shutdown closes once and both unwind.
	processor.wg.Add(1)
	go processor.logMetricsLoop()

	return processor, nil
}

// logMetricsLoop emits one slog.Info line per MetricsInterval with the
// full counter snapshot, until stopChan is closed. Picked up by anything
// tailing kubectl logs.
func (p *VanillaProcessor) logMetricsLoop() {
	defer p.wg.Done()
	t := time.NewTicker(MetricsInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stopChan:
			return
		case <-t.C:
			p.logMetrics()
		}
	}
}

// logMetrics emits one snapshot of every counter plus the current
// in-memory buffer depth. Counters are loaded atomically; buffer depth
// is a brief lock for an O(1) read.
func (p *VanillaProcessor) logMetrics() {
	p.spanLock.Lock()
	bufDepth := len(p.spanBuf)
	p.spanLock.Unlock()
	slog.Info("vanilla_processor_metrics",
		"spans_received", atomic.LoadInt64(&p.spansReceived),
		"spans_flushed", atomic.LoadInt64(&p.spansFlushed),
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"batches_sent", atomic.LoadInt64(&p.batchesSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped),
		"batches_dropped", atomic.LoadInt64(&p.batchesDropped),
		"send_deadline", atomic.LoadInt64(&p.sendDeadline),
		"send_unavailable", atomic.LoadInt64(&p.sendUnavailable),
		"send_exhausted", atomic.LoadInt64(&p.sendExhausted),
		"send_canceled", atomic.LoadInt64(&p.sendCanceled),
		"send_other", atomic.LoadInt64(&p.sendOther),
		"buffer_depth", bufDepth,
	)
	// Tomislav-RetCtx: SDK retry gauges on their own line (sdk_retry.go; parsed as BRIDGES_RETRY)
	sdkRetry.logMetrics()
}

// categorizeSendError increments the appropriate per-code counter for a
// failed UploadTraces. Uses status.Code which unwraps the gRPC status
// out of fmt.Errorf %w wrappers.
func (p *VanillaProcessor) categorizeSendError(err error) {
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		atomic.AddInt64(&p.sendDeadline, 1)
	case codes.Unavailable:
		atomic.AddInt64(&p.sendUnavailable, 1)
	case codes.ResourceExhausted:
		atomic.AddInt64(&p.sendExhausted, 1)
	case codes.Canceled:
		atomic.AddInt64(&p.sendCanceled, 1)
	default:
		atomic.AddInt64(&p.sendOther, 1)
	}
}

// processEvents runs in the background to periodically drain the event
// buffer and dispatch batches for export. Each ticker fire snapshots the
// buffer once and spawns one goroutine per BatchSize-sized chunk; export
// I/O is therefore fully off the OnEnd hot path and parallel across chunks.
func (p *VanillaProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(ExportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Final drain: spawns chunk goroutines and returns; Shutdown
			// waits on exportWG after wg.Wait().
			p.flushBuffer()
			return
		case <-ticker.C:
			go p.flushBuffer()
		}
	}
}

// flushBuffer atomically swaps out the current span buffer, groups spans by
// InstrumentationScope, chunks each scope group at BatchSize, and exports
// each chunk in its own goroutine. Each chunk becomes a single
// ResourceSpans wrapping a single ScopeSpans wrapping ≤BatchSize Spans —
// so the shared Resource envelope is serialized exactly once per chunk
// rather than once per span (~3× smaller wire payload than the
// per-span-ResourceSpans shape it replaced). Chunking keeps every OTLP
// RPC under the receiver-side 4 MiB gRPC limit regardless of arrival rate.
// Tomislav-RetCtx: the chunk's request bytes are built by concatenating the spans' encoded bytes
// (otlp_wire.go, buildBatch); nothing is marshalled at flush time.
func (p *VanillaProcessor) flushBuffer() {
	p.spanLock.Lock()
	snap := p.spanBuf
	p.spanBuf = make([]wireSpan, 0, cap(p.spanBuf))
	p.spanLock.Unlock()

	if len(snap) == 0 {
		return
	}

	for _, g := range groupByScope(snap) {
		for start := 0; start < len(g.spans); start += BatchSize {
			end := start + BatchSize
			if end > len(g.spans) {
				end = len(g.spans)
			}
			batch := buildBatch(p.resource, g.scope, g.spans[start:end])
			// Count the chunk as flushed BEFORE we dispatch — at this point
			// the spans have permanently left the buffer, so whatever
			// happens to the upload they'll be accounted for under one of
			// {sent, dropped}.
			atomic.AddInt64(&p.spansFlushed, batch.n)
			p.exportWG.Add(1)
			go func(batch *wireBatch) {
				defer p.exportWG.Done()
				dropped := func(err error) {
					slog.Error("Failed to send batch", "error", err, "count", batch.n)
					atomic.AddInt64(&p.batchesDropped, 1)
					atomic.AddInt64(&p.spansDropped, batch.n)
					p.categorizeSendError(err)
					recordRefusedIDs(batch.ids, true) // Tomislav-RetCtx: every vanilla span is trace-critical
				}
				sent := func() {
					slog.Debug("Sent batch", "count", batch.n)
					atomic.AddInt64(&p.batchesSent, 1)
					atomic.AddInt64(&p.spansSent, batch.n)
				}
				err := p.sendData(batch.req)
				// Tomislav-RetCtx: vanilla's side of the SDK one-shot retry policy (sdk_retry.go):
				// BRIDGES_RETRY=all retries every refused batch once; nothing is suppressed.
				if err != nil && sdkRetry.wants(true, err) {
					p.exportWG.Add(1)
					sdkRetry.scheduleBytes(int64(len(batch.req)), batch.n, func() error { return p.sendData(batch.req) }, func(err error) {
						defer p.exportWG.Done()
						if err != nil {
							dropped(err)
							return
						}
						sent()
					})
					return
				}
				if err != nil {
					dropped(err)
					return
				}
				sent()
			}(batch)
		}
	}
}

// sendData sends one encoded batch to the OTLP endpoint.
func (p *VanillaProcessor) sendData(req []byte) error {
	// Create a context with timeout (env-tunable, default 1s).
	ctx, cancel := context.WithTimeout(context.Background(), grpcDeadline)
	defer cancel()

	if err := p.conn.export(ctx, req); err != nil { // otlp_wire.go
		slog.Error("❌ Failed to send data", "error", err)
		return fmt.Errorf("failed to send data: %w", err)
	}
	return nil
}

// Tomislav-RetCtx: startAttributeBaggage tells NewOTCollectorTracer (trace.go) whether OnStart
// adds "__bag." attributes.  Vanilla adds at most "padding".
func (p *VanillaProcessor) startAttributeBaggage() bool { return false }

// OnStart implements SpanProcessor.OnStart
func (p *VanillaProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	if spanPadding != "" {
		// Artificial per-span byte inflation for tracing-pressure experiments.
		// Goes on the wire (no __bag. prefix), so otelcol's decode path
		// allocates the bytes. See spanPadding init.
		s.SetAttributes(attribute.String("padding", spanPadding))
	}
}

// OnEnd implements SpanProcessor.OnEnd
func (p *VanillaProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	atomic.AddInt64(&p.spansReceived, 1)

	// Capture the (single, immutable) Resource on the first OnEnd. A
	// TracerProvider has exactly one Resource for its lifetime, so this
	// runs at most once.
	p.resourceOnce.Do(func() {
		p.resource = resourceField(s.Resource()) // otlp_wire.go
	})

	p.routeToPipeline(s)
}

// routeToPipeline builds the mutable Span proto for s and appends it to
// the buffer alongside its InstrumentationScope. The proto's Attributes /
// Events slices stay mutable in the buffer so subsequent stages (e.g.
// bridge processors that need to append breadcrumb attributes between
// OnEnd and the next flush) can extend them in place.
func (p *VanillaProcessor) routeToPipeline(s sdktrace.ReadOnlySpan) {
	// Tomislav-RetCtx: the span's OTLP bytes, written once here (otlp_wire.go).
	entry := encodeSpan(s, s.Attributes(), nil, false, policyVanilla)
	p.spanLock.Lock()
	p.spanBuf = append(p.spanBuf, entry)
	p.spanLock.Unlock()
}

// Shutdown implements SpanProcessor.Shutdown
func (p *VanillaProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 VanillaProcessor shutting down")

	// Stop the ticker goroutine (which runs one final flushBuffer to drain
	// the in-memory buffer into chunk goroutines tracked by exportWG).
	close(p.stopChan)
	p.wg.Wait()

	// Wait for all in-flight per-batch export goroutines to finish before
	// closing the gRPC client — otherwise their UploadTraces calls would
	// race the client.Stop and lose the final tail of spans.
	p.exportWG.Wait()

	// Stop the client
	if err := p.conn.close(); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	// Emit one final counter snapshot before returning so tail-end stats
	// land in the logs even if the shutdown is the last thing the process
	// does.
	slog.Info("🔴 VanillaProcessor shutdown complete — final metrics:")
	p.logMetrics()
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *VanillaProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
