package tracingagent

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracingagent"
	"golang.org/x/exp/slog"
)

// Blueprint IR node representing a client to the zipkin container
type TracingAgentClient struct {
	golang.Node
	golang.Instantiable

	ClientName   string
	TracingAgent ir.IRNode
	Spec         *workflowspec.Service
}

func newTracingAgentClient(name string, tracingAgent ir.IRNode) (*TracingAgentClient, error) {
	spec, err := workflowspec.GetService[tracingagent.TracingAgentClient]()
	if err != nil {
		return nil, err
	}

	node := &TracingAgentClient{
		ClientName:   name,
		TracingAgent: tracingAgent,
		Spec:         spec,
	}

	return node, err
}

// Implements ir.IRNode
func (node *TracingAgentClient) Name() string {
	return node.ClientName
}

// Implements ir.IRNode
func (node *TracingAgentClient) String() string {
	return node.Name() + " = TracingAgentClient(" + node.TracingAgent.Name() + ")"
}

// Implements golang.Instantiable
func (node *TracingAgentClient) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.ClientName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating TracingAgentClient %v in %v/%v", node.ClientName, builder.Info().Package.PackageName, builder.Info().FileName))

	// The constructor expects a TracingAgentService interface, not a concrete type
	// We need to pass the tracing agent service instance
	return builder.DeclareConstructor(node.ClientName, node.Spec.Constructor.AsConstructor(), []ir.IRNode{node.TracingAgent})
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
