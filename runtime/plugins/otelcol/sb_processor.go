// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MetadataPriorityKey is the gRPC metadata key the SB SDK stamps onto
// every UploadTraces call so the collector-side priority processor can
// admit/refuse each batch by priority class without inspecting spans.
// Must match priorityprocessor.MetadataPriorityKey exactly.
const MetadataPriorityKey = "bridges-priority"

// SB processor tunables. SBExportInterval is the flush cadence for BOTH
// the high-prio and low-prio buffers — they tick on the same schedule so
// drops we observe later cannot be attributed to a cadence skew between
// the two priority classes. SBBatchSize bounds the per-RPC payload so we
// stay under the default 4 MiB gRPC receive ceiling; each per-priority
// buffer is sliced into SBBatchSize-sized chunks at flush time and each
// chunk is its own goroutine + UploadTraces call. SBMetricsInterval
// bounds how often the processor logs its per-priority counter snapshot
// to slog.
const (
	SBExportInterval  = 100 * time.Millisecond
	SBBatchSize       = 512
	SBMetricsInterval = 1 * time.Second
)

// sbBufEntry carries one buffered span plus its InstrumentationScope and
// CP/LP classification. We no longer pre-wrap each span in its own
// ResourceSpans envelope (that pattern serialized the ~300 B Resource
// blob per-span and was responsible for ~5 GB of redundant bytes on the
// busiest collector pod under the 2k→4k ramp). Instead the buffer holds
// raw Spans + Scope; flushBuffer groups by Scope and builds ONE
// ResourceSpans per batch — the shared Resource is captured once via
// resourceOnce on first OnEnd. The isCheckpoint bit lets the flush path
// attribute send outcomes (cpDropped vs lpDropped) per priority.
type sbBufEntry struct {
	span         *tracepb.Span
	scope        instrumentation.Scope
	isCheckpoint bool
}

type StructuralBridgeProcessor struct {
	mu sync.RWMutex

	// Tomislav-RetCtx: the OTLP connection (otlp_wire.go); the export bytes are written here.
	conn *otlpConn

	// Configuration
	agentEndpoint string
	ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	// Tomislav-RetCtx: delayed orthogonal trusses (paper §3.4) produced at
	// server OnEnd, drained by the next OnStart in this process.
	delayedEndEventsChan chan structuralDelayed
	deeOnce              sync.Once

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Tracks in-flight per-batch export goroutines so Shutdown can wait
	// for them to drain before closing the gRPC client.
	exportWG sync.WaitGroup

	// sbBufEntry pairs a ResourceSpans with its CP/LP classification so
	// the flush path can attribute send-failure drops to the correct
	// priority bucket (cpDropped vs lpDropped). The priority bit is
	// already known at routeToPipeline time — we just have to carry it.

	// Two physical buffers, separated by CP/LP classification at OnEnd.
	// flushBuffer drains them sequentially with HP preference: it snaps
	// both under one lock, then dispatches all HP chunks as parallel
	// goroutines before dispatching LP chunks. Dispatch order favors HP
	// — the collector's priority processor sees HP RPCs land first on
	// average within each tick, which gives the priority-aware refusal
	// policy a tighter signal to operate on.
	//
	// Each buffer is the same `sbBufEntry` slice the single-buffer
	// version used; isCheckpoint is now an implicit per-buffer property
	// (true for hpBuf, false for lpBuf) but we keep the field for
	// exportBatch's accounting path so the goroutine doesn't need to
	// re-look-up which buffer the chunk came from.
	hpBuf      []wireSpan // Tomislav-RetCtx: encoded spans (otlp_wire.go)
	lpBuf      []wireSpan
	eventsLock sync.Mutex

	// Shared Resource proto for every export. A TracerProvider has exactly
	// one Resource per service instance, so we capture it lazily on the
	// first OnEnd and reuse the same pointer for every flush. This
	// eliminates the per-span Resource duplication that previously
	// inflated the wire payload by a factor of ~3 (matches the same fix
	// vanilla_processor.go already had).
	resource     []byte // the encoded ResourceSpans.resource field (otlp_wire.go, resourceField)
	resourceOnce sync.Once

	// Counters for drop monitoring. All atomic so the metrics goroutine
	// can read them concurrently with OnEnd / flushBuffer.
	// Invariants:
	//   spansReceived = OnEnd entries (input to processor)
	//   spansFlushed  = spans drained from the buffer in flushBuffer ticks
	//                 = spansSent + spansDropped
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

	// deeDropped counts DEE triples that we couldn't enqueue because the
	// delayedEndEventsChan was full. The send is non-blocking — losing
	// occasional bridge state is preferable to blocking the request
	// goroutine. Surfaced in the metrics line so a sustained climb is
	// visible.
	deeDropped int64

	// Per-priority counters. The SDK already classifies every span in
	// OnEnd before calling routeToPipeline; these track how that
	// classification breaks down at received / sent / dropped, so the
	// bridges thesis can quote exact CP-loss numbers at the SDK layer
	// instead of estimating from an assumed CP fraction.
	//   cpReceived + lpReceived = spansReceived
	//   cpSent     + lpSent     = spansSent
	//   cpDropped  + lpDropped  = spansDropped
	cpReceived int64
	lpReceived int64
	cpSent     int64
	lpSent     int64
	cpDropped  int64
	lpDropped  int64

	// AI_ADDED: Removed serverSideSpans map and ssLock - now using hasChildren attribute instead

	// Config discovery
	configDiscoveryPort int
	httpClient          *http.Client
	configMap           map[string]interface{}
	configLock          sync.RWMutex

	// Checkpoint distance (parsed from config, default: 1)
	checkpointDistance int64
	checkpointRange    checkpointRange
	reversePolicy      reversePolicy
}

