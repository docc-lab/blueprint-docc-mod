// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/json"
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
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
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

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string
	ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	delayedEndEventsChan chan string

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
	hpBuf      []sbBufEntry
	lpBuf      []sbBufEntry
	eventsLock sync.Mutex

	// Shared Resource proto for every export. A TracerProvider has exactly
	// one Resource per service instance, so we capture it lazily on the
	// first OnEnd and reuse the same pointer for every flush. This
	// eliminates the per-span Resource duplication that previously
	// inflated the wire payload by a factor of ~3 (matches the same fix
	// vanilla_processor.go already had).
	resource     *resourcepb.Resource
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

	// Create OTLP gRPC client
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: grpcRetryEnabled}),
	)

	slog.Info("🔵 OTLP client created, starting connection")

	// Start the client
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
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
		client:               client,
		agentEndpoint:        agentEndpoint,
		bloomFilter:          bloomFilter,
		stopChan:             make(chan struct{}),
		ancestryMode:         AncestryModePB,
		configDiscoveryPort:  configDiscoveryPortInt,
		httpClient:           httpClient,
		configMap:            make(map[string]interface{}),
		checkpointDistance:   1,                        // Default: every span is a checkpoint
		delayedEndEventsChan: make(chan string, 10000), // Buffered channel to avoid blocking
	}

	slog.Info("🔵 Ancestry mode configured", "mode", AncestryModePB)

	// Fetch full config from config discovery endpoint
	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
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
	)
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
	p.hpBuf = make([]sbBufEntry, 0, cap(p.hpBuf))
	p.lpBuf = make([]sbBufEntry, 0, cap(p.lpBuf))
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

// dispatchPriority groups one priority class's snapshot by scope,
// chunks each scope group at SBBatchSize, and spawns one goroutine per
// chunk. Each chunk becomes a single ResourceSpans (shared
// p.resource) wrapping a single ScopeSpans wrapping ≤SBBatchSize Spans.
func (p *StructuralBridgeProcessor) dispatchPriority(snap []sbBufEntry, isHP bool) {
	if len(snap) == 0 {
		return
	}
	type scopeKey struct{ name, version string }
	type scopeGroup struct {
		scope instrumentation.Scope
		spans []*tracepb.Span
	}
	groups := make(map[scopeKey]*scopeGroup, 1)
	for _, e := range snap {
		k := scopeKey{e.scope.Name, e.scope.Version}
		g, ok := groups[k]
		if !ok {
			g = &scopeGroup{scope: e.scope}
			groups[k] = g
		}
		g.spans = append(g.spans, e.span)
	}
	for _, g := range groups {
		scopeProto := &commonpb.InstrumentationScope{
			Name:    g.scope.Name,
			Version: g.scope.Version,
		}
		for start := 0; start < len(g.spans); start += SBBatchSize {
			end := start + SBBatchSize
			if end > len(g.spans) {
				end = len(g.spans)
			}
			chunk := g.spans[start:end]
			rs := &tracepb.ResourceSpans{
				Resource: p.resource,
				ScopeSpans: []*tracepb.ScopeSpans{{
					Scope: scopeProto,
					Spans: chunk,
				}},
			}
			p.exportWG.Add(1)
			go p.exportBatch([]*tracepb.ResourceSpans{rs}, int64(len(chunk)), isHP)
		}
	}
}

