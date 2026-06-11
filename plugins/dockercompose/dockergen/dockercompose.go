package dockergen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/blueprint"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/linux"
	"golang.org/x/exp/slog"
)

// Compile-time env-var hooks for baking Go runtime tuning into every
// Blueprint-built service container's environment block. Unset → nothing
// is injected (preserves vanilla Blueprint behavior); set → the value is
// written verbatim into the compose `environment:` and propagates through
// kompose into the generated k8s deployment env entries.
//
// Common pairing for the docc-mod experiments:
//
//	BLUEPRINT_GC_INTERVAL_SEC=0.01 BLUEPRINT_GOGC=off BLUEPRINT_BRIDGE_KIND=sb \
//	    go run ./wiring -w docker_sb ...
//
// Override at deploy time with `kubectl set env deployment --all NAME=VAL`
// (but that triggers a rolling update — prefer the compile-time hook).
const (
	BlueprintGCIntervalEnv     = "BLUEPRINT_GC_INTERVAL_SEC"
	BlueprintGOGCEnv           = "BLUEPRINT_GOGC"
	BlueprintBridgeKindEnv     = "BLUEPRINT_BRIDGE_KIND"
	BlueprintSpanPaddingEnv    = "BLUEPRINT_SPAN_PADDING_BYTES"
	BlueprintOTLPRetryEnv      = "BLUEPRINT_OTLP_RETRY"
	BlueprintOTLPDeadlineMSEnv = "BLUEPRINT_OTLP_DEADLINE_MS"
)

/*
Used for generating the docker-compose file of a docker app
*/
type DockerComposeFile struct {
	WorkspaceName string
	WorkspaceDir  string
	FileName      string
	FilePath      string
	Instances     map[string]*instance           // Container instance declarations
	localServers  map[string]*address.BindConfig // Servers that have been defined within this docker-compose file
	localDials    map[string]*address.DialConfig // All servers that will be dialed from within this docker-compose file
}

type instance struct {
	InstanceName      string
	ContainerTemplate string              // only used if built; empty if not
	Image             string              // only used by prebuilt; empty if not
	Ports             map[string]uint16   // Map from bindconfig name to internal port
	Expose            map[uint16]struct{} // Ports exposed with expose directive
	Config            map[string]string   // Map from environment variable name to value
	Passthrough       map[string]struct{} // Environment variables that just get passed through to the container
	Command           []string            // Optional command override; emitted as compose `command:` (kompose translates to k8s `args:`). Used when env-var-based config isn't viable (e.g. elasticsearch settings with dots).
}

func NewDockerComposeFile(workspaceName, workspaceDir, fileName string) *DockerComposeFile {
	return &DockerComposeFile{
		WorkspaceName: workspaceName,
		WorkspaceDir:  workspaceDir,
		FileName:      fileName,
		FilePath:      filepath.Join(workspaceDir, fileName),
		Instances:     make(map[string]*instance),
		localServers:  make(map[string]*address.BindConfig),
		localDials:    make(map[string]*address.DialConfig),
	}
}

func (d *DockerComposeFile) Generate() error {
	slog.Info(fmt.Sprintf("Generating %v/%v", d.WorkspaceName, d.FileName))
	return ExecuteTemplateToFile("docker-compose", dockercomposeTemplate, d, d.FilePath)

}

// Adds an instance to the docker-compose file, that will use an off-the-shelf image.
//
// The instanceName is chosen by the user; it can subsequently be passed in methods such as [AddEnvVar],
// [PassthroughEnvVar], [ExposePort], [MapPort], and [MapPortToEnvVar].
func (d *DockerComposeFile) AddImageInstance(instanceName string, image string) error {
	return d.addInstance(instanceName, image, "")
}

// Adds an instance to the docker-compose file, that will be built from a container template
// on the local filesystem
//
// The instanceName is chosen by the user; it can subsequently be passed in methods such as [AddEnvVar],
// [PassthroughEnvVar], [ExposePort], [MapPort], and [MapPortToEnvVar].
func (d *DockerComposeFile) AddBuildInstance(instanceName string, containerTemplateName string) error {
	return d.addInstance(instanceName, "", containerTemplateName)
}

func (d *DockerComposeFile) getInstance(instanceName string) (*instance, error) {
	instanceName = ir.CleanName(instanceName)
	if i, exists := d.Instances[instanceName]; exists {
		return i, nil
	}
	return nil, blueprint.Errorf("container instance with name %v not found", instanceName)
}

// Sets an environment variable key to the specified val for instanceName
func (d *DockerComposeFile) AddEnvVar(instanceName string, key string, val string) error {
	instance, err := d.getInstance(instanceName)
	if err != nil {
		return err
	}
	key = linux.EnvVar(key)
	instance.Config[key] = val
	return nil
}

// Sets the container's command (entrypoint args) for instanceName. Emitted
// in the compose file as a `command: [a, b, c]` list. Useful when a setting
// can't be passed as an env var (e.g. ES requires `discovery.type=single-node`
// with a literal dot in the key, which Kubernetes env-var names disallow).
func (d *DockerComposeFile) SetCommand(instanceName string, args []string) error {
	instance, err := d.getInstance(instanceName)
	if err != nil {
		return err
	}
	instance.Command = args
	return nil
}

// Pass through the specified environment variable key from the calling environment
func (d *DockerComposeFile) PassthroughEnvVar(instanceName string, key string, optional bool) error {
	var passthroughValue string
	if optional {
		passthroughValue = fmt.Sprintf("${%v:-}", linux.EnvVar(key))
	} else {
		passthroughValue = fmt.Sprintf("${%v?%v must be set by the calling environment}", linux.EnvVar(key), key)
	}
	return d.AddEnvVar(instanceName, key, passthroughValue)
}

