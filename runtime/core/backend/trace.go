package backend

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"go.opentelemetry.io/otel/trace"
)

// baggageKey is used as a context key for storing baggage data
type baggageKey struct{}

// Represents a tracer that can be used by the tracer/opentelemetry plugin
type Tracer interface {
	// Returns a go.opentelemetry.io/otel/trace.TracerProvider
	// TracerProvider provides Tracers that are used by instrumentation code to trace computational workflows.
	GetTracerProvider(ctx context.Context) (trace.TracerProvider, error)
}

// traceCtx mimics the internal trace context object from OpenTelemetry.
// Included here to be able to implement and provide the `GetSpanContext` function.
type traceCtx struct {
	// ID of the current trace
	TraceID string
	// ID of the current span
	SpanID string
	// Additional flags for the trace
	TraceFlags string
	// Additional state for the trace
	TraceState string
	// If span is a remote span
	Remote bool
}

type traceCtxWithBaggage struct {
	TraceCtx traceCtx `json:"trace_ctx"`
	// Baggage for propagating arbitrary key-value pairs across service boundaries
	Baggage map[string]string `json:"baggage,omitempty"`
}

// Utility function to convert an encoded string into a Span Context
func GetSpanContext(encoded_string string) (trace.SpanContextConfig, map[string]string, error) {
	var tCtx traceCtxWithBaggage
	err := json.Unmarshal([]byte(encoded_string), &tCtx)
	if err != nil {
		return trace.SpanContextConfig{}, nil, err
	}
	tid, err := trace.TraceIDFromHex(tCtx.TraceCtx.TraceID)
	if err != nil {
		return trace.SpanContextConfig{}, nil, err
	}
	sid, err := trace.SpanIDFromHex(tCtx.TraceCtx.SpanID)
	if err != nil {
		return trace.SpanContextConfig{}, nil, err
	}
	flag_bytes, err := hex.DecodeString(tCtx.TraceCtx.TraceFlags)
	if err != nil {
		return trace.SpanContextConfig{}, nil, err
	}
	tFlags := trace.TraceFlags(flag_bytes[0])
	tState, err := trace.ParseTraceState(tCtx.TraceCtx.TraceState)
	if err != nil {
		return trace.SpanContextConfig{}, nil, err
	}
	return trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: tFlags,
		TraceState: tState,
		Remote:     tCtx.TraceCtx.Remote,
	}, tCtx.Baggage, nil
}

// AddBaggageToTraceContext takes an already JSON-ified trace context string and adds baggage to it
func AddBaggageToTraceContext(traceContextJSON string, baggage map[string]string) (string, error) {
	// Construct the combined JSON directly
	combined := map[string]interface{}{
		"trace_ctx": json.RawMessage(traceContextJSON),
		"baggage":   baggage,
	}

	result, err := json.Marshal(combined)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// SetBaggageInContext adds baggage to the Go context for the span processor to read
func SetBaggageInContext(ctx context.Context, baggage map[string]string) context.Context {
	return context.WithValue(ctx, baggageKey{}, baggage)
}

// GetBaggageFromContext retrieves baggage from the Go context
func GetBaggageFromContext(ctx context.Context) map[string]string {
	if baggage, ok := ctx.Value(baggageKey{}).(map[string]string); ok {
		return baggage
	}
	return nil
}
