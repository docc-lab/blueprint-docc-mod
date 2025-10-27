package greeter

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
)

// Service can be used by wiring specs to create a greeter service instance with the specified name.
// In the compiled application, uses the [greeter.SimpleGreeter] implementation from the Blueprint runtime package.
func Service(spec wiring.WiringSpec, name string) string {
	// The nodes that we are defining
	serviceName := name + ".service"

	// Define the service instance
	spec.Define(serviceName, &GreeterServiceNode{}, func(namespace wiring.Namespace) (ir.IRNode, error) {
		return newGreeterServiceNode(name)
	})

	// Create a pointer to the service instance
	pointer.CreatePointer[*GreeterServiceNode](spec, name, serviceName)

	// Return the pointer; anybody who wants to access the service instance should do so through the pointer
	return name
}
