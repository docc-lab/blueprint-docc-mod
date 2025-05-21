package fancygreeter

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
)

// Service can be used by wiring specs to create a fancy greeter service instance with the specified name.
// The service requires a basic greeter service instance to be passed as a dependency.
// In the compiled application, uses the [fancygreeter.SimpleFancyGreeter] implementation from the Blueprint runtime package.
func Service(spec wiring.WiringSpec, name string, basicGreeter string) string {
	// The nodes that we are defining
	serviceName := name + ".service"

	// Define the service instance
	spec.Define(serviceName, &FancyGreeterServiceNode{}, func(namespace wiring.Namespace) (ir.IRNode, error) {
		// Get the basic greeter service instance from the namespace
		var basicGreeterService ir.IRNode
		if err := namespace.Get(basicGreeter, &basicGreeterService); err != nil {
			return nil, err
		}
		return newFancyGreeterServiceNode(name, basicGreeterService)
	})

	// Create a pointer to the service instance
	pointer.CreatePointer[*FancyGreeterServiceNode](spec, name, serviceName)

	// Return the pointer; anybody who wants to access the service instance should do so through the pointer
	return name
}
