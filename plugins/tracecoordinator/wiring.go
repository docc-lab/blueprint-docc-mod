// Package tracecoordinator provides a plugin for coordinating between different tracing systems.
// It allows tracers like OpenTelemetry, Jaeger, and Zipkin to communicate with each other
// through a central coordinator.
//
// # Wiring Spec Usage
//
// To instantiate a trace coordinator in your wiring spec, use the NewCoordinator function:
//
//	coordinator := tracecoordinator.NewCoordinator(spec, "my_coordinator")
//
// After instantiating the coordinator, it can be provided as an argument to tracer services
// that need to communicate with each other.
//
// # Description
//
// The trace coordinator is responsible for managing communication between different tracing
// systems. When a tracer (like OpenTelemetry) receives a span, it notifies the coordinator
// about the new span. The coordinator then ensures that other tracers (like Jaeger or Zipkin)
// are aware of this span and can correlate it with their own spans.
package tracecoordinator

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
)

// NewCoordinator creates a new trace coordinator instance with the specified name.
// The coordinator will be responsible for managing communication between different
// tracing systems.
func NewCoordinator(spec wiring.WiringSpec, name string) string {
	// The nodes that we are defining
	coordinatorName := name + ".coordinator"

	// Define the coordinator instance
	spec.Define(coordinatorName, &TraceCoordinator{}, func(namespace wiring.Namespace) (ir.IRNode, error) {
		return newTraceCoordinator(name)
	})

	// Create a pointer to the coordinator instance
	pointer.CreatePointer[*TraceCoordinator](spec, name, coordinatorName)

	// Return the pointer; anybody who wants to access the coordinator instance should do so through the pointer
	return name
}
