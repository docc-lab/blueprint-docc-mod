package opentelemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// OTelCollectorTracer implements the backend.Tracer interface for sending traces to an OTel collector
type OTelCollectorTracer struct {
	tp *tracesdk.TracerProvider
}

// NewOTelCollectorTracer creates a new OTelCollectorTracer that sends spans to the specified collector endpoint
// addr should be in the format "host:port", e.g., "otel-collector:4317"
func NewOTelCollectorTracer(ctx context.Context, addr string, serviceName string) (*OTelCollectorTracer, error) {
	// Configure OTLP exporter to use gRPC
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(addr),
		otlptracegrpc.WithInsecure(), // Remove in production or use TLS
	)
	
	// Create OTLP exporter
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, err
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create TracerProvider with OTLP exporter
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exporter),
		tracesdk.WithResource(res),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
	)

	return &OTelCollectorTracer{tp: tp}, nil
}

// GetTracerProvider implements the backend.Tracer interface
func (t *OTelCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}

// Shutdown ensures all spans are exported before the application exits
func (t *OTelCollectorTracer) Shutdown(ctx context.Context) error {
	return t.tp.Shutdown(ctx)
}

// Keep the existing StdoutTracer for debug/development purposes
type StdoutTracer struct {
	tp *tracesdk.TracerProvider
}

func NewStdoutTracer(ctx context.Context, addr string) (*StdoutTracer, error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	bsp := tracesdk.NewBatchSpanProcessor(exp)
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(bsp),
	)
	return &StdoutTracer{tp}, nil
}

func (t *StdoutTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}