package opentelemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// OTelSidecarAgent represents an OpenTelemetry sidecar agent that runs alongside a service
// and handles trace collection and forwarding
type OTelSidecarAgent struct {
	serviceName       string
	collectorEndpoint string
	port              int
	tracerProvider    *sdktrace.TracerProvider
}

// NewOTelSidecarAgent creates a new OpenTelemetry sidecar agent
func NewOTelSidecarAgent(serviceName string, collectorEndpoint string, port int) (*OTelSidecarAgent, error) {
	// Create OTLP exporter
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, collectorEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create resource with service information
	hostname, _ := os.Hostname()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.HostNameKey.String(hostname),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return &OTelSidecarAgent{
		serviceName:       serviceName,
		collectorEndpoint: collectorEndpoint,
		port:              port,
		tracerProvider:    tp,
	}, nil
}

// Start starts the sidecar agent
func (a *OTelSidecarAgent) Start(ctx context.Context) error {
	// TODO: Implement agent server that listens on the specified port
	return nil
}

// Stop stops the sidecar agent
func (a *OTelSidecarAgent) Stop(ctx context.Context) error {
	if err := a.tracerProvider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}
	return nil
}

// GetTracerProvider returns the tracer provider
func (a *OTelSidecarAgent) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return a.tracerProvider, nil
}

// Name returns the name of the sidecar agent
func (a *OTelSidecarAgent) Name() string {
	return fmt.Sprintf("%s.otel_sidecar", a.serviceName)
}

// String returns a string representation of the sidecar agent
func (a *OTelSidecarAgent) String() string {
	return fmt.Sprintf("OTelSidecarAgent[%s]", a.serviceName)
}
