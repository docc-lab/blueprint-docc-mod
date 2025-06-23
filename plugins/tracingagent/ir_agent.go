package tracingagent

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracingagent"
	"golang.org/x/exp/slog"
)

// GreeterServiceNode represents a greeter service in the IR
type TracingAgentServiceNode struct {
	golang.Service

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.ProvidesInterface
	golang.Instantiable

	InstanceName string
	BindAddr     *address.BindConfig
	Spec         *workflowspec.Service
}

// Creates a new TracingAgentServiceNode
func newTracingAgentServiceNode(name string) (*TracingAgentServiceNode, error) {
	spec, err := workflowspec.GetService[tracingagent.TracingAgent]()
	if err != nil {
		return nil, err
	}

	node := &TracingAgentServiceNode{
		InstanceName: name,
		Spec:         spec,
	}

	return node, nil
}

// Implements ir.IRNode
func (node *TracingAgentServiceNode) Name() string {
	return node.InstanceName
}

// Implements golang.Service
func (node *TracingAgentServiceNode) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesModule
func (node *TracingAgentServiceNode) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements golang.ProvidesInterface
func (node *TracingAgentServiceNode) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.Instantiable
func (node *TracingAgentServiceNode) AddInstantiation(builder golang.NamespaceBuilder) error {
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating TracingAgentService %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), nil)
}

// Implements ir.IRNode
func (node *TracingAgentServiceNode) String() string {
	return fmt.Sprintf("%v = TracingAgentService()", node.InstanceName)
}

func (node *TracingAgentServiceNode) ImplementsGolangNode()    {}
func (node *TracingAgentServiceNode) ImplementsGolangService() {}