// Darby: this gets run once per service (when initialized)
func NewStructuralBridgeProcessor(ctx context.Context, agentEndpoint string, configDiscoveryPort string) (*StructuralBridgeProcessor, error) {
	slog.Info("🔵 Creating new StructuralBridgeProcessor", "agentEndpoint", agentEndpoint)

	bloomFilter := bloom.New(BloomFilterM, BloomFilterK)

	// Extract host from agent endpoint
	var host string
	if strings.Contains(agentEndpoint, ":") {
		parts := strings.Split(agentEndpoint, ":")
		if len(parts) >= 2 {
			host = parts[0]
		}
	}
	if host == "" {
		host = "localhost" // Fallback to localhost
	}

	// Create endpoint
	endpoint := fmt.Sprintf("%s:4317", host)

	slog.Info("🔵 Using endpoint", "endpoint", endpoint)

	// Tomislav-RetCtx: a plain gRPC connection; the processor writes the OTLP bytes itself
	// (otlp_wire.go) instead of handing protobuf objects to the exporter client.  The exporter's
	// retry (OTLP_RETRY) has no counterpart on this path: every campaign runs with it off, and the
	// 1 s export deadline never left room for its 5 s first retry anyway.
	conn, err := dialOTLP(endpoint)
	if err != nil {
		slog.Error("❌ Failed to open the OTLP connection", "error", err)
		return nil, fmt.Errorf("failed to open the OTLP connection: %w", err)
	}
	if grpcRetryEnabled {
		slog.Warn("OTLP_RETRY is not off; the direct OTLP export path does not retry refused batches")
	}

	slog.Info("✅ StructuralBridgeProcessor created successfully")

	// Resolve ancestry mode from environment (default: bloom)
	// mode := AncestryMode(os.Getenv("ANCESTRY_MODE"))
	// if mode == "" {
	// 	// mode = AncestryModeBloom
	// 	mode = AncestryModeHash
	// }

	// Parse config discovery port
	configDiscoveryPortInt, err := strconv.Atoi(configDiscoveryPort)
	if err != nil {
		slog.Error("❌ Failed to convert configDiscoveryPort to int", "error", err)
		return nil, fmt.Errorf("failed to convert configDiscoveryPort to int: %w", err)
	}

	// Create HTTP client for config discovery
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	processor := &StructuralBridgeProcessor{
		conn:                 conn,
		agentEndpoint:        agentEndpoint,
		bloomFilter:          bloomFilter,
		stopChan:             make(chan struct{}),
		ancestryMode:         AncestryModePB,
		configDiscoveryPort:  configDiscoveryPortInt,
		httpClient:           httpClient,
		configMap:            make(map[string]interface{}),
		checkpointDistance:   1,                                   // Default: every span is a checkpoint
		delayedEndEventsChan: make(chan structuralDelayed, 10000), // Buffered channel to avoid blocking
	}

	slog.Info("🔵 Ancestry mode configured", "mode", AncestryModePB)

	// Fetch full config from config discovery endpoint
	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
		if errors.Is(err, errInvalidCheckpointRange) || errors.Is(err, errInvalidReversePolicy) {
			_ = conn.close()
			return nil, err
		}
		slog.Error("❌ Failed to fetch full config", "error", err)
		// Don't fail initialization if config fetch fails - continue with empty config
		slog.Warn("⚠️ Continuing with empty config map")
	} else {
		slog.Info("🟢 Successfully fetched full config", "config_keys", len(processor.configMap))
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

// logMetricsLoop emits one slog.Info line per SBMetricsInterval with the
// full per-priority counter snapshot, until stopChan is closed. Tail via
// `kubectl logs deploy/<svc> | grep sb_processor_metrics`.
func (p *StructuralBridgeProcessor) logMetricsLoop() {
	defer p.wg.Done()
	t := time.NewTicker(SBMetricsInterval)
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
// per-priority in-memory buffer depths. Counters are loaded atomically;
// buffer depths are read under a brief lock.
func (p *StructuralBridgeProcessor) logMetrics() {
	p.eventsLock.Lock()
	hpDepth := len(p.hpBuf)
	lpDepth := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Info("sb_processor_metrics",
		"spans_received", atomic.LoadInt64(&p.spansReceived),
		"spans_flushed", atomic.LoadInt64(&p.spansFlushed),
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"batches_sent", atomic.LoadInt64(&p.batchesSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped),
		"batches_dropped", atomic.LoadInt64(&p.batchesDropped),
		"cp_received", atomic.LoadInt64(&p.cpReceived),
		"lp_received", atomic.LoadInt64(&p.lpReceived),
		"cp_sent", atomic.LoadInt64(&p.cpSent),
		"lp_sent", atomic.LoadInt64(&p.lpSent),
		"cp_dropped", atomic.LoadInt64(&p.cpDropped),
		"lp_dropped", atomic.LoadInt64(&p.lpDropped),
		"send_deadline", atomic.LoadInt64(&p.sendDeadline),
		"send_unavailable", atomic.LoadInt64(&p.sendUnavailable),
		"send_exhausted", atomic.LoadInt64(&p.sendExhausted),
		"send_canceled", atomic.LoadInt64(&p.sendCanceled),
		"send_other", atomic.LoadInt64(&p.sendOther),
		"hp_buffer_depth", hpDepth,
		"lp_buffer_depth", lpDepth,
		"dee_dropped", atomic.LoadInt64(&p.deeDropped),
		"dee_chan_depth", len(p.delayedEndEventsChan),
	)
	// Tomislav-RetCtx: SDK retry gauges on their own line (sdk_retry.go; parsed as BRIDGES_RETRY)
	sdkRetry.logMetrics()
}

// categorizeSendError increments the appropriate per-code counter for a
// failed UploadTraces. Uses status.Code which unwraps the gRPC status
// out of fmt.Errorf %w wrappers.
func (p *StructuralBridgeProcessor) categorizeSendError(err error) {
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
// buffer once and spawns one goroutine per SBBatchSize-sized chunk;
// export I/O is therefore fully off the OnEnd hot path.
func (p *StructuralBridgeProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(SBExportInterval)
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

// flushBuffer atomically snaps both priority buffers, then dispatches
// HP chunks first, then LP chunks. Each chunk carries one priority class
// (never mixed) tagged with a "bridges-priority: hp|lp" gRPC metadata
// header. Chunks are dispatched as parallel goroutines (matching the
// vanilla processor pattern); the HP-first ordering of the dispatch
// loop means HP UploadTraces calls enter flight (and reach the
// collector) on average before LP calls within the same tick.
//
// Chunking bounds each OTLP RPC under the default 4 MiB gRPC receive
// ceiling regardless of arrival rate.
func (p *StructuralBridgeProcessor) flushBuffer() {
	p.eventsLock.Lock()
	hpSnap := p.hpBuf
	lpSnap := p.lpBuf
	p.hpBuf = make([]wireSpan, 0, cap(p.hpBuf))
	p.lpBuf = make([]wireSpan, 0, cap(p.lpBuf))
	p.eventsLock.Unlock()

	if len(hpSnap) == 0 && len(lpSnap) == 0 {
		return
	}

	atomic.AddInt64(&p.spansFlushed, int64(len(hpSnap)+len(lpSnap)))

	// Dispatch HP first, then LP. Within each priority class, group by
	// InstrumentationScope (almost always one scope per Blueprint
	// service) and chunk at SBBatchSize.
	p.dispatchPriority(hpSnap, true)
	p.dispatchPriority(lpSnap, false)
}

// dispatchPriority groups one priority class's snapshot by scope, chunks
// each scope group at SBBatchSize, and spawns one goroutine per chunk.  Each chunk is encoded as
// one ExportTraceServiceRequest (otlp_wire.go, buildBatch): one ResourceSpans (the shared
// Resource) wrapping one ScopeSpans wrapping up to SBBatchSize spans.
func (p *StructuralBridgeProcessor) dispatchPriority(snap []wireSpan, isHP bool) {
	if len(snap) == 0 {
		return
	}
	for _, g := range groupByScope(snap) {
		for start := 0; start < len(g.spans); start += SBBatchSize {
			end := start + SBBatchSize
			if end > len(g.spans) {
				end = len(g.spans)
			}
			batch := buildBatch(p.resource, g.scope, g.spans[start:end])
			p.exportWG.Add(1)
			go p.exportBatch(batch, isHP)
		}
	}
}

// exportBatch sends one priority-homogeneous batch and updates counters
// based on outcome. On failure the spans are permanently dropped (no retry).
func (p *StructuralBridgeProcessor) exportBatch(batch *wireBatch, isHP bool) {
	defer p.exportWG.Done()
	// Tomislav-RetCtx: SDK one-shot retry policy (sdk_retry.go; BRIDGES_RETRY, default off).
	if !isHP && sdkRetry.suppressLP() {
		sdkRetry.lpSuppressed.Add(batch.n)
		p.batchDropped(batch, isHP, errLPSuppressed)
		return
	}
	err := p.sendData(batch.req, isHP)
	if err != nil && sdkRetry.wants(isHP, err) {
		p.exportWG.Add(1)
		sdkRetry.scheduleBytes(int64(len(batch.req)), batch.n, func() error { return p.sendData(batch.req, isHP) }, func(err error) {
			defer p.exportWG.Done()
			if err != nil {
				p.batchDropped(batch, isHP, err)
				return
			}
			p.batchSent(batch.n, isHP)
		})
		return
	}
	if err != nil {
		p.batchDropped(batch, isHP, err)
		return
	}
	p.batchSent(batch.n, isHP)
}

// batchDropped accounts one batch that will not be delivered (census included).
func (p *StructuralBridgeProcessor) batchDropped(batch *wireBatch, isHP bool, err error) {
	atomic.AddInt64(&p.batchesDropped, 1)
	atomic.AddInt64(&p.spansDropped, batch.n)
	if isHP {
		atomic.AddInt64(&p.cpDropped, batch.n)
	} else {
		atomic.AddInt64(&p.lpDropped, batch.n)
	}
	p.categorizeSendError(err)
	recordRefusedIDs(batch.ids, isHP) // Tomislav-RetCtx: census of trace IDs in the dropped batch
	slog.Error("Failed to send batch", "error", err, "count", batch.n, "hp", isHP)
}

// batchSent accounts one delivered batch.
func (p *StructuralBridgeProcessor) batchSent(n int64, isHP bool) {
	atomic.AddInt64(&p.batchesSent, 1)
	atomic.AddInt64(&p.spansSent, n)
	if isHP {
		atomic.AddInt64(&p.cpSent, n)
	} else {
		atomic.AddInt64(&p.lpSent, n)
	}
	slog.Debug("Sent batch", "count", n, "hp", isHP)
}

// sendData sends one encoded, priority-homogeneous batch with the
// "bridges-priority: hp|lp" gRPC metadata header attached. The
// collector-side priority processor reads this header to admit or refuse
// the batch in one O(1) decision.
func (p *StructuralBridgeProcessor) sendData(req []byte, isHP bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), grpcDeadline)
	defer cancel()

	priorityVal := "lp"
	if isHP {
		priorityVal = "hp"
	}
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataPriorityKey, priorityVal)

	if err := p.conn.export(ctx, req); err != nil { // otlp_wire.go
		slog.Error("❌ Failed to send data", "error", err, "hp", isHP)
		return fmt.Errorf("failed to send data: %w", err)
	}
	return nil
}

// Tomislav-RetCtx: startAttributeBaggage tells NewOTCollectorTracer (trace.go) whether OnStart
// adds "__bag." attributes.  SB keeps its state beside the span and adds at most "padding".
func (p *StructuralBridgeProcessor) startAttributeBaggage() bool { return false }

// OnStart implements SpanProcessor.OnStart
//
// Tomislav-RetCtx: S-Bridge = CG-Bridge window core (anchor, absolute depth, window Bloom, fan-out
// witnesses) + vertical start ordinals + orthogonal end events (paper §3.4). Wire layout:
// structural_truss.go.
//
// Two steps.  structuralInputsFrom does every context lookup (they walk the context chain, which
// goes deep) in a small stack frame; startStructural then does the window work, which needs a large
// frame but calls nothing deep.  It used to be one function with a 1,472-byte frame sitting under
// every lookup, and the app's fresh goroutines had to grow their stacks right there (about 1.5 s of
// CPU per 30 s profile at 700 req/s, 95% of SB's baggage lookup; utils/grpc_interceptors/README.md).
func (p *StructuralBridgeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	in := structuralInputsFrom(parent)
	p.startStructural(s, &in)
	if spanPadding != "" {
		// Artificial per-span byte inflation (SPAN_PADDING_BYTES env).
		// Goes on the wire — convertAttributes does not strip it.
		s.SetAttributes(attribute.String("padding", spanPadding))
	}
}

// structuralInputs is what a span's S-Bridge window needs from its parent's context.
type structuralInputs struct {
	br       string       // the parent's outgoing `_br` (raw bytes, pack.go), "" if none
	seqNum   int          // this span's start ordinal among its siblings
	ends     []int        // the siblings that had ended before it started, in end order (a copy)
	parentID trace.SpanID // the parent span, for the fan-out witness of the second child
}

// structuralInputsFrom reads the window's inputs from the parent's context.
func structuralInputsFrom(parent context.Context) (in structuralInputs) {
	if bag := backend.GetBaggageFromContext(parent); bag != nil {
		in.br = bag[BaggageBRKey]
	}
	in.seqNum, _ = parent.Value("seqNum").(int)
	if in.seqNum == 2 {
		in.parentID = trace.SpanFromContext(parent).SpanContext().SpanID()
	}
	// Orthogonal truss: the parent's end events observed so far (sibling start
	// ordinals in end order) are recorded by this child at its start.
	if eePtr, ok := parent.Value("endEvents").(*[]int); ok && eePtr != nil {
		mu, _ := parent.Value("childrenMutex").(*sync.Mutex)
		if mu != nil {
			mu.Lock()
		}
		if len(*eePtr) > 0 {
			in.ends = append([]int(nil), (*eePtr)...)
		}
		if mu != nil {
			mu.Unlock()
		}
	}
	return in
}

// startStructural computes the span's window and stores its wire state (export payload and
// outgoing propagation) beside the span.
//
// The parent's tail is not decoded: the child's tail is the parent's bytes plus one entry
// (appendChildTail, structural_truss.go), built once and shared by both payloads, which differ
// only in their window core (the export payload has the Bloom before this span was added).
func (p *StructuralBridgeProcessor) startStructural(s sdktrace.ReadWriteSpan, in *structuralInputs) {
	var (
		hasParent   bool
		parentTTL   byte
		parentDepth int
		anchor      [8]byte
		distance    int
		inherited   []byte
		parentHA    []byte
		tail        rawStructuralTail
	)
	if raw := viewPropBR(in.br); raw != nil { // pack.go: read-only, nothing below writes to it
		parentTTL, raw = p.checkpointRange.unwrap(raw)
		var ok bool
		parentDepth, anchor, distance, inherited, parentHA, tail, ok = scanStructuralBR(raw)
		hasParent = ok && (!p.checkpointRange.enabled() || int(parentTTL) < distance)
	}
	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}
	depth := 0
	if hasParent {
		depth = parentDepth + 1
	}
	var isCheckpoint bool
	var outgoingTTL byte
	if p.checkpointRange.enabled() {
		isCheckpoint, outgoingTTL = p.checkpointRange.next(parentTTL, hasParent)
	} else {
		// Fixed distance: same window layout, deterministic checkpoint positions.
		isCheckpoint, distance = depth%cpd == 0, min(cpd, 256)
	}
	if !hasParent {
		anchor = [8]byte{}
		if p.checkpointRange.enabled() {
			distance = int(outgoingTTL) + 1
		}
		inherited, parentHA, tail = nil, nil, rawStructuralTail{}
	}
	geometry := checkpointBlooms[distance-1]
	filter := bloom.NewFromBytes(inherited, geometry.m, geometry.k) // copies inherited

	// CG core: fan-out witnesses follow the CGPB rule. The first-started child
	// inherits the parent's window HA, the second records one
	// (parent_span_id || depth) witness, later children carry none.
	var ha []byte
	if hasParent && in.seqNum == 1 {
		ha = append([]byte(nil), parentHA...)
	} else if hasParent && in.seqNum == 2 {
		ha = haAppendEntry(nil, in.parentID.String(), depth)
	}

	// Delayed orthogonal trusses left by earlier requests in this service
	// instance attach to this outgoing span and ride vertically until a
	// checkpoint captures them. The drain is bounded by the channel depth.
	var drained []structuralDelayed
	queue := p.delayedQueue()
	for draining := true; draining; {
		select {
		case entry := <-queue:
			drained = append(drained, entry)
		default:
			draining = false
		}
	}

	// Emitted payload (kept iff this span ends as a checkpoint or leaf): the
	// INHERITED pre-self window plus this span's own ordinal and end-event group.
	headerLen := varintLen(depth) + 9 + geometry.bytes + varintLen(len(ha)) + len(ha)
	tailCap := 24 + len(tail.entries) + len(tail.delayed) + (in.seqNum+7)/8 + 4*len(in.ends) + 48*len(drained)
	emit := appendStructuralHeader(make([]byte, 0, headerLen+tailCap), depth, anchor, distance, filter.Bytes(), ha)
	tailStart := len(emit)
	emit = appendChildTail(emit, tail, in.seqNum, in.ends, drained)
	childTail := emit[tailStart:]

	// Outgoing propagation, built with the range mode's TTL byte in front (checkpointRange.wrap
	// would copy it all into a new buffer).
	sid := s.SpanContext().SpanID()
	var prop []byte
	if p.checkpointRange.enabled() {
		prop = append(make([]byte, 0, 1+headerLen+len(childTail)), outgoingTTL)
	}
	if isCheckpoint {
		// The checkpoint persisted everything above: re-anchor on this span with an empty window.
		if p.checkpointRange.enabled() {
			distance = int(outgoingTTL) + 1
			geometry = checkpointBlooms[distance-1]
		}
		prop = binary.AppendUvarint(prop, uint64(maxInt(depth, 0)))
		prop = append(prop, sid[:]...)
		prop = append(prop, byte(distance-1))
		prop = append(prop, make([]byte, geometry.bytes)...) // a new Bloom is all zeros
		prop = append(prop, 0, 0, 0)                         // no HA, no entries, no delayed entries
	} else {
		filter.AddPrehashed(sid[:])
		prop = appendStructuralHeader(prop, depth, anchor, distance, filter.Bytes(), ha)
		prop = append(prop, childTail...)
	}

	// Tomislav-RetCtx: nothing goes on the OTel span. The outgoing propagation reaches the RPC
	// wrappers through backend.AppendSpanBaggage; the export payload, ordinal, depth and
	// scheduling decision stay beside the span (wire_state.go).
	bridgeWires.store(sid, &bridgeWire{emit: emit, depth: depth, priority: isCheckpoint, structural: true, ordinal: in.seqNum,
		prop: propBROwned(prop)})
}

