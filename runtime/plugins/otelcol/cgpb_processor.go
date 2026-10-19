// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CallGraphBridgeProcessor mirrors StructuralBridgeProcessor's
// export/metrics architecture exactly — dual HP/LP buffers,
// priority-homogeneous batched export with the "bridges-priority: hp|lp"
// gRPC metadata header, retry-off client, and the full per-priority counter
// set logged as a `cgpb_processor_metrics` line. The ONLY thing that
// differs from SB is the breadcrumb composition in OnStart (CGPRB =
// full-width-ckpt-anchored PCRB + a window-local hash array; mirrors
// bridges/bridge/cgprb.go) and the `_d`/`_br` wire emission in
// convertAttributes.
type CallGraphBridgeProcessor struct {
	mu sync.RWMutex

	// Tomislav-RetCtx: the OTLP connection (otlp_wire.go); the export bytes are written here.
	conn *otlpConn

	// Configuration
	agentEndpoint string
	ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Tracks in-flight per-batch export goroutines so Shutdown can wait
	// for them to drain before closing the gRPC client.
	exportWG sync.WaitGroup

	// Two physical buffers, separated by CP/LP classification at OnEnd.
	hpBuf      []wireSpan // Tomislav-RetCtx: encoded spans (otlp_wire.go)
	lpBuf      []wireSpan
	eventsLock sync.Mutex

	// Shared Resource proto for every export, captured lazily on first OnEnd.
	resource     []byte // the encoded ResourceSpans.resource field (otlp_wire.go, resourceField)
	resourceOnce sync.Once

	// Per-batch / per-priority counters (all atomic). Mirrors SB exactly.
	spansReceived   int64
	spansFlushed    int64
	spansSent       int64
	batchesSent     int64
	spansDropped    int64
	batchesDropped  int64
	sendDeadline    int64
	sendUnavailable int64
	sendExhausted   int64
	sendCanceled    int64
	sendOther       int64
	cpReceived      int64
	lpReceived      int64
	cpSent          int64
	lpSent          int64
	cpDropped       int64
	lpDropped       int64

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
func NewCallGraphBridgeProcessor(ctx context.Context, agentEndpoint string, configDiscoveryPort string) (*CallGraphBridgeProcessor, error) {
	slog.Info("🔵 Creating new CallGraphBridgeProcessor", "agentEndpoint", agentEndpoint)

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

	slog.Info("✅ CallGraphBridgeProcessor created successfully")

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

	processor := &CallGraphBridgeProcessor{
		conn:                conn,
		agentEndpoint:       agentEndpoint,
		bloomFilter:         bloom.New(BloomFilterM, BloomFilterK),
		stopChan:            make(chan struct{}),
		ancestryMode:        AncestryModePB,
		configDiscoveryPort: configDiscoveryPortInt,
		httpClient:          httpClient,
		configMap:           make(map[string]interface{}),
		checkpointDistance:  1, // Default: every span is a checkpoint
	}

	slog.Info("🔵 Ancestry mode configured", "mode", AncestryModePB)

	// CGPRB bloom geometry: capacity = cpd-1 (PCRB window), fp =
	// DefaultBloomFPRate. checkpointDistance is the default (1) here; re-sized
	// in fetchFullConfig once the real cpd is discovered.
	expectedElements := uint(pbBloomCapacity(int(processor.checkpointDistance)))
	if expectedElements == 0 {
		expectedElements = 1 // Ensure at least 1 element
	}
	calculatedM, calculatedK := bloom.EstimateParameters(expectedElements, DefaultBloomFPRate)
	BloomFilterM = calculatedM
	BloomFilterK = calculatedK
	backend.SetRTBloomParams(calculatedM, calculatedK)

	// Fetch full config from config discovery endpoint
	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
		if errors.Is(err, errInvalidCheckpointRange) || errors.Is(err, errInvalidReversePolicy) {
			_ = conn.close()
			return nil, err
		}
		slog.Error("❌ Failed to fetch full config", "error", err)
		slog.Warn("⚠️ Continuing with empty config map")
	} else {
		slog.Info("🟢 Successfully fetched full config", "config_keys", len(processor.configMap))
	}

	// Start background worker for batch export
	processor.wg.Add(1)
	go processor.processEvents()

	// Start the metrics logger goroutine.
	processor.wg.Add(1)
	go processor.logMetricsLoop()

	return processor, nil
}

