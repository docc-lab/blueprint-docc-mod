package otelcol

import (
	"context"
	"math"
	"math/rand/v2"
	"os"
	"strconv"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const reverseDepthBaggageKey = "__rt_depth"

// Tomislav-RetCtx: reverseDepthAttribute is no longer WRITTEN -- depth, the
// scheduling decision, the prepared flag and the outgoing carrier now live in
// reverseSpanState beside the processor instead of on the span. The key is kept
// because the export path still strips it defensively and a test asserts it never
// reaches a span. rev_prepared and rev_scheduled are gone entirely.
const reverseDepthAttribute = "__bag." + reverseDepthBaggageKey

// Still a span attribute: the export classifier reads it at OnEnd, after the
// per-span state has been released.
const checkpointRejectedAttribute = "__bag.rev_rejected"

// Tomislav-RetCtx: reverseCheckpointProcessor finalizes leaf rejection and
// independent reverse TTLs/probability trials, preserving each truss's origin.
// All spans still reach the normal processor and export buffer. Checkpoint
// rejection changes the exported span's checkpoint role, not its admission.
// Tomislav-RetCtx: per-span state the reverse path needs between OnStart and
// PrepareCheckpoint. It used to ride on the span as `__bag.`-prefixed attributes
// that were stripped at export -- values written only so they could be read back
// one stack frame later. That was expensive twice over: every read locked the span
// and ran a full dedupe pass with a fresh map, and every write lengthened the
// attribute slice that the export path's snapshot() then has to dedupe for EVERY
// span. Keeping them here mirrors what the forward path already does for
// per-request state (ir_ot_server.go stashes childCount, childrenTracker and the
// rest as context-carried pointers and touches the span exactly once, at the end).
//
// Keyed by SpanID and removed at OnEnd, so a span that never ends is the only way
// to retain an entry; in this workload every span ends.
type reverseSpanState struct {
	depth     uint64
	scheduled bool
	prepared  bool
	carried   string
}

type reverseCheckpointProcessor struct {
	sdktrace.SpanProcessor
	states         *reverseStateTable
	kind           string
	scheduled      func([]attribute.KeyValue) bool
	reverseRange   checkpointRange
	reversePolicy  reversePolicy
	leafRejectRate float64
	root           bool
}

func wrapReverseCheckpointProcessor(next sdktrace.SpanProcessor) sdktrace.SpanProcessor {
	if !backend.ReverseTrussEnabled() {
		return next
	}
	p := &reverseCheckpointProcessor{
		SpanProcessor: next, root: backend.IsRoot(), states: newReverseStateTable(),
	}
	var distance int64 = 1
	switch next := next.(type) {
	case *PathBridgeProcessor:
		p.kind, p.scheduled = backend.SegPathCheckpoint, isScheduledPathCheckpoint
		p.reverseRange, distance = next.checkpointRange, next.checkpointDistance
		p.reversePolicy = next.reversePolicy
	case *CallGraphBridgeProcessor:
		p.kind, p.scheduled = backend.SegCallGraphCheckpoint, isScheduledPathCheckpoint
		p.reverseRange, distance = next.checkpointRange, next.checkpointDistance
		p.reversePolicy = next.reversePolicy
	case *StructuralBridgeProcessor:
		p.kind, p.scheduled = backend.SegStructuralCheckpoint, isScheduledPathCheckpoint
		p.reverseRange, distance = next.checkpointRange, next.checkpointDistance
		p.reversePolicy = next.reversePolicy
	case *VanillaProcessor:
		p.kind = backend.SegVanillaCheckpoint
		p.scheduled = func([]attribute.KeyValue) bool { return true }
	default:
		return next
	}
	if !p.reverseRange.enabled() {
		// Fixed-cpd configurations use a fixed reverse distance. Only the
		// backwards byte is bounded here; the legacy forward format is intact.
		n := int(min(max(distance, 1), 256))
		p.reverseRange = checkpointRange{n, n}
	}
	rate, err := strconv.ParseFloat(os.Getenv("RT_LEAF_REJECT"), 64)
	if err == nil && !math.IsNaN(rate) && rate > 0 {
		p.leafRejectRate = math.Min(rate, 1)
	}
	return p
}

func (p *reverseCheckpointProcessor) OnStart(parent context.Context, span sdktrace.ReadWriteSpan) {
	p.SpanProcessor.OnStart(parent, span)
	// Tomislav-RetCtx: the ranged PB/CGPB window records depth and the scheduling decision
	// beside the span (wire_state.go); no attribute scan needed.
	if w := bridgeWires.load(span.SpanContext().SpanID()); w != nil {
		w.rev = reverseSpanState{depth: uint64(w.depth), scheduled: w.priority}
		return
	}
	depth := uint64(0)
	if value := backend.GetBaggageFromContext(parent)[reverseDepthBaggageKey]; value != "" {
		if n, err := strconv.ParseUint(value, 10, 64); err == nil && n < math.MaxUint64 {
			depth = n + 1
		}
	}
	// Tomislav-RetCtx: ONE Attributes() call serves both the truss depth and the
	// scheduling decision. Each call locks the span and runs dedupeAttrs, which
	// allocates a fresh map every time (sdk/trace/span.go): the SDK defers dedupe to
	// read time on the assumption that a span is written often and read once, at
	// export. Reading it twice here cost more than the work being done.
	attrs := span.Attributes()
	if p.kind == backend.SegPathCheckpoint || p.kind == backend.SegCallGraphCheckpoint || p.kind == backend.SegStructuralCheckpoint {
		// These bridge formats already contain absolute depth. Use the SDK's
		// value even when the parent did not send the auxiliary reverse depth.
		for _, attr := range attrs {
			if attr.Key == AttrBREmit {
				if d, ok := decodeBRDepth(attr.Value.AsString()); ok {
					depth = uint64(d)
				}
			}
		}
	}
	// Vanilla has no truss depth; the auxiliary reverse depth carries it.
	// Tomislav-RetCtx: preserve the OnStart role before reverse arrivals can
	// create an additional checkpoint at OnEnd. Reverse emission must not change
	// that role or reset its forward window.
	// No SetAttributes here: depth and the scheduling decision never reach the wire,
	// so they live beside the processor instead of on the span.
	p.states.store(span.SpanContext().SpanID(), &reverseSpanState{
		depth: depth, scheduled: p.scheduled(attrs),
	})
}

// OnEnd releases the per-span state after the bridge processor has exported. (Bridge spans
// keep it in their wire entry, which the bridge's OnEnd frees.)
func (p *reverseCheckpointProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.SpanProcessor.OnEnd(s)
	p.states.delete(s.SpanContext().SpanID()) // spans without a wire entry (vanilla, legacy fixed mode)
}

func (p *reverseCheckpointProcessor) state(id trace.SpanID) *reverseSpanState {
	if w := bridgeWires.load(id); w != nil {
		return &w.rev
	}
	if state := p.states.load(id); state != nil {
		return state
	}
	// A span the wrapper never saw at OnStart (or one already ended) still has to
	// behave: treat it as depth 0 and unscheduled rather than failing the request.
	return &reverseSpanState{}
}

// PrepareCheckpoint consumes __bag.rev_in and writes the SDK's final decision
// before Span.End freezes the attributes. Tomislav-RetCtx: both client and server
// spans spend one reverse hop or probability trial per truss. Repeated
// preparation and fan-in spend none.
// Tomislav-RetCtx: returns the carrier the caller should forward upstream, so the
// generated wrapper no longer has to scan the span for a value written here one
// stack frame earlier. That scan was the single largest consumer of Attributes()
// in the deployed profile (31 % of it on composepost).
func (p *reverseCheckpointProcessor) PrepareCheckpoint(span trace.Span, returned string) string {
	s, ok := span.(sdktrace.ReadWriteSpan)
	if !ok || !s.IsRecording() {
		return ""
	}
	state := p.state(s.SpanContext().SpanID())
	if state.prepared {
		// Repeated preparation is a no-op, but the caller still needs whatever the
		// first call decided to forward.
		return state.carried
	}
	state.prepared = true
	depth, scheduled := state.depth, state.scheduled
	// The remaining two values are the bridge processor's own wire attributes, so
	// they still come off the span -- one scan, where there used to be several.
	var truss []byte
	var forceLP bool
	var attrs []attribute.KeyValue
	// Tomislav-RetCtx: the ranged window's truss is the raw emit payload beside the span;
	// attributes are read only for what still lives there (SB's force_lp, the children
	// markers a server-leaf rejection checks, or a legacy bridge with no wire state).
	w := bridgeWires.load(s.SpanContext().SpanID())
	if w != nil {
		truss = w.emit
	}
	if w == nil || p.kind == backend.SegStructuralCheckpoint || s.SpanKind() == trace.SpanKindServer {
		attrs = s.Attributes()
		for _, attr := range attrs {
			switch attr.Key {
			case AttrBREmit:
				if w == nil {
					truss, _ = decodeBR(attr.Value.AsString())
				}
			case AttrForceLP:
				forceLP = p.kind == backend.SegStructuralCheckpoint && attr.Value.AsBool()
			}
		}
	}
	root := depth == 0 || (p.root && s.SpanKind() == trace.SpanKindServer)
	if returned != "" {
		backend.CountTrussReceived()
		var emitted string
		if !forceLP {
			emitted, returned = p.reversePolicy.route(returned, p.reversePolicy.terminates(scheduled, root), depth, rand.Float64)
		}
		if emitted != "" {
			s.SetAttributes(attribute.String(backend.ReverseTrussCheckpointKey, emitted))
			backend.CountCheckpoint()
			backend.SampleLogCheckpoint(emitted)
		}
	}
	// Only forced, unscheduled leaves may reject. Original checkpoints retain
	// their own data; they terminate every returned truss unless reverse_passthrough
	// is on (reversePolicy.terminates), in which case the policy routes it as above.
	if s.SpanKind() == trace.SpanKindServer && !spanHasChildrenIn(attrs) && !forceLP && !scheduled && !root && p.leafRejectRate > 0 && len(truss) > 0 {
		if rand.Float64() < p.leafRejectRate {
			var own string
			if p.reversePolicy.probabilistic() {
				// Tomislav-RetCtx: receivers use each leaf's existing origin depth;
				// probability mode neither draws nor propagates a reverse TTL.
				own = backend.EncodeCheckpointRetCtx(s.SpanContext().SpanID(), depth, p.kind, truss)
			} else {
				own = backend.EncodeCheckpointRetCtxWithTTL(s.SpanContext().SpanID(), depth, p.kind, truss, p.reverseRange.newTTL())
			}
			returned = backend.MergeRetCtx(returned, own)
			s.SetAttributes(attribute.Bool(checkpointRejectedAttribute, true))
			backend.CountLeafReject()
		} else {
			backend.CountLocalCheckpoint()
		}
	}
	state.carried = returned
	return returned
}

// checkpointTracerProvider exposes the preparation hook without replacing OTel
// spans or their End method (including its panic and sampling behavior).
type checkpointTracerProvider struct {
	trace.TracerProvider
	backend.CheckpointPreparer
}

// isPathCheckpoint is shared by PB/CGPB finalization and export classification.
func isPathCheckpoint(s sdktrace.ReadOnlySpan) bool {
	return pathCheckpoint(s.SpanKind(), s.Attributes(), bridgeWires.load(s.SpanContext().SpanID()))
}

// pathCheckpoint classifies from the attribute slice the caller already holds. The scheduling
// decision comes from the span's wire state when the ranged window recorded one
// (Tomislav-RetCtx, wire_state.go), else from the legacy __bag.prio attribute.
func pathCheckpoint(kind trace.SpanKind, attrs []attribute.KeyValue, w *bridgeWire) bool {
	var priority, hasPriority, rejected, reverseCheckpoint bool
	if w != nil {
		priority, hasPriority = w.priority, true
	}
	for _, attr := range attrs {
		switch attr.Key {
		case checkpointRejectedAttribute:
			rejected = attr.Value.AsBool()
		case backend.ReverseTrussCheckpointKey:
			reverseCheckpoint = attr.Value.AsString() != ""
		case AttrBagPrio:
			priority, hasPriority = attr.Value.AsInt64() == 1, true
		}
	}
	return reverseCheckpoint || (!rejected && hasPriority && (priority || (kind == trace.SpanKindServer && !spanHasChildrenIn(attrs))))
}

func isScheduledPathCheckpoint(attrs []attribute.KeyValue) bool {
	for _, attr := range attrs {
		if attr.Key == AttrBagPrio {
			return attr.Value.AsInt64() == 1
		}
	}
	return false
}

// Tomislav-RetCtx: both helpers take the attribute slice the caller already has.
// They used to re-fetch it, which meant a second locked dedupe pass for data the
// caller was holding.
func spanHasChildren(s sdktrace.ReadOnlySpan) bool { return spanHasChildrenIn(s.Attributes()) }

func spanHasChildrenIn(attrs []attribute.KeyValue) bool {
	for _, attr := range attrs {
		switch attr.Key {
		case "hasChildren":
			if attr.Value.AsBool() {
				return true
			}
		case "childCount", "eventCount":
			if attr.Value.AsInt64() > 0 {
				return true
			}
		}
	}
	return false
}

// structuralCheckpoint is SB's classification from the attribute slice the caller holds:
// the PB/CGPB window classification, with force_lp keeping synthetic pressure spans ordinary.
func structuralCheckpoint(kind trace.SpanKind, attrs []attribute.KeyValue, w *bridgeWire) bool {
	for _, attr := range attrs {
		if attr.Key == AttrForceLP && attr.Value.AsBool() {
			return false
		}
	}
	return pathCheckpoint(kind, attrs, w)
}

func (p *StructuralBridgeProcessor) isCheckpoint(s sdktrace.ReadOnlySpan) bool {
	var forceLP, rejected, reverseCheckpoint bool
	for _, attr := range s.Attributes() {
		switch attr.Key {
		case checkpointRejectedAttribute:
			rejected = attr.Value.AsBool()
		case backend.ReverseTrussCheckpointKey:
			reverseCheckpoint = attr.Value.AsString() != ""
		case AttrForceLP:
			forceLP = attr.Value.AsBool()
		}
	}
	// Tomislav-RetCtx: SB shares the PB/CGPB window classification; force_lp
	// keeps synthetic pressure spans ordinary.
	_, _, _ = rejected, reverseCheckpoint, forceLP
	return !forceLP && isPathCheckpoint(s)
}