// delayedQueue returns the process-wide delayed-truss queue, creating it for
// processors built without the constructor (tests).
func (p *StructuralBridgeProcessor) delayedQueue() chan structuralDelayed {
	p.deeOnce.Do(func() {
		if p.delayedEndEventsChan == nil {
			p.delayedEndEventsChan = make(chan structuralDelayed, 10000)
		}
	})
	return p.delayedEndEventsChan
}

// OnEnd implements SpanProcessor.OnEnd. A server span's wrapper summarizes the
// end events its children produced after the last child started
// (remEndEvents = varint(children) || varint(count) || ordinals, minus the
// implied last end). Those become a delayed orthogonal truss stamped with this
// span's trace and span ID, queued for the next outgoing span of this process.
// The checkpoint decision itself is the shared PB/CGPB rule (scheduled window
// checkpoint, or a childless server span, unless rejected by the reverse SDK).
func (p *StructuralBridgeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// Tomislav-RetCtx: one attribute read serves the DEE summary, classification and
	// conversion; the span's wire state is taken (and freed) here.
	attrs := s.Attributes()
	w := bridgeWires.take(s.SpanContext().SpanID())
	for _, attr := range attrs {
		if attr.Key != "remEndEvents" {
			continue
		}
		// Tomislav-RetCtx: raw bytes (the wrapper and the interceptor write them unencoded).
		if summary := []byte(attr.Value.AsString()); len(summary) > 0 {
			d, valid := structuralDelayedFromServer(s.SpanContext().TraceID(), s.SpanContext().SpanID(), summary)
			if !valid || len(d.ends) == 0 {
				continue
			}
			select {
			case p.delayedQueue() <- d:
			default:
				atomic.AddInt64(&p.deeDropped, 1)
			}
		}
	}
	// Tomislav-RetCtx: apply reverse decisions without changing the DEE queue.
	p.routeToPipeline(s, attrs, w, structuralCheckpoint(s.SpanKind(), attrs, w))
}