// exportBatch sends one priority-homogeneous batch and updates
// counters based on outcome. On failure the spans in the batch are
// permanently dropped (no retry — the bridges design intends the
// priority processor + structural breadcrumb mechanism to absorb
// pressure, not SDK-side retry queues). isHP determines both the
// gRPC metadata header attached to the call AND which counter
// (cpDropped/lpDropped, cpSent/lpSent) accounts for the outcome.
func (p *StructuralBridgeProcessor) exportBatch(events []*tracepb.ResourceSpans, n int64, isHP bool) {
	defer p.exportWG.Done()
	err := p.sendData(events, isHP)
	if err != nil {
		atomic.AddInt64(&p.batchesDropped, 1)
		atomic.AddInt64(&p.spansDropped, n)
		if isHP {
			atomic.AddInt64(&p.cpDropped, n)
		} else {
			atomic.AddInt64(&p.lpDropped, n)
		}
		p.categorizeSendError(err)
		slog.Error("Failed to send batch", "error", err, "count", n, "hp", isHP)
		return
	}
	atomic.AddInt64(&p.batchesSent, 1)
	atomic.AddInt64(&p.spansSent, n)
	if isHP {
		atomic.AddInt64(&p.cpSent, n)
	} else {
		atomic.AddInt64(&p.lpSent, n)
	}
	slog.Debug("Sent batch", "count", n, "hp", isHP)
}

// sendData sends one priority-homogeneous batch with the
// "bridges-priority: hp|lp" gRPC metadata header attached. The
// collector-side priority processor reads this header (via
// client.FromContext on the receiver-populated ctx) to admit or
// refuse the batch in one O(1) decision.
func (p *StructuralBridgeProcessor) sendData(events []*tracepb.ResourceSpans, isHP bool) error {
	if len(events) == 0 {
		return nil
	}

	slog.Debug("🔵 Sending data", "count", len(events), "hp", isHP)

	ctx, cancel := context.WithTimeout(context.Background(), grpcDeadline)
	defer cancel()

	priorityVal := "lp"
	if isHP {
		priorityVal = "hp"
	}
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataPriorityKey, priorityVal)

	err := p.client.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ Failed to send data", "error", err, "count", len(events), "hp", isHP)
		return fmt.Errorf("failed to send data: %w", err)
	}

	slog.Debug("✅ Data sent successfully", "count", len(events), "hp", isHP)
	return nil
}

