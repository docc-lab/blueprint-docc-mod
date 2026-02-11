// Package jaeger implements a tracer [backend.Tracer] client interface for the jaeger tracer.
package jaeger

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// JaegerTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the jaeger collector.
type JaegerTracer struct {
	tp *tracesdk.TracerProvider
}

// Returns a new instance of JaegerTracer.
// Configures opentelemetry to export jaeger traces to the jaeger collector hosted at address `addr` using OTLP/gRPC.
func NewJaegerTracer(ctx context.Context, addr string) (*JaegerTracer, error) {
	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	tp := tracesdk.NewTracerProvider(
		// Always be sure to batch in production.
		tracesdk.WithBatcher(exp),
	)
	return &JaegerTracer{tp}, nil
}

// Implements the backend/trace interface.
func (t *JaegerTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
