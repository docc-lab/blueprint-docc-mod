package otelcol

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/linux"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/otelcol"
)

// Blueprint IR node that represents the OpenTelemetry Collector container
type OTCollectorContainer struct {
	docker.Container
	docker.ProvidesContainerInstance

	CollectorName      string
	BindAddr           *address.BindConfig
	Iface              *goparser.ParsedInterface
	CentralBackendAddr *address.DialConfig
}

// OpenTelemetry Collector interface exposed to the application.
type OTCollectorInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (j *OTCollectorInterface) GetName() string {
	return "j(" + j.Wrapped.GetName() + ")"
}

func (j *OTCollectorInterface) GetMethods() []service.Method {
	return j.Wrapped.GetMethods()
}

func newOTCollectorContainer(name string, centralBackendAddr *address.DialConfig) (*OTCollectorContainer, error) {
	spec, err := workflowspec.GetService[otelcol.OTCollectorTracer]()
	if err != nil {
		return nil, err
	}

	collector := &OTCollectorContainer{
		CollectorName:      name,
		Iface:              spec.Iface,
		CentralBackendAddr: centralBackendAddr,
	}
	return collector, nil
}

// Implements ir.IRNode
func (node *OTCollectorContainer) Name() string {
	return node.CollectorName
}

// Implements ir.IRNode
func (node *OTCollectorContainer) String() string {
	return node.Name() + " = OTCollector(" + node.BindAddr.Name() + ")"
}

// Implements service.ServiceNode
func (node *OTCollectorContainer) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface := node.Iface.ServiceInterface(ctx)
	return &OTCollectorInterface{Wrapped: iface}, nil
}

// Implements docker.ProvidesContainerInstance
func (node *OTCollectorContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	node.BindAddr.Port = 4317

	// Declare the prebuilt instance with the backend address as an argument
	// The docker-compose system will resolve the address and pass it as an environment variable
	err := target.DeclarePrebuiltInstance(node.CollectorName, "otel/opentelemetry-collector", node.BindAddr, node.CentralBackendAddr)
	if err != nil {
		return err
	}

	// Get the backend address name for environment variable substitution
	backendEnvVarName := linux.EnvVar(node.CentralBackendAddr.Name())

	// Configure the OpenTelemetry collector using environment variables
	// This sets up a complete configuration with receivers, processors, and exporters
	configYaml := fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024

exporters:
  otlp:
    endpoint: "${%s}"
    tls:
      insecure: true
    protocol: grpc

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]`, backendEnvVarName)

	// Set the configuration as an environment variable
	err = target.SetEnvironmentVariable(node.CollectorName, "OTEL_CONFIG_YAML", configYaml)
	if err != nil {
		return err
	}

	return nil
}