// OnStart implements SpanProcessor.OnStart
func (p *StructuralBridgeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// No mutex needed - checkpointDistance and ancestryMode are read-only after initialization
	// slog.Debug("🔵 StructuralBridgeProcessor OnStart called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// parentSpan := trace.SpanFromContext(parent)

	// if s.SpanKind() == trace.SpanKindServer {
	// 	totalSpanID := s.SpanContext().TraceID().String() + ":" + s.SpanContext().SpanID().String()
	// 	// AI_ADDED: No longer need to initialize map entry - using hasChildren attribute instead
	// 	s.SetAttributes(attribute.String("selfTotalID", totalSpanID))
	// } else {
	// 	slog.Info("🔵 Client-side span", "span_name", s.Name())
	// 	parentTotalSpanID := parentSpan.SpanContext().TraceID().String() + ":" + parentSpan.SpanContext().SpanID().String()
	// 	// AI_ADDED: No longer need map-based counting - server template sets hasChildren attribute via context
	// 	s.SetAttributes(attribute.String("parentTotalID", parentTotalSpanID))
	// }

	// Decode incoming _br baggage. Bit-packed layout (mirrors
	// bridges/bridge/pack.go PackSBridgeBR):
	//   varint(depth) || ckpt8(8) || varint(numGroups) ||
	//     per group: varint(d) || varint(numSeqs) || numSeqs * varint(seq)
	//   varint(numEnd) || numEnd * varint(seq) || deeBytes (tail)
	// Replaces legacy split baggage["depth"]+baggage["oa"]+baggage["ee"]+baggage["dee"].
	var (
		hasParent     bool
		parentDepth   int
		ckpt8         [8]byte
		ordinalGroups map[int][]int
		endEvents     []int
		deeBytes      []byte
	)
	if baggage := backend.GetBaggageFromContext(parent); baggage != nil {
		if br, ok := baggage[BaggageBRKey]; ok && br != "" {
			if raw, okB := decodeBR(br); okB {
				if d, c, og, ee, dee, okU := unpackSBridgeBR(raw); okU {
					hasParent = true
					parentDepth = d
					ckpt8 = c
					ordinalGroups = og
					endEvents = ee
					// Detach from baggage-backed buffer; we mutate below.
					deeBytes = append([]byte(nil), dee...)
				}
			}
		}
	}

	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}

	var depthMod int
	if hasParent {
		depthMod = (parentDepth + 1) % cpd
	} else {
		depthMod = 0
	}

	// Append this span's ordinal to ordinalGroups[depthMod].
	seqNum, _ := parent.Value("seqNum").(int)
	if ordinalGroups == nil {
		ordinalGroups = make(map[int][]int)
	}
	ordinalGroups[depthMod] = append(ordinalGroups[depthMod], seqNum)

	// Inherit per-call end-events from parent context (comma-delim string of
	// integer seqs; best-effort Atoi, skip malformed entries).
	if eeStr, ok := parent.Value("endEvents").(string); ok && eeStr != "" {
		for _, tok := range strings.Split(eeStr, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if v, err := strconv.Atoi(tok); err == nil {
				endEvents = append(endEvents, v)
			}
		}
	}

	// Drain delayed end events from process-wide channel and encode each as a
	// DEE triple: 16-byte traceID || varint(depth) || varint(n) || n*varint(seq).
	// Channel format is "<traceIDHex>::seq1,seq2,...".
	draining := true
	for draining {
		select {
		case event := <-p.delayedEndEventsChan:
			idx := strings.Index(event, "::")
			if idx <= 0 {
				continue
			}
			tid16 := traceIDHexTo16Bytes(event[:idx])
			var seqs []int
			for _, tok := range strings.Split(event[idx+2:], ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if v, err := strconv.Atoi(tok); err == nil {
					seqs = append(seqs, v)
				}
			}
			if len(seqs) > 0 {
				deeBytes = append(deeBytes, encodeDEETriple(tid16, depthMod, seqs)...)
			}
		default:
			draining = false
		}
	}

	// Pre-reset packed form is the breadcrumb payload that will be
	// wire-emitted iff this span ends up high-priority. Computed BEFORE
	// the checkpoint reset so it carries the full inherited chain.
	preResetPacked := packSBridgeBR(depthMod, ckpt8, ordinalGroups, endEvents, deeBytes)

	// On a checkpoint (depthMod == 0): reset ckpt8 to this span's ID,
	// clear groups/events/dee. The post-reset state is what we
	// propagate to children via baggage.
	if depthMod == 0 {
		sid := s.SpanContext().SpanID()
		copy(ckpt8[:], sid[:])
		ordinalGroups = nil
		endEvents = nil
		deeBytes = nil
	}

	propagationPacked := packSBridgeBR(depthMod, ckpt8, ordinalGroups, endEvents, deeBytes)

	// Two attributes are written here — that's the entire SB-processor
	// wire+intra-process surface. The first becomes outgoing baggage
	// (stripped from the exported span); the second is the wire-emit
	// candidate, kept iff the span ends up high-priority. There is NO
	// separate priority bit: OnEnd recovers the depth from the second
	// attribute's leading varint (see decodeBRDepth) and re-derives
	// `depth % cpd == 0` from there, matching the simulator's
	// compute-on-the-fly model.
	s.SetAttributes(
		// Becomes outgoing `_br` baggage via Blueprint's __bag.* →
		// baggage translation. Carries the POST-RESET propagation
		// snapshot.
		attribute.String(AttrBR, encodeBR(propagationPacked)),
		// Wire-export candidate: the PRE-RESET packed payload. Set on
		// every span; convertAttributes keeps it only for high-priority
		// spans. Presence on the wire IS the checkpoint signal —
		// matches trace_simulator.py's emit-iff-checkpoint model.
		attribute.String(AttrBREmit, encodeBR(preResetPacked)),
	)
	if spanPadding != "" {
		// Artificial per-span byte inflation (SPAN_PADDING_BYTES env).
		// Goes on the wire — convertAttributes does not strip it.
		s.SetAttributes(attribute.String("padding", spanPadding))
	}
}

