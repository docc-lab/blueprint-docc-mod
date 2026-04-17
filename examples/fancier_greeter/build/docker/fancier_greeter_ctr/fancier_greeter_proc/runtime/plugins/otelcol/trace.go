// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
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
	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
	)
	return &OTCollectorTracer{tp}, nil
}

// Implements the backend/trace interface.
func (t *OTCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
