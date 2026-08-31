package specs

import (
	"os"

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
)

// Reverse-truss test topologies. OT-cgpb wrappers on every gRPC edge (so retCtx
// crosses the wire). Root is an HTTP frontend so we can drive it with curl.

var DockerCGPBTree4 = cmdbuilder.SpecOption{
	Name:        "docker_cgpb_tree4",
	Description: "4-node reverse-truss tree: root -> mid -> {leaf1,leaf2} (fan-in at mid).",
	Build: func(spec wiring.WiringSpec) ([]string, error) {
		os.Setenv("OT_BRIDGE", "cgpb")
		return makeTreeSpec(spec, "cgpb_tree4", 4)
	},
}

var DockerCGPBTree7 = cmdbuilder.SpecOption{
	Name:        "docker_cgpb_tree7",
	Description: "7-node reverse-truss tree: root->a->{b,c}; b->{leaf1,leaf2}; c->leaf3.",
	Build: func(spec wiring.WiringSpec) ([]string, error) {
		os.Setenv("OT_BRIDGE", "cgpb")
		return makeTreeSpec(spec, "cgpb_tree7", 7)
	},
}

func makeTreeSpec(spec wiring.WiringSpec, suffix string, nodes int) ([]string, error) {
	sn := func(name string) string { return name + "_" + suffix }

	jaeger_collector := jaeger.Collector(spec, sn("jaeger"))
	trace_collector := otelcol.Collector(spec, sn("otelcol"), jaeger_collector, 8080, "jaeger")

	grpcNode := func(name string) string {
		opentelemetry.Instrument(spec, name, trace_collector)
		grpc.Deploy(spec, name)
		goproc.Deploy(spec, name)
		return linuxcontainer.Deploy(spec, name)
	}
	httpNode := func(name string) string {
		opentelemetry.Instrument(spec, name, trace_collector)
		http.Deploy(spec, name)
		goproc.Deploy(spec, name)
		return linuxcontainer.Deploy(spec, name)
	}

	var ctrs []string
	if nodes == 4 {
		leaf1 := workflow.Service[*leaf.LeafNodeImpl](spec, sn("leaf1"))
		ctrs = append(ctrs, grpcNode(leaf1))
		leaf2 := workflow.Service[*leaf.LeafNodeImpl](spec, sn("leaf2"))
		ctrs = append(ctrs, grpcNode(leaf2))
		mid := workflow.Service[*leaf.BinaryNodeImpl](spec, sn("mid"), leaf1, leaf2) // fan-in
		ctrs = append(ctrs, grpcNode(mid))
		root := workflow.Service[*leaf.UnaryNodeImpl](spec, sn("root"), mid)
		ctrs = append(ctrs, httpNode(root))
	} else {
		leaf1 := workflow.Service[*leaf.LeafNodeImpl](spec, sn("leaf1")) // D
		ctrs = append(ctrs, grpcNode(leaf1))
		leaf2 := workflow.Service[*leaf.LeafNodeImpl](spec, sn("leaf2")) // E
		ctrs = append(ctrs, grpcNode(leaf2))
		leaf3 := workflow.Service[*leaf.LeafNodeImpl](spec, sn("leaf3")) // F
		ctrs = append(ctrs, grpcNode(leaf3))
		b := workflow.Service[*leaf.BinaryNodeImpl](spec, sn("b"), leaf1, leaf2) // fan-in
		ctrs = append(ctrs, grpcNode(b))
		c := workflow.Service[*leaf.UnaryNodeImpl](spec, sn("c"), leaf3)
		ctrs = append(ctrs, grpcNode(c))
		a := workflow.Service[*leaf.BinaryNodeImpl](spec, sn("a"), b, c) // fan-in
		ctrs = append(ctrs, grpcNode(a))
		root := workflow.Service[*leaf.UnaryNodeImpl](spec, sn("root"), a)
		ctrs = append(ctrs, httpNode(root))
	}
	ctrs = append(ctrs, sn("otelcol"), sn("jaeger"))
	return ctrs, nil
}