// OnEnd implements SpanProcessor.OnEnd. The priority decision lives
// entirely here — there is no OnStart-time priority attribute to carry
// across. We:
//
//  1. Recover OnStart-time depth by peeking at the leading varint of
//     AttrBREmit (set unconditionally in OnStart). depth % cpd == 0
//     re-derives "was-CP-at-OnStart".
//  2. Apply the leaf-server override: a Server-kind span with no
//     children gets forced to CP regardless of depth. We use the
//     childCount / eventCount attributes set by Blueprint's gRPC/HTTP
//     wrappers as the leaf signal.
//  3. Flush any pending end-event marker into the cross-trace DEE
//     channel so the next OnStart in the same service can piggyback
//     it.
//
// The final isCheckpoint bool is passed to routeToPipeline, which
// flows through buildSpanProto → convertAttributes. convertAttributes
// then keeps or strips AttrBREmit based on that bool — that's how the
// breadcrumb's wire-presence ends up matching the priority decision.
func (p *StructuralBridgeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	var preResetEncoded, remEndEvents string
	var hasChildren, forceLP bool

	for _, attr := range s.Attributes() {
		switch attr.Key {
		case AttrBREmit:
			preResetEncoded = attr.Value.AsString()
		case "childCount", "eventCount":
			// Int variants — set by older Blueprint server templates
			// (serverTemplate, serverTemplateCGPB) which record the
			// raw child/event count as int.
			if attr.Value.AsInt64() > 0 {
				hasChildren = true
			}
		case "hasChildren":
			// Bool variant — set by the CURRENT active server template
			// (serverTemplatePath in plugins/opentelemetry/ir_ot_server.go).
			// Reads as `attribute.Bool("hasChildren", childCount > 0)`.
			// Without this case, the leaf-server override fires for
			// every server span and the classification collapses to
			// 100% CP. (Found 2026-06-05 after composepost reported
			// 100% CP / 33% drop, which is structurally impossible at
			// cpd=3 for a depth-2 server with depth-3 client children.)
			if attr.Value.AsBool() {
				hasChildren = true
			}
		case "remEndEvents":
			remEndEvents = attr.Value.AsString()
			if remEndEvents != "" {
				// Send to the cross-trace DEE channel so a later
				// OnStart in this service can pick it up. Blocking
				// send — the channel is buffered (10000) and the
				// next OnStart drains opportunistically.
				event := s.SpanContext().TraceID().String() + "::" + remEndEvents
				p.delayedEndEventsChan <- event
			}
		case AttrForceLP:
			// Synthetic pressure spans (e.g. TracePressureService)
			// set this to force LP classification regardless of depth.
			// Without this escape hatch, manually-created spans inside
			// a request handler all classify identically to the root
			// (no inter-process baggage hop → same depthMod), which
			// makes it impossible to generate pure-LP volume for
			// stress-testing the collector's priority-aware shedding.
			if attr.Value.AsBool() {
				forceLP = true
			}
		}
	}

	// Step 1: recover OnStart's depth decision from the breadcrumb's
	// leading varint.
	isCheckpoint := false
	if depth, ok := decodeBRDepth(preResetEncoded); ok {
		cpd := int(p.checkpointDistance)
		if cpd < 1 {
			cpd = 1
		}
		isCheckpoint = depth%cpd == 0
	}

	// Step 2: leaf-server override. A server span with no children is
	// always a checkpoint, regardless of where it lands modulo cpd.
	if s.SpanKind() == trace.SpanKindServer && !hasChildren {
		isCheckpoint = true
	}

	// Step 3: force-LP escape hatch for synthetic pressure spans.
	// Applied LAST so it overrides both the depth-based decision and
	// the leaf-server CP override.
	if forceLP {
		isCheckpoint = false
	}

	p.routeToPipeline(s, isCheckpoint)
}