// routeToPipeline builds the ResourceSpans envelope and appends it to
// the single buffer. The `highPriority` arg flows down to
// convertAttributes, which keeps the `_br` breadcrumb attribute on the
// wire iff highPriority — that PRESENCE is the priority signal the
// collector-side priority processor reads. The SDK itself no longer
// treats priorities differently at the export layer.
func (p *StructuralBridgeProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, attrs []attribute.KeyValue, w *bridgeWire, highPriority bool) {
	atomic.AddInt64(&p.spansReceived, 1)
	if highPriority {
		atomic.AddInt64(&p.cpReceived, 1)
	} else {
		atomic.AddInt64(&p.lpReceived, 1)
	}
	// Capture the (single, immutable) Resource on the first OnEnd. A
	// TracerProvider has exactly one Resource for its lifetime, so this
	// runs at most once. flushBuffer reuses this pointer for every chunk
	// so the Resource is serialized once per batch, not once per span.
	p.resourceOnce.Do(func() {
		p.resource = resourceField(s.Resource()) // otlp_wire.go
	})
	// Tomislav-RetCtx: the span's OTLP bytes, written once here (otlp_wire.go); `_br` / `_o` / `_d`
	// come from the wire entry, the attributes through the policyStructural strip list.
	entry := encodeSpan(s, attrs, w, highPriority, policyStructural)
	p.eventsLock.Lock()
	if highPriority {
		p.hpBuf = append(p.hpBuf, entry)
	} else {
		p.lpBuf = append(p.lpBuf, entry)
	}
	hpSize := len(p.hpBuf)
	lpSize := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Debug("🔴 Routed to pipeline", "span_name", s.Name(), "hp_buffer_size", hpSize, "lp_buffer_size", lpSize, "high_prio", highPriority)
}

