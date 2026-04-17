package fanciergreeter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/greeter"
	"go.opentelemetry.io/otel/trace"
)

// FancierGreeterService extends the basic Greeter service with additional functionality
type FancierGreeterService interface {
	// GreetWithTitle greets a person with their name and title
	GreetWithTitle(ctx context.Context, name string, title string) (string, error)
	// FarewellWithEmotion bids farewell with an emotional tone
	FarewellWithEmotion(ctx context.Context, name string, emotion string) (string, error)
}

// SimpleFancierGreeter implements the FancierGreeterService interface
type SimpleFancierGreeter struct {
	basicGreeter greeter.GreeterService
}

// NewSimpleFancierGreeter creates a new SimpleFancierGreeter instance
func NewSimpleFancierGreeter(ctx context.Context, basicGreeter greeter.GreeterService) (*SimpleFancierGreeter, error) {
	return &SimpleFancierGreeter{
		basicGreeter: basicGreeter,
	}, nil
}

// GreetWithTitle implements the FancierGreeterService interface
func (g *SimpleFancierGreeter) GreetWithTitle(ctx context.Context, name string, title string) (string, error) {
	slog.Info("FancierGreeter: About to call basic greeter",
		"trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
		"span_id", trace.SpanFromContext(ctx).SpanContext().SpanID().String())

	// Use the internal basic greeter to get the base greeting
	greeting, err := g.basicGreeter.SayHello(ctx, name)
	if err != nil {
		return "", err
	}

	slog.Info("FancierGreeter: Called basic greeter",
		"trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
		"span_id", trace.SpanFromContext(ctx).SpanContext().SpanID().String(),
		"error", err)

	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s, %s", greeting, title), nil
}

// FarewellWithEmotion implements the FancierGreeterService interface
func (g *SimpleFancierGreeter) FarewellWithEmotion(ctx context.Context, name string, emotion string) (string, error) {
	// Use the internal basic greeter to get the base farewell
	farewell, err := g.basicGreeter.SayGoodbye(ctx, name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s", farewell, emotion), nil
}