// routeToPipeline builds the ResourceSpans envelope and appends it to
// the single buffer. The `highPriority` arg flows down to
// convertAttributes, which keeps the `_br` breadcrumb attribute on the
// wire iff highPriority — that PRESENCE is the priority signal the
// collector-side priority processor reads. The SDK itself no longer
// treats priorities differently at the export layer.
func (p *StructuralBridgeProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, highPriority bool) {
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
		p.resource = p.convertResourceToProto(s.Resource())
	})
	spanProto := p.buildSpanProto(s, highPriority)
	entry := sbBufEntry{
		span:         spanProto,
		scope:        s.InstrumentationScope(),
		isCheckpoint: highPriority,
	}
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

// buildSpanProto materializes a single *tracepb.Span from a ReadOnlySpan.
// The Resource and ScopeSpans envelopes are NOT built here — they're
// added at flush time so that a single ResourceSpans wraps many spans
// (sharing the Resource bytes) rather than one ResourceSpans per span.
// highPriority is threaded through to convertAttributes which keeps the
// `_br` breadcrumb attribute iff highPriority (that presence is the
// collector's priority signal).
func (p *StructuralBridgeProcessor) buildSpanProto(s sdktrace.ReadOnlySpan, highPriority bool) *tracepb.Span {
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()
	spanProto := &tracepb.Span{
		TraceId:           traceID[:],
		SpanId:            spanID[:],
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(s.EndTime().UnixNano()),
		Name:              s.Name(),
		Kind:              p.convertSpanKind(s.SpanKind()),
		Status:            p.convertStatus(s.Status()),
		Attributes:        p.convertAttributes(s.Attributes(), highPriority),
		Events:            p.convertEvents(s.Events()),
		Links:             p.convertLinks(s.Links()),
	}
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:]
	}
	return spanProto
}

// convertResourceToProto converts an OpenTelemetry resource to protobuf format
func (p *StructuralBridgeProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
	if resource == nil {
		return &resourcepb.Resource{}
	}

	// Try to get the resource's iterator
	var iter attribute.Iterator
	if r, ok := resource.(interface{ Iter() attribute.Iterator }); ok {
		iter = r.Iter()
	} else {
		// Fallback to empty resource
		return &resourcepb.Resource{}
	}

	// Convert attributes using the iterator
	attrs := p.convertAttributeIterator(iter)

	return &resourcepb.Resource{
		Attributes: attrs,
	}
}

// convertAttributeIterator converts an attribute iterator to protobuf format
func (p *StructuralBridgeProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
	if iter.Len() == 0 {
		return nil
	}

	attrs := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attr := iter.Attribute()
		attrs = append(attrs, p.convertAttribute(attr))
	}
	return attrs
}

// convertAttribute converts a single attribute to protobuf format
func (p *StructuralBridgeProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *StructuralBridgeProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
	av := new(commonpb.AnyValue)
	switch v.Type() {
	case attribute.STRING:
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: v.AsString(),
		}
	case attribute.INT64:
		av.Value = &commonpb.AnyValue_IntValue{
			IntValue: v.AsInt64(),
		}
	case attribute.FLOAT64:
		av.Value = &commonpb.AnyValue_DoubleValue{
			DoubleValue: v.AsFloat64(),
		}
	case attribute.BOOL:
		av.Value = &commonpb.AnyValue_BoolValue{
			BoolValue: v.AsBool(),
		}
	case attribute.BOOLSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertBoolSlice(v.AsBoolSlice()),
			},
		}
	case attribute.INT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertInt64Slice(v.AsInt64Slice()),
			},
		}
	case attribute.FLOAT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertFloat64Slice(v.AsFloat64Slice()),
			},
		}
	case attribute.STRINGSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertStringSlice(v.AsStringSlice()),
			},
		}
	default:
		// Fallback to string representation
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: fmt.Sprintf("%v", v.AsInterface()),
		}
	}
	return av
}

