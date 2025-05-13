package zipkin

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// FooBarProcessor is a span processor that adds a "foo" attribute with value "bar" to all spans.
type fooBarProcessor struct {
	nextProcessor tracesdk.SpanProcessor
}

// NewFooBarProcessor creates a new FooBarProcessor that wraps the given span processor.
func newFooBarProcessor(next tracesdk.SpanProcessor) *fooBarProcessor {
	return &fooBarProcessor{
		nextProcessor: next,
	}
}

// OnStart implements the SpanProcessor interface.
func (p *fooBarProcessor) OnStart(parent context.Context, s tracesdk.ReadWriteSpan) {
	// Add the foo=bar attribute to the span
	s.SetAttributes(attribute.String("foo", "bar"))

	// Pass the span to the next processor
	p.nextProcessor.OnStart(parent, s)
}

// OnEnd implements the SpanProcessor interface.
func (p *fooBarProcessor) OnEnd(s tracesdk.ReadOnlySpan) {
	p.nextProcessor.OnEnd(s)
}

// Shutdown implements the SpanProcessor interface.
func (p *fooBarProcessor) Shutdown(ctx context.Context) error {
	return p.nextProcessor.Shutdown(ctx)
}

// ForceFlush implements the SpanProcessor interface.
func (p *fooBarProcessor) ForceFlush(ctx context.Context) error {
	return p.nextProcessor.ForceFlush(ctx)
}
