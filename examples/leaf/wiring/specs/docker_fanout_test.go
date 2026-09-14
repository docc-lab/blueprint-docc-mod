package specs

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/leaf/workflow/leaf"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
)

func TestFanoutTopologySharedChildren(t *testing.T) {
	topology, err := parseFanoutTopology(defaultFanoutTopology)
	if err != nil {
		t.Fatal(err)
	}
	order, err := topology.orderedNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != len(topology.Nodes) || order[len(order)-1] != topology.Root {
		t.Fatalf("invalid node order: %v", order)
	}
	seen := make(map[string]bool)
	for _, name := range order {
		if seen[name] {
			t.Fatalf("shared service %q deployed more than once", name)
		}
		for _, child := range topology.Nodes[name].Children {
			if !seen[child] {
				t.Fatalf("%q precedes its dependency %q", name, child)
			}
		}
		seen[name] = true
	}
}

func TestPatternTopologyJSONAndYAMLEquivalence(t *testing.T) {
	data, err := os.ReadFile("patterns.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML, err := parseFanoutTopology(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := parseFanoutTopology(encoded)
	if err != nil || !reflect.DeepEqual(fromYAML, fromJSON) {
		t.Fatalf("JSON and YAML disagree: %v", err)
	}
	order, err := fromYAML.orderedNodes()
	if err != nil || len(order) != 6 || order[len(order)-1] != "frontend" {
		t.Fatalf("pattern dependencies did not form the expected graph: %v, %v", order, err)
	}
	children, err := fromYAML.Nodes["frontend"].dependencies()
	if err != nil || !reflect.DeepEqual(children, []string{"auth", "catalog", "inventory", "audit"}) {
		t.Fatalf("pattern targets = %v, %v", children, err)
	}
}

func TestPatternTopologyValidation(t *testing.T) {
	for _, tc := range []struct{ data, message string }{
		{`{"version":2,"root":"a","nodes":{"a":{}}}`, "version"},
		{`{"rpc_timeout":"0s","root":"a","nodes":{"a":{}}}`, "rpc_timeout"},
		{`{"root":"a","nodes":{"a":{"pattern":{"parallel":[{"call":"missing"}]}}}}`, "undefined child"},
		{`{"root":"a","nodes":{"a":{"pattern":{"call":"b"}},"b":{"pattern":{"repeat":{"count":2,"do":{"call":"a"}}}}}}`, "cycle"},
		{`{"root":"a","nodes":{"a":{"pattern":{"sleep":"1s"},"children":[]}}}`, "cannot be combined"},
		{`{"root":"a","nodes":{"a":{"pattern":{"sleep":"1s"},"parallelism":0}}}`, "cannot be combined"},
		{`{"root":"a","nodes":{"a":{"pattern":{"sequence":[{"sleep":"1s","call":"b"}]}}}}`, "exactly one"},
		{"root: a\nnodes:\n  a:\n    pattern:\n      slep: 1s\n", "slep"},
		{"root: a\nnodes:\n  a: {}\n  a: {}\n", "already defined"},
		{"root: a\nnodes: {a: {}}\n---\nroot: b\nnodes: {b: {}}\n", "one JSON object or YAML document"},
	} {
		topology, err := parseFanoutTopology([]byte(tc.data))
		if err == nil {
			_, err = topology.orderedNodes()
		}
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("%s: got %v, want %q", tc.data, err, tc.message)
		}
	}
}

func TestPatternWorkflowVariadicDependencies(t *testing.T) {
	workflowspec.AddModule("github.com/blueprint-uservices/blueprint/examples/leaf/workflow")
	spec := wiring.NewWiringSpec("pattern")
	child := workflow.Service[*leaf.FanoutNodeImpl](spec, "child", "0")
	config := `{"dependencies":["child"],"pattern":{"repeat":{"count":3,"do":{"call":"child"}}}}`
	root := workflow.Service[*leaf.PatternNodeImpl](spec, "root", config, child)
	if _, err := spec.BuildIR(root); err != nil {
		t.Fatalf("cannot wire pattern service: %v", err)
	}
}

func TestFanoutTopologyInvalid(t *testing.T) {
	for _, tc := range []struct{ name, data, message string }{
		{"missing_root", `{"root":"root","nodes":{}}`, "not defined"},
		{"missing_child", `{"root":"root","nodes":{"root":{"children":["missing"]}}}`, "undefined child"},
		{"cycle", `{"root":"root","nodes":{"root":{"children":["a"]},"a":{"children":["root"]}}}`, "cycle"},
		{"unreachable", `{"root":"root","nodes":{"root":{},"orphan":{}}}`, "unreachable"},
		{"negative_limit", `{"root":"root","nodes":{"root":{"parallelism":-1}}}`, "negative"},
		{"invalid_name", `{"root":"../root","nodes":{"../root":{}}}`, "invalid node name"},
		{"typo", `{"root":"root","nodes":{"root":{"paralelism":2}}}`, "unknown field"},
		{"trailing_json", `{"root":"root","nodes":{"root":{}}} {}`, "one JSON object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			topology, err := parseFanoutTopology([]byte(tc.data))
			if err == nil {
				_, err = topology.orderedNodes()
			}
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v, want %q", err, tc.message)
			}
		})
	}
}

func TestFanoutWorkflowVariadicDependencies(t *testing.T) {
	workflowspec.AddModule("github.com/blueprint-uservices/blueprint/examples/leaf/workflow")
	for _, count := range []int{0, 1, 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			spec := wiring.NewWiringSpec("fanout")
			args := []string{"0"}
			for i := 0; i < count; i++ {
				child := workflow.Service[*leaf.FanoutNodeImpl](spec, fmt.Sprintf("child%d", i), "1")
				args = append(args, child)
			}
			root := workflow.Service[*leaf.FanoutNodeImpl](spec, "root", args...)
			if _, err := spec.BuildIR(root); err != nil {
				t.Fatalf("cannot wire %d children: %v", count, err)
			}
		})
	}
	spec := wiring.NewWiringSpec("missing_parallelism")
	root := workflow.Service[*leaf.FanoutNodeImpl](spec, "root")
	if _, err := spec.BuildIR(root); err == nil {
		t.Fatal("variadic constructor accepted a missing required argument")
	}
}
