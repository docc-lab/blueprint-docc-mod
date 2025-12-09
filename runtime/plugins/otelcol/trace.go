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

// Research Code:
// Returns a new instance of OTCollectorTracer.
// Configures opentelemetry to export traces to the OpenTelemetry collector hosted at address `addr`.
// The ipDiscoveryPort parameter is used as the config discovery port for fetching configuration.
func NewOTCollectorTracer(ctx context.Context, addr string, additionalPort string) (*OTCollectorTracer, error) {
	// Create priority span processor for priority-based routing
	// ipDiscoveryPort is used as configDiscoveryPort for fetching full config

	// spanProcessor, err := NewPriorityProcessor(ctx, addr, additionalPort)

	// spanProcessor, err := NewVanillaProcessor(ctx, addr, additionalPort)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create vanilla span processor: %w", err)
	// }

	// spanProcessor, err := NewPathBridgeProcessor(ctx, addr, additionalPort)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create path-bridge span processor: %w", err)
	// }

	// spanProcessor, err := NewExactBridgeProcessor(ctx, addr, additionalPort)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create exact-bridge span processor: %w", err)
	// }

	// spanProcessor, err := NewCallGraphBridgeProcessor(ctx, addr, additionalPort)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create call-graph bridge span processor: %w", err)
	// }

	spanProcessor, err := NewStructuralBridgeProcessor(ctx, addr, additionalPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create structural bridge span processor: %w", err)
	}

	// Commented out: Real-time span processor for partial spans (START/END events)
	// spanProcessor, err := NewRealTimeSpanProcessor(ctx, addr, ipDiscoveryPort)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create real-time span processor: %w", err)
	// }

	// exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())

	// Create tracer provider with the priority span processor
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(spanProcessor),
	)
	return &OTCollectorTracer{tp}, nil
}

// // Vanilla Code:
// func NewOTCollectorTracer(ctx context.Context, addr string, additionalPort string) (*OTCollectorTracer, error) {
// 	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())
// 	if err != nil {
// 		return nil, err
// 	}
// 	tp := tracesdk.NewTracerProvider(
// 		// Always be sure to batch in production.
// 		tracesdk.WithBatcher(exp),
// 	)
// 	return &OTCollectorTracer{tp}, nil
// }

// Implements the backend/trace interface.
func (t *OTCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
