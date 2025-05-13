package opentelemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// FooBarProcessor is a span processor that adds a "foo" attribute with value "bar" to all spans.
type FooBarProcessor struct {
	nextProcessor tracesdk.SpanProcessor
}

// NewFooBarProcessor creates a new FooBarProcessor that wraps the given span processor.
func NewFooBarProcessor(next tracesdk.SpanProcessor) *FooBarProcessor {
	return &FooBarProcessor{
		nextProcessor: next,
	}
}

// OnStart implements the SpanProcessor interface.
func (p *FooBarProcessor) OnStart(parent context.Context, s tracesdk.ReadWriteSpan) {
	// Add the foo=bar attribute to the span
	s.SetAttributes(attribute.String("foo", "bar"))

	// Pass the span to the next processor
	p.nextProcessor.OnStart(parent, s)
}

// OnEnd implements the SpanProcessor interface.
func (p *FooBarProcessor) OnEnd(s tracesdk.ReadOnlySpan) {
	p.nextProcessor.OnEnd(s)
}

// Shutdown implements the SpanProcessor interface.
func (p *FooBarProcessor) Shutdown(ctx context.Context) error {
	return p.nextProcessor.Shutdown(ctx)
}

// ForceFlush implements the SpanProcessor interface.
func (p *FooBarProcessor) ForceFlush(ctx context.Context) error {
	return p.nextProcessor.ForceFlush(ctx)
}