// Helper functions for slice conversions
func (p *StructuralBridgeProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		}
	}
	return values
}

// convertSpanKind converts OpenTelemetry span kind to protobuf format
func (p *StructuralBridgeProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindInternal:
		return tracepb.Span_SPAN_KIND_INTERNAL
	case trace.SpanKindServer:
		return tracepb.Span_SPAN_KIND_SERVER
	case trace.SpanKindClient:
		return tracepb.Span_SPAN_KIND_CLIENT
	case trace.SpanKindProducer:
		return tracepb.Span_SPAN_KIND_PRODUCER
	case trace.SpanKindConsumer:
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

// convertStatus converts OpenTelemetry span status to protobuf format
func (p *StructuralBridgeProcessor) convertStatus(status sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{
		Code:    tracepb.Status_StatusCode(status.Code),
		Message: status.Description,
	}
}

// convertAttributes applies the SB processor's wire-emission policy,
// matching the bridges trace_simulator model exactly:
//
//  1. Strip every `__bag.*` attribute — those are intra-process signals
//     (OnStart-to-OnEnd priority, outgoing baggage triggers, etc.) and
//     never belong on the wire.
//  2. Strip Blueprint instrumentation helpers (`depth`, `hasChildren`,
//     `childCount`, `eventCount`, `remEndEvents`) — these are used by
//     OnEnd's classification logic, not part of the wire payload.
//  3. Strip `_br` (AttrBREmit, the breadcrumb wire-emission) unless
//     this span ended up high-priority. The breadcrumb's PRESENCE on
//     the wire IS the checkpoint signal — there is no separate
//     priority bit. Matches trace_simulator's emit-iff-checkpoint model
//     and the simulator's `BRPropertyNameOverheadBytes + TypeID +
//     payload` accounting (we encode the type marker implicitly via
//     the attribute key "_br").
//  4. Pass every other attribute through unchanged.
//
// Build the result with `append`, not by fixed-index assignment, so
// skipped entries don't leave nil holes in the returned slice.
func (p *StructuralBridgeProcessor) convertAttributes(attrs []attribute.KeyValue, highPriority bool) []*commonpb.KeyValue {
	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		// (1) intra-process baggage signals — never go on the wire.
		if strings.HasPrefix(string(attr.Key), "__bag.") {
			continue
		}
		// (2) Blueprint wrapper helpers — also stay intra-process.
		switch attr.Key {
		case "depth", "hasChildren", "childCount", "eventCount", "remEndEvents":
			continue
		}
		// (3) breadcrumb wire-emit: keep iff this is a high-prio span.
		if attr.Key == AttrBREmit && !highPriority {
			continue
		}
		// (4) everything else passes through.
		out = append(out, p.convertAttribute(attr))
	}
	return out
}

// convertEvents converts span events to protobuf format
func (p *StructuralBridgeProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	protoEvents := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		protoEvents[i] = &tracepb.Span_Event{
			TimeUnixNano: uint64(event.Time.UnixNano()),
			Name:         event.Name,
			Attributes:   p.convertAttributes(event.Attributes, true), // This will include all attributes
		}
	}
	return protoEvents
}

// convertLinks converts span links to protobuf format
func (p *StructuralBridgeProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
	if len(links) == 0 {
		return nil
	}

	protoLinks := make([]*tracepb.Span_Link, len(links))
	for i, link := range links {
		traceID := link.SpanContext.TraceID()
		spanID := link.SpanContext.SpanID()

		protoLinks[i] = &tracepb.Span_Link{
			TraceId:    traceID[:],
			SpanId:     spanID[:],
			Attributes: p.convertAttributes(link.Attributes, true), // This will include all attributes
		}
	}
	return protoLinks
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
	p.configLock.Unlock()

	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd)
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

	if err := p.client.Stop(ctx); err != nil {
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
