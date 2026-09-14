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
const reverseDepthAttribute = "__bag." + reverseDepthBaggageKey
const checkpointPreparedAttribute = "__bag.rev_prepared"
const checkpointRejectedAttribute = "__bag.rev_rejected"
const checkpointScheduledAttribute = "__bag.rev_scheduled"

// Tomislav-RetCtx: reverseCheckpointProcessor finalizes leaf rejection and
// independent reverse TTLs/probability trials, preserving each truss's origin.
// All spans still reach the normal processor and export buffer. Checkpoint
// rejection changes the exported span's checkpoint role, not its admission.
type reverseCheckpointProcessor struct {
	sdktrace.SpanProcessor
	kind           string
	scheduled      func(sdktrace.ReadOnlySpan) bool
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
		SpanProcessor: next, root: backend.IsRoot(),
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
		p.kind, p.scheduled = backend.SegStructuralCheckpoint, next.isScheduledCheckpoint
		p.reverseRange, distance = next.checkpointRange, next.checkpointDistance
		p.reversePolicy = next.reversePolicy
	case *VanillaProcessor:
		p.kind = backend.SegVanillaCheckpoint
		p.scheduled = func(sdktrace.ReadOnlySpan) bool { return true }
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
	depth := uint64(0)
	if value := backend.GetBaggageFromContext(parent)[reverseDepthBaggageKey]; value != "" {
		if n, err := strconv.ParseUint(value, 10, 64); err == nil && n < math.MaxUint64 {
			depth = n + 1
		}
	}
	if p.kind == backend.SegPathCheckpoint || p.kind == backend.SegCallGraphCheckpoint {
		// These bridge formats already contain absolute depth. Use the SDK's
		// value even when the parent did not send the auxiliary reverse depth.
		for _, attr := range span.Attributes() {
			if attr.Key == AttrBREmit {
				if d, ok := decodeBRDepth(attr.Value.AsString()); ok {
					depth = uint64(d)
				}
			}
		}
	}
	// SB's _br contains depth modulo cpd. Keep its format intact and carry
	// absolute span depth separately while reverse checkpointing is enabled.
	// Tomislav-RetCtx: preserve the OnStart role before reverse arrivals can
	// create an additional checkpoint at OnEnd. Reverse emission must not change
	// that role or reset its forward window.
	span.SetAttributes(
		attribute.String(reverseDepthAttribute, strconv.FormatUint(depth, 10)),
		attribute.Bool(checkpointScheduledAttribute, p.scheduled(span)),
	)
}

// PrepareCheckpoint consumes __bag.rev_in and writes the SDK's final decision
// before Span.End freezes the attributes. Tomislav-RetCtx: both client and server
// spans spend one reverse hop or probability trial per truss. Repeated
// preparation and fan-in spend none.
func (p *reverseCheckpointProcessor) PrepareCheckpoint(span trace.Span) {
	s, ok := span.(sdktrace.ReadWriteSpan)
	if !ok || !s.IsRecording() {
		return
	}
	var depth uint64
	var truss []byte
	var returned string
	var forceLP, scheduled bool
	for _, attr := range s.Attributes() {
		switch attr.Key {
		case checkpointPreparedAttribute:
			if attr.Value.AsBool() {
				return
			}
		case checkpointScheduledAttribute:
			scheduled = attr.Value.AsBool()
		case reverseDepthAttribute:
			depth, _ = strconv.ParseUint(attr.Value.AsString(), 10, 64)
		case AttrBREmit:
			truss, _ = decodeBR(attr.Value.AsString())
		case backend.ReverseTrussInputKey:
			returned = attr.Value.AsString()
		case AttrForceLP:
			forceLP = p.kind == backend.SegStructuralCheckpoint && attr.Value.AsBool()
		}
	}
	s.SetAttributes(attribute.Bool(checkpointPreparedAttribute, true))
	root := depth == 0 || (p.root && s.SpanKind() == trace.SpanKindServer)
	if returned != "" {
		backend.CountTrussReceived()
		var emitted string
		if !forceLP {
			emitted, returned = p.reversePolicy.route(returned, scheduled || root, depth, rand.Float64)
		}
		if emitted != "" {
			s.SetAttributes(attribute.String(backend.ReverseTrussCheckpointKey, emitted))
			backend.CountCheckpoint()
			backend.SampleLogCheckpoint(emitted)
		}
	}
	// Only forced, unscheduled leaves may reject. Original checkpoints retain
	// their own data and terminate every returned truss, regardless of policy.
	if s.SpanKind() == trace.SpanKindServer && !spanHasChildren(s) && !forceLP && !scheduled && !root && p.leafRejectRate > 0 && len(truss) > 0 {
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
	if returned != "" {
		s.SetAttributes(attribute.String(backend.ReverseBaggageKey, returned), attribute.Bool("bridges.forward_up", true))
	}
}

// checkpointTracerProvider exposes the preparation hook without replacing OTel
// spans or their End method (including its panic and sampling behavior).
type checkpointTracerProvider struct {
	trace.TracerProvider
	backend.CheckpointPreparer
}

// isPathCheckpoint is shared by PB/CGPB finalization and export classification.
func isPathCheckpoint(s sdktrace.ReadOnlySpan) bool {
	var priority, hasPriority, rejected, reverseCheckpoint bool
	for _, attr := range s.Attributes() {
		switch attr.Key {
		case checkpointRejectedAttribute:
			rejected = attr.Value.AsBool()
		case backend.ReverseTrussCheckpointKey:
			reverseCheckpoint = attr.Value.AsString() != ""
		case AttrBagPrio:
			priority, hasPriority = attr.Value.AsInt64() == 1, true
		}
	}
	return reverseCheckpoint || (!rejected && hasPriority && (priority || (s.SpanKind() == trace.SpanKindServer && !spanHasChildren(s))))
}

func isScheduledPathCheckpoint(s sdktrace.ReadOnlySpan) bool {
	for _, attr := range s.Attributes() {
		if attr.Key == AttrBagPrio {
			return attr.Value.AsInt64() == 1
		}
	}
	return false
}

func spanHasChildren(s sdktrace.ReadOnlySpan) bool {
	for _, attr := range s.Attributes() {
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

func (p *StructuralBridgeProcessor) isScheduledCheckpoint(s sdktrace.ReadOnlySpan) bool {
	var depth int
	var hasDepth bool
	for _, attr := range s.Attributes() {
		if attr.Key == AttrBREmit {
			depth, hasDepth = decodeBRDepth(attr.Value.AsString())
		}
	}
	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}
	if p.checkpointRange.enabled() {
		return hasDepth && depth == 0
	}
	return hasDepth && depth%cpd == 0
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
	return !forceLP && (reverseCheckpoint || (!rejected && (p.isScheduledCheckpoint(s) || (s.SpanKind() == trace.SpanKindServer && !spanHasChildren(s)))))
}
