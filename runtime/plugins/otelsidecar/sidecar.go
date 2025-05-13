package otelsidecar

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// OtelSidecar represents an OpenTelemetry sidecar that can collect and forward traces
type OtelSidecar struct {
	tp *sdktrace.TracerProvider
}

// NewOtelSidecar creates a new OpenTelemetry sidecar instance
func NewOtelSidecar(ctx context.Context, collectorAddr string) (*OtelSidecar, error) {
	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(collectorAddr),
	)
	if err != nil {
		return nil, err
	}

	// Create batch span processor
	bsp := sdktrace.NewBatchSpanProcessor(exporter)

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
	)

	return &OtelSidecar{
		tp: tp,
	}, nil
}

// GetTracerProvider returns the tracer provider for the sidecar
func (s *OtelSidecar) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return s.tp, nil
}

// Shutdown gracefully shuts down the sidecar
func (s *OtelSidecar) Shutdown(ctx context.Context) error {
	return s.tp.Shutdown(ctx)
}
