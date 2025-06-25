package otelcol

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/linux"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/otelcol"
	"golang.org/x/exp/slog"
)

// Blueprint IR node that represents the OpenTelemetry Collector container
type OTCollectorContainer struct {
	docker.Container
	docker.ProvidesContainerImage
	docker.ProvidesContainerInstance

	CollectorName      string
	BindAddr           *address.BindConfig
	Iface              *goparser.ParsedInterface
	CentralBackendAddr *address.DialConfig
	ExporterType       string // "otlp", "zipkin", "jaeger", etc. Defaults to "otlp"
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

func newOTCollectorContainer(name string, centralBackendAddr *address.DialConfig, exporterType ...string) (*OTCollectorContainer, error) {
	spec, err := workflowspec.GetService[otelcol.OTCollectorTracer]()
	if err != nil {
		return nil, err
	}

	collector := &OTCollectorContainer{
		CollectorName:      name,
		Iface:              spec.Iface,
		CentralBackendAddr: centralBackendAddr,
	}

	// Set exporter type if provided, otherwise defaults to "otlp"
	if len(exporterType) > 0 {
		collector.ExporterType = exporterType[0]
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

// Implements docker.ProvidesContainerImage
func (node *OTCollectorContainer) AddContainerArtifacts(target docker.ContainerWorkspace) error {
	// The image only needs to be created in the output directory once
	if target.Visited(node.CollectorName + ".artifacts") {
		return nil
	}

	// Create a new subdirectory to construct the image
	// Use CleanName to ensure the directory name matches what docker-compose expects
	cleanName := ir.CleanName(node.CollectorName)
	slog.Info(fmt.Sprintf("Creating container image %v", cleanName))
	dir, err := target.CreateImageDir(cleanName)
	if err != nil {
		return err
	}

	// Create a workspace for generating our artifacts
	workspace := NewOTCollectorWorkspace(cleanName, dir)
	if err := node.generateArtifacts(workspace); err != nil {
		return err
	}
	return nil
}

// generateArtifacts creates the OpenTelemetry collector artifacts
func (node *OTCollectorContainer) generateArtifacts(workspace *otCollectorWorkspace) error {
	// Generate the configuration file using Blueprint's template system
	configContent := node.generateConfig()
	if err := workspace.WriteConfigFile(configContent); err != nil {
		return err
	}

	// Generate the Dockerfile using Blueprint's template system
	dockerfileContent := node.generateDockerfile()
	if err := workspace.WriteDockerfile(dockerfileContent); err != nil {
		return err
	}

	return workspace.Finish()
}

// generateConfig generates the OpenTelemetry collector configuration
func (node *OTCollectorContainer) generateConfig() string {
	exporterType := node.ExporterType
	if exporterType == "" {
		exporterType = "otlp" // Default to otlp
	}

	// Get the backend address name for environment variable substitution
	backendEnvVarName := linux.EnvVar(node.CentralBackendAddr.Name())

	var exporterConfig string
	switch exporterType {
	case "zipkin":
		exporterConfig = fmt.Sprintf(`  zipkin:
    endpoint: "http://${%s}/api/v2/spans"`, backendEnvVarName)
	case "jaeger":
		exporterConfig = fmt.Sprintf(`  jaeger:
    endpoint: "${%s}"`, backendEnvVarName)
	case "otlp":
		fallthrough
	default:
		exporterConfig = fmt.Sprintf(`  otlp:
    endpoint: "${%s}"
    tls:
      insecure: true`, backendEnvVarName)
	}

	return fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024

exporters:
%s

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [%s]
`, exporterConfig, exporterType)
}

// generateDockerfile creates a Dockerfile for the OpenTelemetry collector
func (node *OTCollectorContainer) generateDockerfile() string {
	return `FROM otel/opentelemetry-collector

# Copy the configuration file
COPY config.yaml /etc/otelcol/config.yaml

# Set the command to use the configuration file
CMD ["--config", "/etc/otelcol/config.yaml"]
`
}

// Implements docker.ProvidesContainerInstance
func (node *OTCollectorContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	node.BindAddr.Port = 4317

	// Declare the local image instance (built from our Dockerfile)
	// Use CleanName for the container template name to match the directory name
	cleanName := ir.CleanName(node.CollectorName)
	err := target.DeclareLocalImage(node.CollectorName, cleanName, node.BindAddr, node.CentralBackendAddr)
	if err != nil {
		return err
	}

	return nil
}

// otCollectorWorkspace provides a Blueprint-specific workspace for generating otelcol artifacts
type otCollectorWorkspace struct {
	ir.VisitTrackerImpl
	info       docker.ContainerWorkspaceInfo
	configFile *configFile
	dockerfile *dockerfile
}

// NewOTCollectorWorkspace creates a new workspace for otelcol artifacts
func NewOTCollectorWorkspace(name string, dir string) *otCollectorWorkspace {
	return &otCollectorWorkspace{
		info: docker.ContainerWorkspaceInfo{
			Path:   dir,
			Target: "otelcol",
		},
		configFile: newConfigFile(name, dir),
		dockerfile: newDockerfile(name, dir),
	}
}

// WriteConfigFile writes the configuration file using Blueprint's abstractions
func (ws *otCollectorWorkspace) WriteConfigFile(content string) error {
	return ws.configFile.Write(content)
}

// WriteDockerfile writes the Dockerfile using Blueprint's abstractions
func (ws *otCollectorWorkspace) WriteDockerfile(content string) error {
	return ws.dockerfile.Write(content)
}

// Finish completes the workspace generation
func (ws *otCollectorWorkspace) Finish() error {
	return nil
}

// configFile represents a configuration file using Blueprint abstractions
type configFile struct {
	name string
	dir  string
}

func newConfigFile(name, dir string) *configFile {
	return &configFile{name: name, dir: dir}
}

func (cf *configFile) Write(content string) error {
	// Use Blueprint's pattern for writing files
	configPath := filepath.Join(cf.dir, "config.yaml")
	slog.Info(fmt.Sprintf("Writing OpenTelemetry collector config to %s", configPath))

	f, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	return err
}

// dockerfile represents a Dockerfile using Blueprint abstractions
type dockerfile struct {
	name string
	dir  string
}

func newDockerfile(name, dir string) *dockerfile {
	return &dockerfile{name: name, dir: dir}
}

func (df *dockerfile) Write(content string) error {
	// Use Blueprint's pattern for writing files
	dockerfilePath := filepath.Join(df.dir, "Dockerfile")
	slog.Info(fmt.Sprintf("Writing Dockerfile to %s", dockerfilePath))

	f, err := os.OpenFile(dockerfilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	return err
}
