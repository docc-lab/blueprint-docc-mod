package tracecoordinator

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/tracecoordinator"
	"golang.org/x/exp/slog"
)

// The TraceCoordinator IR node represents a coordinator service that manages communication
// between different tracing systems (OpenTelemetry, Jaeger, Zipkin).
type TraceCoordinator struct {
	golang.Service

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.ProvidesInterface
	golang.Instantiable

	InstanceName string
	Spec         *workflowspec.Service
}

// Creates a new TraceCoordinator IR node
func newTraceCoordinator(name string) (*TraceCoordinator, error) {
	spec, err := workflowspec.GetService[tracecoordinator.Coordinator]()
	if err != nil {
		return nil, err
	}
	node := &TraceCoordinator{
		InstanceName: name,
		Spec:         spec,
	}

	return node, nil
}

// Implements ir.IRNode
func (node *TraceCoordinator) Name() string {
	return node.InstanceName
}

// Implements golang.Service
func (node *TraceCoordinator) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Spec.Iface.ServiceInterface(ctx), nil
}

// Implements golang.ProvidesModule
func (node *TraceCoordinator) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	return node.Spec.AddToWorkspace(builder)
}

// Implements golang.ProvidesInterface
func (node *TraceCoordinator) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Spec.AddToModule(builder)
}

// Implements golang.Instantiable
func (node *TraceCoordinator) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating TraceCoordinator %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, node.Spec.Constructor.AsConstructor(), nil)
}

// Implements ir.IRNode
func (node *TraceCoordinator) String() string {
	return fmt.Sprintf("%v = TraceCoordinator()", node.InstanceName)
}

func (node *TraceCoordinator) ImplementsGolangNode()    {}
func (node *TraceCoordinator) ImplementsGolangService() {}