// getConfigDiscoveryEndpoint converts the agent endpoint to the config discovery endpoint
func (p *StructuralBridgeProcessor) getConfigDiscoveryEndpoint() string {
	// Extract host from agent endpoint
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			host := parts[0]
			// Use configurable port for config discovery (same host, different port)
			return fmt.Sprintf("%s:%d", host, p.configDiscoveryPort)
		}
	}
	// Fallback to localhost with configurable port
	return fmt.Sprintf("localhost:%d", p.configDiscoveryPort)
}

// parseCheckpointDistance extracts and parses the checkpoint distance (cpd) from the config map
func (p *StructuralBridgeProcessor) parseCheckpointDistance(config map[string]interface{}) int64 {
	const defaultCPD = int64(1) // Default: every span is a checkpoint

	if config == nil {
		return defaultCPD
	}

	if cpdVal, exists := config["cpd"]; exists {
		// Handle different possible types for cpd (int, int64, float64, string)
		switch v := cpdVal.(type) {
		case int64:
			if v > 0 {
				return v
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case int:
			if v > 0 {
				return int64(v)
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case float64:
			cpd := int64(v)
			if cpd > 0 && float64(cpd) == v {
				return cpd
			}
			slog.Warn("cpd must be a positive integer, using default", "cpd", v)
			return defaultCPD
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				if parsed > 0 {
					return parsed
				}
				slog.Warn("cpd must be positive, using default", "cpd", v)
				return defaultCPD
			}
			slog.Warn("Failed to parse cpd as int64, using default", "cpd", v, "error", err)
			return defaultCPD
		default:
			slog.Warn("cpd has unexpected type, using default", "cpd", v, "type", fmt.Sprintf("%T", v))
			return defaultCPD
		}
	}

	// cpd not found in config, use default
	return defaultCPD
}

// fetchFullConfig fetches the full config from the config discovery endpoint
func (p *StructuralBridgeProcessor) fetchFullConfig(ctx context.Context) error {
	// Try to fetch config from the discovery endpoint with retries
	config, err := p.fetchFullConfigFromEndpointWithRetries(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch full config: %w", err)
	}

	// Parse checkpoint distance from config
	cpd := p.parseCheckpointDistance(config)
	// Tomislav-RetCtx: validate reverse routing before publishing SDK config.
	policy, err := parseReversePolicy(config)
	if err != nil {
		return err
	}
	rangeConfig, err := parseCheckpointRange(config)
	if err != nil {
		return err
	}
	if rangeConfig.enabled() {
		cpd = int64(rangeConfig.max)
	}

	// Log the full config as JSON
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		slog.Warn("Failed to marshal config to JSON for logging", "error", err)
	} else {
		slog.Info("Fetched config JSON", "config", string(configJSON))
	}

	p.configLock.Lock()
	p.configMap = config
	p.checkpointDistance = cpd
	p.checkpointRange = rangeConfig
	p.reversePolicy = policy
	p.configLock.Unlock()

	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd,
		"cpd_min", rangeConfig.min, "cpd_max", rangeConfig.max,
		"reverse_policy", policy.mode, "reverse_probability", policy.probability,
		"reverse_passthrough", policy.passthrough)
	return nil
}

