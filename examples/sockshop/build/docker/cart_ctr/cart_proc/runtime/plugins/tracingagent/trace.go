package tracingagent

import (
	"context"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ZipkinTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the zipkin collector.
type TracingAgentClient struct {
	tp    *tracesdk.TracerProvider
	agent TracingAgentService
}

// Returns a new instance of ZipkinTracer.
// Configures opentelemetry to export zipkin traces to the zipkin collector hosted at address `addr`.
func NewTracingAgentClient(ctx context.Context, agent TracingAgentService) (*TracingAgentClient, error) {
	// exp, err := zipkin.New("http://" + agent.ownIP + "/api/v2/spans")
	// if err != nil {
	// 	return nil, err
	// }

	tp := tracesdk.NewTracerProvider(
	// tracesdk.WithBatcher(exp),
	)

	return &TracingAgentClient{tp, agent}, nil
}

// Implements the backend/trace interface.
func (t *TracingAgentClient) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
