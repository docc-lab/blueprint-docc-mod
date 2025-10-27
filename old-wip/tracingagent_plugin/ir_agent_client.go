// Package tracingagent contains IR node definitions for the tracing agent plugin.
//
// This file is analogous to plugins/jaeger/ir_collector_client.go.
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

// Blueprint IR node representing a client to the tracing agent
type TracingAgentClient struct {
	golang.Node
	service.ServiceNode
	golang.Instantiable
	ClientName string
	ServerDial *address.DialConfig

	InstanceName string
	Spec         *workflowspec.Service
}

func newTracingAgentClient(name string, addr *address.DialConfig) (*TracingAgentClient, error) {
	spec, err := workflowspec.GetService[tracingagent.TracingAgentClient]()
	if err != nil {
		return nil, err
	}

	node := &TracingAgentClient{
		InstanceName: name,
		ClientName:   name,
		ServerDial:   addr,
		Spec:         spec,
	}
	return node, nil
}

// Implements ir.IRNode
func (node *TracingAgentClient) Name() string {
	return node.ClientName
}

// Implements ir.IRNode
func (node *TracingAgentClient) String() string {
	return node.Name() + " = TracingAgentClient(" + node.ServerDial.Name() + ")"
}

// Implements golang.Instantiable
func (node *TracingAgentClient) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.ClientName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating TracingAgentClient %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))

	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), []ir.IRNode{node.ServerDial})
}

// Implements service.ServiceNode
func (node *TracingAgentClient) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesInterface
func (node *TracingAgentClient) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.ProvidesModule
func (node *TracingAgentClient) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

func (node *TracingAgentClient) ImplementsGolangNode() {}

func (node *TracingAgentClient) ImplementsOTCollectorClient() {}
