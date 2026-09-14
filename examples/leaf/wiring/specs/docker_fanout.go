package specs

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/leaf/workflow/leaf"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
	"gopkg.in/yaml.v3"
)

var fanoutTopologyFile = flag.String("topology", "", "JSON or YAML topology and call patterns for docker_fanout (default: embedded fanout.json)")
var fanoutCollectorImage = flag.String("collector-image", "10.10.1.1:30000/otelcontribcol:latest", "Collector image with configdiscovery support for docker_fanout")
var fanoutCollectorConfig = flag.String("collector-config", defaultFanoutCollectorConfig(), "Collector YAML for docker_fanout")

func defaultFanoutCollectorConfig() string {
	_, source, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(source), "fanout-collector.yaml")
}

//go:embed fanout.json
var defaultFanoutTopology []byte

// Tomislav-RetCtx: FanoutTopology compiles JSON/YAML into a rooted DAG. Shared downstream services and
// repeated child entries are allowed; every child entry generates one call.
type FanoutTopology struct {
	Schema     string                    `json:"$schema,omitempty" yaml:"$schema,omitempty"`
	Version    *int                      `json:"version,omitempty" yaml:"version,omitempty"`
	Root       string                    `json:"root" yaml:"root"`
	RPCTimeout string                    `json:"rpc_timeout,omitempty" yaml:"rpc_timeout,omitempty"`
	Nodes      map[string]FanoutNodeSpec `json:"nodes" yaml:"nodes"`
}

type FanoutNodeSpec struct {
	Children    []string          `json:"children,omitempty" yaml:"children,omitempty"`
	Parallelism *int              `json:"parallelism,omitempty" yaml:"parallelism,omitempty"`
	Pattern     *leaf.CallPattern `json:"pattern,omitempty" yaml:"pattern,omitempty"`
}

var fanoutNodeName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func parseFanoutTopology(data []byte) (FanoutTopology, error) {
	var topology FanoutTopology
	var decoder interface{ Decode(any) error }
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		jsonDecoder := json.NewDecoder(bytes.NewReader(data))
		jsonDecoder.DisallowUnknownFields()
		decoder = jsonDecoder
	} else {
		yamlDecoder := yaml.NewDecoder(bytes.NewReader(data))
		yamlDecoder.KnownFields(true)
		decoder = yamlDecoder
	}
	if err := decoder.Decode(&topology); err != nil {
		return topology, fmt.Errorf("decode fanout topology: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return topology, fmt.Errorf("fanout topology must contain one JSON object or YAML document")
	}
	return topology, nil
}

func (node FanoutNodeSpec) dependencies() ([]string, error) {
	if node.Pattern != nil {
		if node.Children != nil || node.Parallelism != nil {
			return nil, fmt.Errorf("pattern cannot be combined with children or node-level parallelism")
		}
		return node.Pattern.DependencyNames()
	}
	if node.Parallelism != nil && *node.Parallelism < 0 {
		return nil, fmt.Errorf("negative parallelism")
	}
	return node.Children, nil
}

// orderedNodes validates before any wiring is changed, then returns a stable
// children-first order so each node can depend on already-deployed children.
func (t FanoutTopology) orderedNodes() ([]string, error) {
	if t.Version != nil && *t.Version != 1 {
		return nil, fmt.Errorf("unsupported topology version %d; expected 1", *t.Version)
	}
	if t.RPCTimeout != "" {
		if duration, err := time.ParseDuration(t.RPCTimeout); err != nil || duration <= 0 {
			return nil, fmt.Errorf("rpc_timeout must be a positive duration")
		}
	}
	if _, ok := t.Nodes[t.Root]; !ok {
		return nil, fmt.Errorf("fanout root %q is not defined", t.Root)
	}
	names := make([]string, 0, len(t.Nodes))
	for name := range t.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	dependencies := make(map[string][]string, len(names))
	for _, name := range names {
		node := t.Nodes[name]
		if !fanoutNodeName.MatchString(name) {
			return nil, fmt.Errorf("invalid node name %q: use lowercase letters, digits and underscores, starting with a letter", name)
		}
		children, err := node.dependencies()
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", name, err)
		}
		dependencies[name] = children
		for _, child := range children {
			if _, ok := t.Nodes[child]; !ok {
				return nil, fmt.Errorf("node %q references undefined child %q", name, child)
			}
		}
	}
	state := make(map[string]int)
	var order []string
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("fanout topology contains a cycle through %q", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, child := range dependencies[name] {
			if err := visit(child); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}
	if err := visit(t.Root); err != nil {
		return nil, err
	}
	for _, name := range names {
		if state[name] == 0 {
			return nil, fmt.Errorf("node %q is unreachable from root %q", name, t.Root)
		}
	}
	return order, nil
}

