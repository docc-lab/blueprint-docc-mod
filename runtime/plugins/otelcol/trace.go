// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// OTCollectorTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the OpenTelemetry collector.
type OTCollectorTracer struct {
	tp *tracesdk.TracerProvider
}

// Returns a new instance of OTCollectorTracer.
// Configures opentelemetry to export traces to the OpenTelemetry collector hosted at address `addr`.
func NewOTCollectorTracer(ctx context.Context, addr string) (*OTCollectorTracer, error) {
	// Create real-time span processor for partial spans (START/END events)
	spanProcessor, err := NewRealTimeSpanProcessor(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create real-time span processor: %w", err)
	}

	// exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())

	// Create tracer provider with the real-time span processor
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(spanProcessor),
	)
	return &OTCollectorTracer{tp}, nil
}

// Implements the backend/trace interface.
func (t *OTCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
