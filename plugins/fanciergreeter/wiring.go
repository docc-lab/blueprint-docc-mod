package fanciergreeter

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/greeter"
)

// Service can be used by wiring specs to create a fancier greeter service instance with the specified name.
// The service creates its own internal basic greeter instance.
// In the compiled application, uses the [fanciergreeter.SimpleFancierGreeter] implementation from the Blueprint runtime package.
func Service(spec wiring.WiringSpec, name string) string {
	// The nodes that we are defining
	serviceName := name + ".service"

	// Create a new basic greeter service instance for internal use
	basicGreeterName := name + "__basic_greeter"
	basicGreeter := greeter.Service(spec, basicGreeterName)

	// Define the service instance
	spec.Define(serviceName, &FancierGreeterServiceNode{}, func(namespace wiring.Namespace) (ir.IRNode, error) {
		// Get the basic greeter service instance from the namespace
		var basicGreeterService ir.IRNode
		if err := namespace.Get(basicGreeter, &basicGreeterService); err != nil {
			return nil, err
		}

		return newFancierGreeterServiceNode(name, basicGreeterService)
	})

	// Create a pointer to the service instance
	pointer.CreatePointer[*FancierGreeterServiceNode](spec, name, serviceName)

	// Return both the fancier greeter and basic greeter names
	return name
}
