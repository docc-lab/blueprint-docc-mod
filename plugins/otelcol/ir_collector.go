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
	"gopkg.in/yaml.v3"
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
	// New fields for dynamic configuration
	BaseImage        string // Docker base image to use
	CustomConfigPath string // Path to custom YAML config file
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
		// Set default values for new fields
		BaseImage: "otel/opentelemetry-collector-contrib",
	}

	// Set exporter type if provided, otherwise defaults to "otlp"
	if len(exporterType) > 0 {
		collector.ExporterType = exporterType[0]
	}

	return collector, nil
}

// newOTCollectorContainerWithConfig creates a new OTCollectorContainer with custom configuration
func newOTCollectorContainerWithConfig(name string, centralBackendAddr *address.DialConfig, customConfigPath string, baseImage string, exporterType ...string) (*OTCollectorContainer, error) {
	collector, err := newOTCollectorContainer(name, centralBackendAddr, exporterType...)
	if err != nil {
		return nil, err
	}

	collector.CustomConfigPath = customConfigPath
	if baseImage != "" {
		collector.BaseImage = baseImage
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
	var configContent string
	if node.CustomConfigPath != "" {
		// Read and process the custom config file
		content, err := os.ReadFile(node.CustomConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read custom config file %s: %w", node.CustomConfigPath, err)
		}
		configContent = node.processCustomConfig(string(content))
	} else {
		// Generate the configuration file using Blueprint's template system
		configContent = node.generateConfig()
	}

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
	if exporterType == "jaeger" {
		exporterType = "otlp"
	}
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
		exporterConfig = fmt.Sprintf(`  otlp:
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

// processCustomConfig processes a custom configuration file by merging it with dynamic exporter configuration
func (node *OTCollectorContainer) processCustomConfig(customConfig string) string {
	// Parse the custom YAML config
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(customConfig), &config); err != nil {
		// If parsing fails, fall back to the original config
		return customConfig
	}

	// Determine exporter type
	exporterType := node.ExporterType
	if exporterType == "jaeger" {
		exporterType = "otlp"
	}
	if exporterType == "" {
		exporterType = "otlp"
	}

	// Get the backend address name for environment variable substitution
	backendEnvVarName := linux.EnvVar(node.CentralBackendAddr.Name())

	// Always replace receivers with the standard OTLP receiver
	config["receivers"] = map[string]interface{}{
		"otlp": map[string]interface{}{
			"protocols": map[string]interface{}{
				"grpc": map[string]interface{}{
					"endpoint": "0.0.0.0:4317",
				},
			},
		},
	}

	// Get or create exporters section
	exporters, exists := config["exporters"]
	if !exists {
		exporters = make(map[string]interface{})
		config["exporters"] = exporters
	}
	exportersMap := exporters.(map[string]interface{})

	// Add the dynamic exporter based on type
	switch exporterType {
	case "zipkin":
		exportersMap["zipkin"] = map[string]interface{}{
			"endpoint": fmt.Sprintf("http://${%s}/api/v2/spans", backendEnvVarName),
		}
	case "otlp":
		exportersMap["otlp"] = map[string]interface{}{
			"endpoint": fmt.Sprintf("${%s}", backendEnvVarName),
			"tls": map[string]interface{}{
				"insecure": true,
			},
		}
	}

	// Update service pipeline to include the dynamic exporter
	if service, exists := config["service"]; exists {
		if serviceMap, ok := service.(map[string]interface{}); ok {
			if pipelines, exists := serviceMap["pipelines"]; exists {
				if pipelinesMap, ok := pipelines.(map[string]interface{}); ok {
					if traces, exists := pipelinesMap["traces"]; exists {
						if tracesMap, ok := traces.(map[string]interface{}); ok {
							// Update receivers to use otlp
							tracesMap["receivers"] = []string{"otlp"}

							// Update exporters to include the dynamic exporter
							if exporters, exists := tracesMap["exporters"]; exists {
								if exportersList, ok := exporters.([]interface{}); ok {
									// Add the dynamic exporter if not already present
									hasDynamicExporter := false
									for _, exp := range exportersList {
										if exp == exporterType {
											hasDynamicExporter = true
											break
										}
									}
									if !hasDynamicExporter {
										exportersList = append(exportersList, exporterType)
										tracesMap["exporters"] = exportersList
									}
								}
							} else {
								// Create exporters list if it doesn't exist
								tracesMap["exporters"] = []string{exporterType}
							}
						}
					}
				}
			}
		}
	}

	// Marshal back to YAML
	out, err := yaml.Marshal(config)
	if err != nil {
		// If marshaling fails, fall back to the original config
		return customConfig
	}

	return string(out)
}

// generateDockerfile creates a Dockerfile for the OpenTelemetry collector
func (node *OTCollectorContainer) generateDockerfile() string {
	baseImage := node.BaseImage
	if baseImage == "" {
		baseImage = "otel/opentelemetry-collector-contrib" // fallback default
	}

	return fmt.Sprintf(`FROM %s

# Copy the configuration file
COPY config.yaml /etc/otelcol/config.yaml

# Set the command to use the configuration file
CMD ["--config", "/etc/otelcol/config.yaml"]
`, baseImage)
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
