package otelcol

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/otelcol"
	"golang.org/x/exp/slog"
)

// Blueprint IR node representing a client to the OpenTelemetry collector container
type OTCollectorClient struct {
	golang.Node
	golang.Instantiable
	ClientName string
	ServerDial *address.DialConfig
	Spec       *workflowspec.Service
}

func newOTCollectorClient(name string, addr *address.DialConfig) (*OTCollectorClient, error) {
	spec, err := workflowspec.GetService[otelcol.OTCollectorTracer]()
	node := &OTCollectorClient{
		ClientName: name,
		ServerDial: addr,
		Spec:       spec,
	}
	return node, err
}

// Implements ir.IRNode
func (node *OTCollectorClient) Name() string {
	return node.ClientName
}

// Implements ir.IRNode
func (node *OTCollectorClient) String() string {
	return node.Name() + " = OTCollectorClient(" + node.ServerDial.Name() + ")"
}

// Implements golang.Instantiable
func (node *OTCollectorClient) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.ClientName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating OTCollectorClient %v in %v/%v", node.ClientName, builder.Info().Package.PackageName, builder.Info().FileName))

	return builder.DeclareConstructor(node.ClientName, node.Spec.Constructor.AsConstructor(), []ir.IRNode{node.ServerDial})
}

// Implements service.ServiceNode
func (node *OTCollectorClient) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesInterface
func (node *OTCollectorClient) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.ProvidesModule
func (node *OTCollectorClient) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

func (node *OTCollectorClient) ImplementsGolangNode() {}

func (node *OTCollectorClient) ImplementsOTCollectorClient() {}
