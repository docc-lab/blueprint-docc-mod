package backend

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ReverseBaggageKey is an SDK-to-instrumentation carrier. Its value becomes
// retCtx on the RPC response; it must not enter forward request baggage.
const ReverseBaggageKey = "__bag.rev"

// Tomislav-RetCtx: ReverseTrussInputKey passes received trusses to the SDK on a live span.
// Only the SDK decides whether to consume them or return them in __bag.rev.
const ReverseTrussInputKey = "__bag.rev_in"

// IsReverseBaggageKey identifies private return carriers and SDK decision state.
// None of these attributes should enter forward baggage or span exports.
func IsReverseBaggageKey(key attribute.Key) bool {
	return key == ReverseBaggageKey || strings.HasPrefix(string(key), ReverseBaggageKey+"_")
}

// CheckpointPreparer is implemented by Blueprint's tracing SDK. Its synchronous
// hook finalizes checkpointing while the span is still mutable: OTel OnEnd gets
// a read-only snapshot, which is too late to write reverse baggage.
type CheckpointPreparer interface {
	// Returns the carrier to forward upstream. Tomislav-RetCtx: returning it means
	// the caller does not have to read it back off the span. Reading a recording
	// span's attributes locks it and runs a full dedupe pass with a fresh map, so a
	// read-back costs far more than passing the value.
	PrepareCheckpoint(span trace.Span, returned string) string
}

// Tomislav-RetCtx: PrepareCheckpoint asks the SDK to finish its checkpoint decision. Call once,
// after attaching received trusses and final span attributes, before reading
// reverse baggage and ending the span. False means no SDK decision was possible;
// instrumentation must forward the received trusses unchanged in that case.
func PrepareCheckpoint(provider trace.TracerProvider, span trace.Span, returned string) (string, bool) {
	if !span.IsRecording() {
		return "", false
	}
	if preparer, ok := provider.(CheckpointPreparer); ok {
		return preparer.PrepareCheckpoint(span, returned), true
	}
	return "", false
}

// ReadReverseBaggage extracts the SDK's encoded return carrier from a recording
// span. Instrumentation carries it upstream or adds it to the fan-in accumulator.
func ReadReverseBaggage(span trace.Span) string {
	if readable, ok := span.(interface{ Attributes() []attribute.KeyValue }); ok {
		for _, attr := range readable.Attributes() {
			if attr.Key == ReverseBaggageKey {
				return attr.Value.AsString()
			}
		}
	}
	return ""
}