// Exposes a container-internal port for use by other containers within the docker-compose file
func (d *DockerComposeFile) ExposePort(instanceName string, internalPort uint16) error {
	instance, err := d.getInstance(instanceName)
	if err != nil {
		return err
	}
	instance.Expose[internalPort] = struct{}{}
	return nil
}

// Further to [ExposePort], adds a Port directive so that the host machine can access the internalPort
// of the container via the externalAddress.  Typically externalAddress will be a localhost or 0.0.0.0
// address
func (d *DockerComposeFile) MapPort(instanceName string, internalPort uint16, externalAddress string) error {
	instance, err := d.getInstance(instanceName)
	if err != nil {
		return err
	}
	instance.Ports[externalAddress] = internalPort
	return nil
}

// Further to [ExposePort], adds a Port directive so that the host machine can access the internalPort
// of the container, using a runtime substitution of envVarName as the externalAddress
func (d *DockerComposeFile) MapPortToEnvVar(instanceName string, internalPort uint16, envVarName string) error {
	externalAddress := fmt.Sprintf("${%v?%v must be set by the calling environment}", linux.EnvVar(envVarName), envVarName)
	return d.MapPort(instanceName, internalPort, externalAddress)
}

func (d *DockerComposeFile) addInstance(instanceName string, image string, containerTemplateName string) error {
	instanceName = ir.CleanName(instanceName)
	if _, exists := d.Instances[instanceName]; exists {
		return blueprint.Errorf("re-declaration of container instance %v of image %v", instanceName, image)
	}
	instance := instance{
		InstanceName:      instanceName,
		ContainerTemplate: containerTemplateName,
		Image:             image,
		Expose:            make(map[uint16]struct{}),
		Ports:             make(map[string]uint16),
		Config:            make(map[string]string),
		Passthrough:       make(map[string]struct{}),
	}

	// Only inject Go runtime tuning into BUILT services — prebuilt images
	// like mongo/redis/jaeger ignore these vars and don't benefit from them.
	//
	// EXCEPTION: otelcol containers skip the GOGC injection. Blueprint's
	// GOGC=off convention is meant for app pods, where Blueprint's own
	// goproc runtime calls runtime.GC() on a GC_INTERVAL_SEC ticker to
	// give deterministic GC timing. The otelcontribcol process does not
	// read GC_INTERVAL_SEC and has no equivalent ticker, so injecting
	// GOGC=off leaves the otelcol with no automatic GC at all — only
	// the manual force-GC that memory_limiter / priorityprocessor call
	// from their check loops. At 1s memory_limiter check interval that
	// becomes too coarse to keep RSS under tight cgroup caps (observed:
	// alloc spikes past hard threshold + 30% RSS overhead → OOMKill
	// under SB SDK throughput at 256 MiB).
	if containerTemplateName != "" {
		isOtelcol := strings.HasPrefix(instanceName, "otelcol")
		if v := os.Getenv(BlueprintGCIntervalEnv); v != "" {
			instance.Config["GC_INTERVAL_SEC"] = v
		}
		if v := os.Getenv(BlueprintGOGCEnv); v != "" && !isOtelcol {
			instance.Config["GOGC"] = v
		}
		if v := os.Getenv(BlueprintBridgeKindEnv); v != "" {
			instance.Config["BRIDGE_KIND"] = v
		}
		// SPAN_PADDING_BYTES is read by the vanilla/SB processors and
		// injects N bytes per span as a wire-going attribute. Only
		// useful on app containers (otelcol just forwards). Skip
		// otelcol to avoid confusion in pod env dumps.
		if v := os.Getenv(BlueprintSpanPaddingEnv); v != "" && !isOtelcol {
			instance.Config["SPAN_PADDING_BYTES"] = v
		}
		// OTLP_RETRY controls whether the SDK's gRPC client retries
		// on retriable errors (default: on; set to "off" to disable).
		// Only meaningful for app containers — the otelcol uses its
		// own retry config in the OTLP exporter.
		if v := os.Getenv(BlueprintOTLPRetryEnv); v != "" && !isOtelcol {
			instance.Config["OTLP_RETRY"] = v
		}
		// OTLP_DEADLINE_MS is the per-RPC context.WithTimeout deadline
		// the SDK applies to UploadTraces calls. Default 1000ms.
		if v := os.Getenv(BlueprintOTLPDeadlineMSEnv); v != "" && !isOtelcol {
			instance.Config["OTLP_DEADLINE_MS"] = v
		}
	}

	d.Instances[instanceName] = &instance
	return nil
}

var dockercomposeTemplate = `
version: '3'
services:
{{range $_, $decl := .Instances}}
  {{.InstanceName}}:
    {{if .Image -}}
    image: {{.Image}}
    {{- else if .ContainerTemplate -}}
    build:
      context: {{.ContainerTemplate}}
      dockerfile: ./Dockerfile
    {{- end}}
    hostname: {{.InstanceName}}
    {{- if .Ports}}
    expose:
    {{- range $internal, $_ := .Expose}}
     - "{{$internal}}"
    {{- end}}
    ports:
    {{- range $external, $internal := .Ports}}
     - "{{$external}}:{{$internal}}"
    {{- end}}
    {{- end}}
    {{- if .Config}}
    environment:
    {{- range $name, $value := .Config}}
     - {{$name}}={{$value}}
    {{- end}}
    {{- end}}
    {{- if .Command}}
    command:
    {{- range $_, $arg := .Command}}
     - "{{$arg}}"
    {{- end}}
    {{- end}}
    restart: always
{{end}}
`
