package greeter

import (
	"context"
	"fmt"
)

// GreeterService defines the interface for our greeter service
type GreeterService interface {
	// SayHello greets a person by name
	SayHello(ctx context.Context, name string) (string, error)
	// SayGoodbye bids farewell to a person by name
	SayGoodbye(ctx context.Context, name string) (string, error)
}

// SimpleGreeter is a basic implementation of the GreeterService
type SimpleGreeter struct{}

// NewSimpleGreeter creates a new instance of SimpleGreeter
func NewSimpleGreeter(ctx context.Context) (*SimpleGreeter, error) {
	return &SimpleGreeter{}, nil
}

// SayHello implements the GreeterService interface
func (g *SimpleGreeter) SayHello(ctx context.Context, name string) (string, error) {
	return fmt.Sprintf("Hello, %s!", name), nil
}

// SayGoodbye implements the GreeterService interface
func (g *SimpleGreeter) SayGoodbye(ctx context.Context, name string) (string, error) {
	return fmt.Sprintf("Goodbye, %s!", name), nil
}
