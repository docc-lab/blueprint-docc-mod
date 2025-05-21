package fanciergreeter

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/fanciergreeter"
	"golang.org/x/exp/slog"
)

// FancierGreeterServiceNode represents a fancier greeter service in the IR
type FancierGreeterServiceNode struct {
	golang.Service

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.ProvidesInterface
	golang.Instantiable

	InstanceName string
	BasicGreeter ir.IRNode // The actual basic greeter service instance
	Spec         *workflowspec.Service
}

// Creates a new FancierGreeterServiceNode
func newFancierGreeterServiceNode(name string, basicGreeter ir.IRNode) (*FancierGreeterServiceNode, error) {
	spec, err := workflowspec.GetService[fanciergreeter.SimpleFancierGreeter]()
	if err != nil {
		return nil, err
	}

	node := &FancierGreeterServiceNode{
		InstanceName: name,
		BasicGreeter: basicGreeter,
		Spec:         spec,
	}

	return node, nil
}

// Implements ir.IRNode
func (node *FancierGreeterServiceNode) Name() string {
	return node.InstanceName
}

// Implements golang.Service
func (node *FancierGreeterServiceNode) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesModule
func (node *FancierGreeterServiceNode) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements golang.ProvidesInterface
func (node *FancierGreeterServiceNode) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.Instantiable
func (node *FancierGreeterServiceNode) AddInstantiation(builder golang.NamespaceBuilder) error {
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating FancierGreeterService %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))

	// Add the basic greeter as a constructor argument
	args := []ir.IRNode{
		node.BasicGreeter,
	}

	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), args)
}

// Implements ir.IRNode
func (node *FancierGreeterServiceNode) String() string {
	return fmt.Sprintf("%v = FancierGreeterService(%v)", node.InstanceName, node.BasicGreeter)
}

func (node *FancierGreeterServiceNode) ImplementsGolangNode()    {}
func (node *FancierGreeterServiceNode) ImplementsGolangService() {}
