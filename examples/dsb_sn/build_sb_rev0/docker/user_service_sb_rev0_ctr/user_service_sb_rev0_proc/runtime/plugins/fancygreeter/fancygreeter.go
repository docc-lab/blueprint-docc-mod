package fancygreeter

import (
	"context"
	"fmt"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/greeter"
)

// FancyGreeterService defines the interface for our fancy greeter service
type FancyGreeterService interface {
	// GreetWithTitle greets a person with their name and title
	GreetWithTitle(ctx context.Context, name string, title string) (string, error)
	// FarewellWithEmotion bids farewell with an emotional tone
	FarewellWithEmotion(ctx context.Context, name string, emotion string) (string, error)
}

// SimpleFancyGreeter is a basic implementation of the FancyGreeterService
type SimpleFancyGreeter struct {
	basicGreeter greeter.GreeterService
}

// NewSimpleFancyGreeter creates a new instance of SimpleFancyGreeter
func NewSimpleFancyGreeter(ctx context.Context, basicGreeter greeter.GreeterService) (*SimpleFancyGreeter, error) {
	return &SimpleFancyGreeter{
		basicGreeter: basicGreeter,
	}, nil
}

// GreetWithTitle implements the FancyGreeterService interface
func (g *SimpleFancyGreeter) GreetWithTitle(ctx context.Context, name string, title string) (string, error) {
	// Get the basic greeting
	greeting, err := g.basicGreeter.SayHello(ctx, name)
	if err != nil {
		return "", err
	}

	// Add the title
	return fmt.Sprintf("%s Welcome, %s!", greeting, title), nil
}

// FarewellWithEmotion implements the FancyGreeterService interface
func (g *SimpleFancyGreeter) FarewellWithEmotion(ctx context.Context, name string, emotion string) (string, error) {
	// Get the basic farewell
	farewell, err := g.basicGreeter.SayGoodbye(ctx, name)
	if err != nil {
		return "", err
	}

	// Add the emotional tone
	return fmt.Sprintf("%s *%s*", farewell, emotion), nil
}
