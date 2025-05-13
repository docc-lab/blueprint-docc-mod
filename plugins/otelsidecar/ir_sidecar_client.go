package otelsidecar

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/tracecoordinator"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/zipkin"
	"golang.org/x/exp/slog"
)

// Blueprint IR node representing a client to the OpenTelemetry sidecar
type OtelSidecarClient struct {
	golang.Node
	golang.Instantiable
	ClientName    string
	ServerDial    *address.DialConfig
	Coordinator   *tracecoordinator.TraceCoordinator
	Spec          *workflowspec.Service
	CollectorType string // "jaeger" or "zipkin"
}

func newOtelSidecarClient(name string, addr *address.DialConfig, coordinator *tracecoordinator.TraceCoordinator, collectorType string) (*OtelSidecarClient, error) {
	var spec *workflowspec.Service
	var err error

	switch collectorType {
	case "jaeger":
		spec, err = workflowspec.GetService[jaeger.JaegerTracer]()
	case "zipkin":
		spec, err = workflowspec.GetService[zipkin.ZipkinTracer]()
	default:
		return nil, fmt.Errorf("unsupported collector type: %s", collectorType)
	}

	if err != nil {
		return nil, err
	}

	node := &OtelSidecarClient{
		ClientName:    name,
		ServerDial:    addr,
		Coordinator:   coordinator,
		Spec:          spec,
		CollectorType: collectorType,
	}
	return node, nil
}

// Implements ir.IRNode
func (node *OtelSidecarClient) Name() string {
	return node.ClientName
}

// Implements ir.IRNode
func (node *OtelSidecarClient) String() string {
	return node.Name() + " = OtelSidecarClient(" + node.ServerDial.Name() + ")"
}

// Implements golang.Instantiable
func (node *OtelSidecarClient) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.ClientName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating OtelSidecarClient %v in %v/%v", node.ClientName, builder.Info().Package.PackageName, builder.Info().FileName))

	// Pass the coordinator and collector type along with the server dial config
	return builder.DeclareConstructor(node.ClientName, node.Spec.Constructor.AsConstructor(), []ir.IRNode{node.ServerDial, node.Coordinator})
}

// Implements service.ServiceNode
func (node *OtelSidecarClient) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesInterface
func (node *OtelSidecarClient) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.ProvidesModule
func (node *OtelSidecarClient) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

func (node *OtelSidecarClient) ImplementsGolangNode() {}

func (node *OtelSidecarClient) ImplementsOTSidecarClient() {}