// logMetricsLoop emits one slog.Info line per SBMetricsInterval with the
// full per-priority counter snapshot. Tail via
// `kubectl logs deploy/<svc> | grep cgpb_processor_metrics`.
func (p *CallGraphBridgeProcessor) logMetricsLoop() {
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
// per-priority in-memory buffer depths.
func (p *CallGraphBridgeProcessor) logMetrics() {
	p.eventsLock.Lock()
	hpDepth := len(p.hpBuf)
	lpDepth := len(p.lpBuf)
	p.eventsLock.Unlock()
	slog.Info("cgpb_processor_metrics",
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
	// Tomislav-RetCtx: SDK retry gauges on their own line (sdk_retry.go; parsed as BRIDGES_RETRY)
	sdkRetry.logMetrics()
}

// categorizeSendError increments the appropriate per-code counter for a
// failed UploadTraces.
func (p *CallGraphBridgeProcessor) categorizeSendError(err error) {
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
// buffer and dispatch batches for export.
func (p *CallGraphBridgeProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(SBExportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			p.flushBuffer()
			return
		case <-ticker.C:
			go p.flushBuffer()
		}
	}
}

// flushBuffer atomically snaps both priority buffers, then dispatches HP
// chunks first, then LP chunks. Each chunk carries one priority class
// (never mixed) tagged with a "bridges-priority: hp|lp" gRPC metadata header.
func (p *CallGraphBridgeProcessor) flushBuffer() {
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

	p.dispatchPriority(hpSnap, true)
	p.dispatchPriority(lpSnap, false)
}

// dispatchPriority groups one priority class's snapshot by scope, chunks
// each scope group at SBBatchSize, and spawns one goroutine per chunk.  Each chunk is encoded as
// one ExportTraceServiceRequest (otlp_wire.go, buildBatch): one ResourceSpans (the shared
// Resource) wrapping one ScopeSpans wrapping up to SBBatchSize spans.
func (p *CallGraphBridgeProcessor) dispatchPriority(snap []wireSpan, isHP bool) {
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
func (p *CallGraphBridgeProcessor) exportBatch(batch *wireBatch, isHP bool) {
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
func (p *CallGraphBridgeProcessor) batchDropped(batch *wireBatch, isHP bool, err error) {
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
func (p *CallGraphBridgeProcessor) batchSent(n int64, isHP bool) {
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
func (p *CallGraphBridgeProcessor) sendData(req []byte, isHP bool) error {
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
// adds "__bag." attributes: as for PB, only the legacy fixed-cpd mode does.
func (p *CallGraphBridgeProcessor) startAttributeBaggage() bool { return !p.checkpointRange.enabled() }

// OnStart implements SpanProcessor.OnStart
func (p *CallGraphBridgeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// Tomislav-RetCtx: ranged windows preserve Bloom and trailing HA boundaries.
	if p.checkpointRange.enabled() {
		onStartCheckpointWindow(parent, s, p.checkpointRange, true)
		return
	}
	p.onStartFixed(parent, s)
}

// onStartFixed is OnStart for the legacy fixed-cpd configuration.  Tomislav-RetCtx: it is its own
// function only to keep OnStart's stack frame small.  Go sizes a function's frame for everything in
// it, so while this code was inline, every span start in ranged mode (which returns above) also
// reserved its 1.3 KB, and on goroutines that start with a small stack that is what made the stack
// grow (be copied) during the span start (utils/grpc_interceptors/README.md, "Stack growth").
func (p *CallGraphBridgeProcessor) onStartFixed(parent context.Context, s sdktrace.ReadWriteSpan) {
	// Canonical call-graph path bridge (CGPRB = full-width-ckpt-anchored PCRB + a window-
	// local hash array). Decode inbound baggage `_br` =
	// varint(absolute depth) || ckpt(8) || propagated bloom (fixed ceil(m/8)) || HA.
	bloomLen := int((BloomFilterM + 7) / 8)
	var (
		hasParent        bool
		parentDepth      int
		parentCkpt       [8]byte
		parentBloomBytes []byte
		parentHA         []byte
	)
	if baggage := backend.GetBaggageFromContext(parent); baggage != nil {
		if br, ok := baggage[BaggageBRKey]; ok && br != "" {
			if raw, okB := decodeBR(br); okB {
				if d, c4, bb, hb, okU := unpackCGPRBBR(raw, bloomLen); okU {
					hasParent = true
					parentDepth = d
					parentCkpt = c4
					parentBloomBytes = bb
					parentHA = hb
				}
			}
		}
	}

	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}

	// Absolute depth (root=0); inherited ckpt anchor; INHERITED (pre-self)
	// window bloom.
	var depth int
	var inheritedCkpt [8]byte
	var bf *bloom.BloomFilter
	if hasParent {
		depth = parentDepth + 1
		inheritedCkpt = parentCkpt
		bf = bloom.NewFromBytes(parentBloomBytes, BloomFilterM, BloomFilterK)
	} else {
		depth = 0
		bf = bloom.New(BloomFilterM, BloomFilterK)
	}

	// HA (CGPRB rules): the 1st-started child inherits the parent's window HA;
	// the 2nd-started child starts FRESH and appends one branch record
	// (parent_span_id(8) || varint(absolute depth)); 3rd+ carry none. seqNum is
	// set by the OT client wrapper (per-parent atomic child counter). An entry
	// is thus never duplicated across a fan-out — one record per branching
	// parent asserts the branch exists.
	seqNum, _ := parent.Value("seqNum").(int)
	var ha []byte
	if hasParent && seqNum == 1 {
		ha = append([]byte(nil), parentHA...)
	} else if hasParent && seqNum == 2 {
		parentSpanID := trace.SpanFromContext(parent).SpanContext().SpanID().String()
		ha = haAppendEntry(nil, parentSpanID, depth)
	}

	isCheckpoint := depth%cpd == 0

	spanID := s.SpanContext().SpanID().String()
	sid8, _ := spanIDHexTo8Bytes(spanID)
	var ownCkpt [8]byte
	copy(ownCkpt[:], sid8[:])

	// Emit payload (checkpoints + leaves): names the PREVIOUS checkpoint
	// (inheritedCkpt), the INHERITED pre-self window bloom, and the window HA.
	emitPayload := packCGPRBBR(depth, inheritedCkpt, bf.Bytes(), ha)

	// Propagation + priority. Checkpoint re-roots ckpt→own and resets BOTH the
	// bloom AND the HA to empty (window-scoped). Non-checkpoint keeps anchor,
	// adds self to bloom, carries the HA forward (only the 1st child inherits it).
	priority := 0
	var propCkpt [8]byte
	var propBloomBytes []byte
	propHA := ha
	if isCheckpoint {
		priority = 1
		propCkpt = ownCkpt
		propBloomBytes = bloom.New(BloomFilterM, BloomFilterK).Bytes()
		propHA = nil
	} else {
		propCkpt = inheritedCkpt
		// Span IDs are already uniformly random -> use them directly as bloom hash
		// material (no MurmurHash). Same FPR, ~31% cheaper Add. RAW 8 bytes, not the
		// hex string. See notes/bloom_prehashed_spanid.md.
		bf.AddPrehashed(sid8[:])
		propBloomBytes = bf.Bytes()
	}
	propagationPacked := packCGPRBBR(depth, propCkpt, propBloomBytes, propHA)

	s.SetAttributes(
		attribute.Int(AttrBagPrio, priority),
		attribute.String(AttrBR, encodeBR(propagationPacked)),  // __bag._br: outgoing propagation baggage
		attribute.String(AttrBREmit, encodeBR(emitPayload)),    // "_br": wire payload, kept iff checkpoint/leaf
		attribute.String(AttrD, encodeBR(varintEncode(depth))), // "_d": wire depth, kept iff interior non-checkpoint
		attribute.Int("depth", depth),
	)
}

// OnEnd implements SpanProcessor.OnEnd
func (p *CallGraphBridgeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// Tomislav-RetCtx: honor SDK rejection/receipt decisions at export. One attribute read
	// serves classification and conversion; the window's wire state is taken (and freed) here.
	attrs := s.Attributes()
	w := bridgeWires.take(s.SpanContext().SpanID())
	p.routeToPipeline(s, attrs, w, pathCheckpoint(s.SpanKind(), attrs, w))
}

// routeToPipeline classifies and buffers the span, capturing the shared
// Resource on first use and counting per-priority received totals.
func (p *CallGraphBridgeProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, attrs []attribute.KeyValue, w *bridgeWire, highPriority bool) {
	atomic.AddInt64(&p.spansReceived, 1)
	if highPriority {
		atomic.AddInt64(&p.cpReceived, 1)
	} else {
		atomic.AddInt64(&p.lpReceived, 1)
	}
	p.resourceOnce.Do(func() {
		p.resource = resourceField(s.Resource()) // otlp_wire.go
	})
	// Tomislav-RetCtx: the span's OTLP bytes, written once here (otlp_wire.go); `_br` / `_o` / `_d`
	// come from the wire entry, the attributes through the policyCallGraph strip list.
	entry := encodeSpan(s, attrs, w, highPriority, policyCallGraph)
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
func (p *CallGraphBridgeProcessor) getConfigDiscoveryEndpoint() string {
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			host := parts[0]
			return fmt.Sprintf("%s:%d", host, p.configDiscoveryPort)
		}
	}
	return fmt.Sprintf("localhost:%d", p.configDiscoveryPort)
}

// parseCheckpointDistance extracts and parses the checkpoint distance (cpd) from the config map
func (p *CallGraphBridgeProcessor) parseCheckpointDistance(config map[string]interface{}) int64 {
	const defaultCPD = int64(1) // Default: every span is a checkpoint

	if config == nil {
		return defaultCPD
	}

	if cpdVal, exists := config["cpd"]; exists {
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

	return defaultCPD
}

// fetchFullConfig fetches the full config from the config discovery endpoint
func (p *CallGraphBridgeProcessor) fetchFullConfig(ctx context.Context) error {
	config, err := p.fetchFullConfigFromEndpointWithRetries(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch full config: %w", err)
	}

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
		cpd = 0 // No single fixed distance or global Bloom geometry in range mode.
	}

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

	if !rangeConfig.enabled() {
		// Legacy fixed mode retains its original geometry and wire format.
		bm, bk := bloom.EstimateParameters(uint(pbBloomCapacity(int(cpd))), DefaultBloomFPRate)
		BloomFilterM = bm
		BloomFilterK = bk
		backend.SetRTBloomParams(bm, bk)
	}

	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd,
		"cpd_min", rangeConfig.min, "cpd_max", rangeConfig.max,
		"reverse_policy", policy.mode, "reverse_probability", policy.probability,
		"reverse_passthrough", policy.passthrough)
	return nil
}

// fetchFullConfigFromEndpointWithRetries fetches config from the discovery endpoint with retries
func (p *CallGraphBridgeProcessor) fetchFullConfigFromEndpointWithRetries(ctx context.Context) (map[string]interface{}, error) {
	configDiscoveryEndpoint := p.getConfigDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getFullConfig", configDiscoveryEndpoint)

	for attempt := 1; attempt <= 60; attempt++ {
		slog.Debug("Attempting config discovery", "attempt", attempt, "endpoint", url)

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

		slog.Info("Config discovery successful", "attempt", attempt, "config_keys", len(configResp.Config))
		return configResp.Config, nil
	}

	return nil, fmt.Errorf("config discovery failed after 60 attempts")
}

// getConfigMap returns the config map, with thread-safe access
func (p *CallGraphBridgeProcessor) getConfigMap() map[string]interface{} {
	p.configLock.RLock()
	defer p.configLock.RUnlock()

	result := make(map[string]interface{})
	for k, v := range p.configMap {
		result[k] = v
	}
	return result
}

// Shutdown implements SpanProcessor.Shutdown
func (p *CallGraphBridgeProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 CallGraphBridgeProcessor shutting down")

	// Stop the background workers, then wait for in-flight export goroutines
	// to drain before closing the gRPC client.
	close(p.stopChan)
	p.wg.Wait()
	p.exportWG.Wait()

	if err := p.conn.close(); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	slog.Info("✅ CallGraphBridgeProcessor shutdown complete",
		"spans_sent", atomic.LoadInt64(&p.spansSent),
		"spans_dropped", atomic.LoadInt64(&p.spansDropped))
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *CallGraphBridgeProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil
}
