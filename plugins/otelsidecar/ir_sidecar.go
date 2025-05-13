package otelsidecar

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/zipkin"
)

// Blueprint IR node that represents the OpenTelemetry sidecar container
type OtelSidecarContainer struct {
	docker.Container
	docker.ProvidesContainerInstance

	SidecarName   string
	BindAddr      *address.BindConfig
	Iface         *goparser.ParsedInterface
	CollectorType string // "jaeger" or "zipkin"
}

// OpenTelemetry sidecar interface exposed to the application.
type OtelSidecarInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (j *OtelSidecarInterface) GetName() string {
	return "j(" + j.Wrapped.GetName() + ")"
}

func (j *OtelSidecarInterface) GetMethods() []service.Method {
	return j.Wrapped.GetMethods()
}

func newOtelSidecarContainer(name string, collectorType string) (*OtelSidecarContainer, error) {
	// spec, err := workflowspec.GetService[opentelemetry.OtelSidecarTracer]()
	// if err != nil {
	// 	return nil, err
	// }

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

	sidecar := &OtelSidecarContainer{
		SidecarName:   name,
		CollectorType: collectorType,
		Iface:         spec.Iface,
	}
	return sidecar, nil
}

// Implements ir.IRNode
func (node *OtelSidecarContainer) Name() string {
	return node.SidecarName
}

// Implements ir.IRNode
func (node *OtelSidecarContainer) String() string {
	return node.Name() + " = OtelSidecar(" + node.BindAddr.Name() + ")"
}

// Implements service.ServiceNode
func (node *OtelSidecarContainer) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface := node.Iface.ServiceInterface(ctx)
	return &OtelSidecarInterface{Wrapped: iface}, nil
}

// Implements docker.ProvidesContainerInstance
func (node *OtelSidecarContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	node.BindAddr.Port = 4318 // Default OTLP HTTP port
	return target.DeclarePrebuiltInstance(node.SidecarName, "otel/opentelemetry-collector", node.BindAddr)
}
