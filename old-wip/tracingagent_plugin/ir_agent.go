// Package tracingagent contains IR node definitions for the tracing agent plugin.
//
// This file is analogous to plugins/jaeger/ir_collector.go.
// The client IR node is now in ir_agent_client.go.
package tracingagent

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracingagent"
)

// Blueprint IR node that represents the tracing agent service
type TracingAgentContainer struct {
	golang.Node
	service.ServiceNode
	golang.Instantiable

	AgentName        string
	CentralCollector string
	BindAddr         *address.BindConfig

	Iface *goparser.ParsedInterface
	Spec  *workflowspec.Service
}

// TracingAgent interface exposed to the application.
type TracingAgentInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (t *TracingAgentInterface) GetName() string {
	return "t(" + t.Wrapped.GetName() + ")"
}

func (t *TracingAgentInterface) GetMethods() []service.Method {
	return t.Wrapped.GetMethods()
}

func newTracingAgentContainer(name string, centralCollector string) (*TracingAgentContainer, error) {
	spec, err := workflowspec.GetService[tracingagent.TracingAgent]()
	if err != nil {
		return nil, err
	}

	agent := &TracingAgentContainer{
		AgentName:        name,
		CentralCollector: centralCollector,
		Iface:            spec.Iface,
		Spec:             spec,
	}
	return agent, nil
}

func (node *TracingAgentContainer) Name() string {
	return node.AgentName
}

func (node *TracingAgentContainer) String() string {
	return node.Name() + " = TracingAgent(" + node.BindAddr.Name() + ")"
}

func (node *TracingAgentContainer) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface := node.Iface.ServiceInterface(ctx)
	return &TracingAgentInterface{Wrapped: iface}, nil
}

// Implements golang.Instantiable
func (node *TracingAgentContainer) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.AgentName) {
		return nil
	}

	return builder.DeclareConstructor(node.AgentName, node.Spec.Constructor.AsConstructor(), []ir.IRNode{})
}

// Implements service.ServiceNode
func (node *TracingAgentContainer) GetServiceInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.GetInterface(ctx)
}

// Implements golang.ProvidesInterface
func (node *TracingAgentContainer) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.ProvidesModule
func (node *TracingAgentContainer) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements docker.Container for deployment
func (node *TracingAgentContainer) AddContainerArtifacts(target docker.ContainerWorkspace) error {
	return nil
}

func (node *TracingAgentContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	// The agent is a service we build, not a pre-built container
	// This will be handled by the golang plugin during container generation
	return nil
}

func (node *TracingAgentContainer) ImplementsGolangNode() {}

func (node *TracingAgentContainer) ImplementsOTCollectorClient() {}