var DockerFanout = cmdbuilder.SpecOption{
	Name:        "docker_fanout",
	Description: "Synthetic DAG with composed call patterns; use -topology JSON/YAML and OT_BRIDGE=pb|cgpb|sb|v.",
	Build: func(spec wiring.WiringSpec) ([]string, error) {
		data := defaultFanoutTopology
		if *fanoutTopologyFile != "" {
			var err error
			data, err = os.ReadFile(*fanoutTopologyFile)
			if err != nil {
				return nil, err
			}
		}
		topology, err := parseFanoutTopology(data)
		if err != nil {
			return nil, err
		}
		return makeFanoutSpec(spec, topology, os.Getenv("OT_BRIDGE"))
	},
}

func makeFanoutSpec(spec wiring.WiringSpec, topology FanoutTopology, kind string) ([]string, error) {
	order, err := topology.orderedNodes()
	if err != nil {
		return nil, err
	}
	switch kind {
	case "":
		kind = "cgpb"
	case "pb", "cgpb", "sb", "v":
	default:
		return nil, fmt.Errorf("unsupported OT_BRIDGE %q; use pb, cgpb, sb or v", kind)
	}
	if _, err := os.ReadFile(*fanoutCollectorConfig); err != nil {
		return nil, fmt.Errorf("read fanout collector config (override with -collector-config): %w", err)
	}
	// Keep generated wrappers and the runtime SDK's processor in agreement.
	os.Setenv("OT_BRIDGE", kind)
	os.Setenv("BLUEPRINT_BRIDGE_KIND", kind)
	suffix := "fanout_" + kind
	serviceName := func(name string) string { return "node_" + name + "_" + suffix }
	jaegerName, collectorName := "jaeger_"+suffix, "otelcol_"+suffix
	jaegerCollector := jaeger.Collector(spec, jaegerName)
	collector := otelcol.CollectorWithConfig(spec, collectorName, jaegerCollector,
		*fanoutCollectorConfig, *fanoutCollectorImage, 8080, "jaeger")
	rpcTimeout := topology.RPCTimeout
	if rpcTimeout == "" {
		rpcTimeout = "1s"
	}
	var containers []string
	for _, name := range order {
		node := topology.Nodes[name]
		children, err := node.dependencies()
		if err != nil {
			return nil, err
		}
		limit := 0
		if node.Parallelism != nil {
			limit = *node.Parallelism
		}
		config := strconv.Itoa(limit)
		if node.Pattern != nil {
			encoded, err := json.Marshal(leaf.PatternConfig{Dependencies: children, Pattern: *node.Pattern})
			if err != nil {
				return nil, err
			}
			config = string(encoded)
		}
		args := []string{config}
		for _, child := range children {
			args = append(args, serviceName(child))
		}
		var service string
		if node.Pattern != nil {
			service = workflow.Service[*leaf.PatternNodeImpl](spec, serviceName(name), args...)
		} else {
			service = workflow.Service[*leaf.FanoutNodeImpl](spec, serviceName(name), args...)
		}
		opentelemetry.Instrument(spec, service, collector)
		if name == topology.Root {
			http.Deploy(spec, service)
		} else {
			grpc.DeployWithTimeout(spec, service, rpcTimeout)
		}
		goproc.Deploy(spec, service)
		containers = append(containers, linuxcontainer.Deploy(spec, service))
	}
	return append(containers, collectorName, jaegerName), nil
}