// fetchFullConfigFromEndpointWithRetries fetches config from the discovery endpoint with retries
func (p *StructuralBridgeProcessor) fetchFullConfigFromEndpointWithRetries(ctx context.Context) (map[string]interface{}, error) {
	configDiscoveryEndpoint := p.getConfigDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getFullConfig", configDiscoveryEndpoint)

	// Retry loop with 1-second intervals
	for attempt := 1; attempt <= 60; attempt++ { // Max 60 attempts (60 seconds)
		slog.Debug("Attempting config discovery", "attempt", attempt, "endpoint", url)

		// Create a new request for each attempt
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			slog.Debug("Config discovery attempt failed, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to make HTTP request after %d attempts: %w", attempt, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Debug("Config discovery endpoint returned non-OK status, will retry", "attempt", attempt, "status", resp.StatusCode)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("config discovery endpoint returned status %d after %d attempts", resp.StatusCode, attempt)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Debug("Failed to read response body, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to read response body after %d attempts: %w", attempt, err)
		}

		var configResp ConfigResponse
		if err := json.Unmarshal(body, &configResp); err != nil {
			slog.Debug("Failed to parse config response, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to parse config response after %d attempts: %w", attempt, err)
		}

		if configResp.Config == nil {
			slog.Debug("Empty config in response, will retry", "attempt", attempt)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("empty config in response after %d attempts", attempt)
		}

		// Success! Return the config
		slog.Info("Config discovery successful", "attempt", attempt, "config_keys", len(configResp.Config))
		return configResp.Config, nil
	}

	return nil, fmt.Errorf("config discovery failed after 60 attempts")
}

// getConfigMap returns the config map, with thread-safe access
func (p *StructuralBridgeProcessor) getConfigMap() map[string]interface{} {
	p.configLock.RLock()
	defer p.configLock.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]interface{})
	for k, v := range p.configMap {
		result[k] = v
	}
	return result
}

// Shutdown implements SpanProcessor.Shutdown
func (p *StructuralBridgeProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 StructuralBridgeProcessor shutting down")

	// Stop the ticker + metrics-logger goroutines (the ticker also runs
	// one final flushBuffer to drain both priority buffers into export
	// goroutines tracked by exportWG).
	close(p.stopChan)
	p.wg.Wait()

	// Wait for all in-flight per-batch export goroutines to finish
	// before closing the gRPC client — otherwise their UploadTraces
	// calls would race the client.Stop and lose the final tail of spans.
	p.exportWG.Wait()

	if err := p.conn.close(); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	// Emit one final per-priority counter snapshot so tail-end stats
	// land in the logs even if the shutdown is the last thing the
	// process does.
	slog.Info("🔴 StructuralBridgeProcessor shutdown complete — final metrics:")
	p.logMetrics()
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *StructuralBridgeProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
