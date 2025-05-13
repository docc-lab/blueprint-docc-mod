// Package jaeger implements a tracer [backend.Tracer] client interface for the jaeger tracer.
package jaeger

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracecoordinator"
	jaeger_exporter "go.opentelemetry.io/otel/exporters/jaeger"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// JaegerTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the jaeger collector.
type JaegerTracer struct {
	tp          *tracesdk.TracerProvider
	coordinator tracecoordinator.Coordinator
}

// Returns a new instance of JaegerTracer.
// Configures opentelemetry to export jaeger traces to the jaeger collector hosted at address `addr`.
// If a coordinator is provided, it will be used to coordinate with other tracers.
func NewJaegerTracer(ctx context.Context, addr string, coordinator tracecoordinator.Coordinator) (*JaegerTracer, error) {
	exp, err := jaeger_exporter.New(jaeger_exporter.WithCollectorEndpoint(jaeger_exporter.WithEndpoint("http://" + addr + "/api/traces")))
	if err != nil {
		return nil, err
	}

	// Create the batch processor for exporting spans
	batchProcessor := tracesdk.NewBatchSpanProcessor(exp)

	// Create the tracer provider with the processors
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(batchProcessor),
	)

	// If we have a coordinator, add a coordinated span processor
	if coordinator != nil {
		// Register this tracer with the coordinator
		err = coordinator.RegisterTracer("jaeger", &JaegerTracerCoordinatorClient{})
		if err != nil {
			return nil, err
		}
	}

	return &JaegerTracer{
		tp:          tp,
		coordinator: coordinator,
	}, nil
}

// Implements the backend/trace interface.
func (t *JaegerTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}

// JaegerTracerCoordinatorClient implements the tracecoordinator.Tracer interface for JaegerTracer
type JaegerTracerCoordinatorClient struct{}

// Ping is called by the coordinator to notify the tracer
func (t *JaegerTracerCoordinatorClient) Ping(ctx context.Context) error {
	// Process the ping from the coordinator
	// Add implementation as needed
	return nil
}
