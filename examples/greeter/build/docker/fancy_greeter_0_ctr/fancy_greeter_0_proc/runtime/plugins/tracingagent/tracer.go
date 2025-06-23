// Package tracingagent implements a tracer [backend.Tracer] client interface for the tracing agent.
package tracingagent

import (
	"context"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TracingAgentTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional tracing agent running at the specified address.
type TracingAgentTracer struct {
	tp *tracesdk.TracerProvider
}

// Returns a new instance of TracingAgentTracer.
// Configures opentelemetry to export traces to the tracing agent hosted at address `addr`.
func NewTracingAgentTracer(ctx context.Context, addr string) (*TracingAgentTracer, error) {
	// TODO: Implement proper trace exporter that sends to the tracing agent
	// For now, create a basic tracer provider
	tp := tracesdk.NewTracerProvider()
	return &TracingAgentTracer{tp}, nil
}

// Implements the backend/trace interface.
func (t *TracingAgentTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
