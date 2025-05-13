// Package zipkin implements a tracer [backend.Tracer] client interface for the zipkin tracer.
package zipkin

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracecoordinator"
	"go.opentelemetry.io/otel/exporters/zipkin"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ZipkinTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the zipkin collector.
type ZipkinTracer struct {
	tp          *tracesdk.TracerProvider
	coordinator tracecoordinator.Coordinator
}

// Returns a new instance of ZipkinTracer.
// Configures opentelemetry to export zipkin traces to the zipkin collector hosted at address `addr`.
// If a coordinator is provided, it will be used to coordinate with other tracers.
func NewZipkinTracer(ctx context.Context, addr string, coordinator tracecoordinator.Coordinator) (*ZipkinTracer, error) {
	exp, err := zipkin.New("http://" + addr + "/api/v2/spans")
	if err != nil {
		return nil, err
	}

	// Create the batcher processor
	batcher := tracesdk.NewBatchSpanProcessor(exp)

	// Create our FooBarProcessor that wraps the batcher
	foobarProcessor := newFooBarProcessor(batcher)

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(foobarProcessor),
	)

	// If we have a coordinator, register this tracer with it
	if coordinator != nil {
		err = coordinator.RegisterTracer("zipkin", &ZipkinTracerCoordinatorClient{})
		if err != nil {
			return nil, err
		}
	}

	return &ZipkinTracer{
		tp:          tp,
		coordinator: coordinator,
	}, nil
}

// Implements the backend/trace interface.
func (t *ZipkinTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}

// ZipkinTracerCoordinatorClient implements the tracecoordinator.Tracer interface for ZipkinTracer
type ZipkinTracerCoordinatorClient struct{}

// Ping is called by the coordinator to notify the tracer
func (t *ZipkinTracerCoordinatorClient) Ping(ctx context.Context) error {
	// Process the ping from the coordinator
	// Add implementation as needed
	return nil
}
