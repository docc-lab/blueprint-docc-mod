package greeter

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/greeter"
	"golang.org/x/exp/slog"
)

// GreeterServiceNode represents a greeter service in the IR
type GreeterServiceNode struct {
	golang.Service

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.ProvidesInterface
	golang.Instantiable

	InstanceName string
	Spec         *workflowspec.Service
}

// Creates a new GreeterServiceNode
func newGreeterServiceNode(name string) (*GreeterServiceNode, error) {
	spec, err := workflowspec.GetService[greeter.SimpleGreeter]()
	if err != nil {
		return nil, err
	}

	node := &GreeterServiceNode{
		InstanceName: name,
		Spec:         spec,
	}

	return node, nil
}

// Implements ir.IRNode
func (node *GreeterServiceNode) Name() string {
	return node.InstanceName
}

// Implements golang.Service
func (node *GreeterServiceNode) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesModule
func (node *GreeterServiceNode) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements golang.ProvidesInterface
func (node *GreeterServiceNode) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.Instantiable
func (node *GreeterServiceNode) AddInstantiation(builder golang.NamespaceBuilder) error {
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating GreeterService %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), nil)
}

// Implements ir.IRNode
func (node *GreeterServiceNode) String() string {
	return fmt.Sprintf("%v = GreeterService()", node.InstanceName)
}

func (node *GreeterServiceNode) ImplementsGolangNode()    {}
func (node *GreeterServiceNode) ImplementsGolangService() {}
