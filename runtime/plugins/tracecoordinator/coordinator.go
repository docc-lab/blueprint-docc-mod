package tracecoordinator

import (
	"context"
	"sync"
)

// Tracer represents a tracer that can receive pings from the coordinator
type Tracer interface {
	// Ping is called by the coordinator to notify the tracer
	Ping(ctx context.Context) error
}

// Coordinator represents a service that coordinates between tracers
type Coordinator interface {
	// RegisterTracer registers a tracer with the coordinator
	RegisterTracer(name string, tracer Tracer) error

	// PingTracer sends a ping to a registered tracer
	PingTracer(ctx context.Context, tracerName string) error

	// ReceivePing is called by tracers to notify the coordinator
	ReceivePing(ctx context.Context, tracerName string) error
}

// TraceCoordinator is an implementation of the Coordinator interface
type TraceCoordinator struct {
	tracers map[string]Tracer
	mu      sync.RWMutex
}

// NewTraceCoordinator creates a new TraceCoordinator instance
func NewTraceCoordinator() *TraceCoordinator {
	return &TraceCoordinator{
		tracers: make(map[string]Tracer),
	}
}

// RegisterTracer implements Coordinator.RegisterTracer
func (c *TraceCoordinator) RegisterTracer(name string, tracer Tracer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tracers[name] = tracer
	return nil
}

// PingTracer implements Coordinator.PingTracer
func (c *TraceCoordinator) PingTracer(ctx context.Context, tracerName string) error {
	c.mu.RLock()
	tracer, exists := c.tracers[tracerName]
	c.mu.RUnlock()

	if !exists {
		return nil // Tracer not found, silently ignore
	}

	return tracer.Ping(ctx)
}

// ReceivePing implements Coordinator.ReceivePing
func (c *TraceCoordinator) ReceivePing(ctx context.Context, tracerName string) error {
	// For now, just log that we received a ping
	// In the future, this could be used to coordinate between tracers
	return nil
}
