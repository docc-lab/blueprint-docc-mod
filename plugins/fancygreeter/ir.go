package fancygreeter

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/fancygreeter"
	"golang.org/x/exp/slog"
)

// FancyGreeterServiceNode represents a fancy greeter service in the IR
type FancyGreeterServiceNode struct {
	golang.Service

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.ProvidesInterface
	golang.Instantiable

	InstanceName string
	BasicGreeter ir.IRNode // The actual basic greeter service instance
	Spec         *workflowspec.Service
}

// Creates a new FancyGreeterServiceNode
func newFancyGreeterServiceNode(name string, basicGreeter ir.IRNode) (*FancyGreeterServiceNode, error) {
	spec, err := workflowspec.GetService[fancygreeter.SimpleFancyGreeter]()
	if err != nil {
		return nil, err
	}

	node := &FancyGreeterServiceNode{
		InstanceName: name,
		BasicGreeter: basicGreeter,
		Spec:         spec,
	}

	return node, nil
}

// Implements ir.IRNode
func (node *FancyGreeterServiceNode) Name() string {
	return node.InstanceName
}

// Implements golang.Service
func (node *FancyGreeterServiceNode) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesModule
func (node *FancyGreeterServiceNode) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements golang.ProvidesInterface
func (node *FancyGreeterServiceNode) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.Instantiable
func (node *FancyGreeterServiceNode) AddInstantiation(builder golang.NamespaceBuilder) error {
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating FancyGreeterService %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))

	// Add the basic greeter as a constructor argument
	args := []ir.IRNode{
		node.BasicGreeter,
	}

	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), args)
}

// Implements ir.IRNode
func (node *FancyGreeterServiceNode) String() string {
	return fmt.Sprintf("%v = FancyGreeterService(%v)", node.InstanceName, node.BasicGreeter)
}

func (node *FancyGreeterServiceNode) ImplementsGolangNode()    {}
func (node *FancyGreeterServiceNode) ImplementsGolangService() {}
