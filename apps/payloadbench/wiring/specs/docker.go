// Package specs defines the wiring specs for the PayloadBench application.
//
// PayloadBench is a deliberately minimal two-service chain for measuring the
// effect of inter-service payload size — forward path (edge→internal) and
// return path (internal→edge) independently — on throughput and response
// time. The deployment stack (retries, clientpool, opentelemetry wrappers,
// grpc/http, goproc, linuxcontainer, otelcol→jaeger[→ES]) intentionally
// mirrors examples/dsb_sn so measurements transfer between the two apps.
//
// Variant naming follows dsb_sn: the spec suffix tags every generated
// identifier, `_es` selects an Elasticsearch-backed jaeger, and the bridge
// kind pairs a compile-time wrapper template (OT_BRIDGE env at codegen time)
// with a runtime span processor (BRIDGE_KIND env on the deployed pods).
package specs

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/blueprint-uservices/blueprint/apps/payloadbench/workflow/payloadbench"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/clientpool"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/retries"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
)

// Same collector image and configs as examples/dsb_sn — one hand-edit site.
const (
	otelImage     = "10.10.1.1:30000/otelcontribcol:latest"
	bridgesConfig = "/users/tomislav/opentelemetry-collector-contrib/test-config-bridges.yaml"
	vanillaConfig = "/users/tomislav/opentelemetry-collector-contrib/config-vanilla.yaml"
)

// Go runtime tuning baked into every service container, matching dsb_sn.
// Override by setting the env var before `go run` (empty string disables).
const (
	DefaultGCIntervalSec = "0.1"
	DefaultGOGC          = "off"
)

func init() {
	if _, set := os.LookupEnv("BLUEPRINT_GC_INTERVAL_SEC"); !set {
		os.Setenv("BLUEPRINT_GC_INTERVAL_SEC", DefaultGCIntervalSec)
	}
	if _, set := os.LookupEnv("BLUEPRINT_GOGC"); !set {
		os.Setenv("BLUEPRINT_GOGC", DefaultGOGC)
	}
}

// extraSuffix optionally extends the identifier suffix (e.g. `-extra 2` on
// docker_v_es yields identifiers `..._v_es2`) so multiple instances of one
// variant can coexist. BRIDGE_KIND stays the bare bridge kind.
var extraSuffix = flag.String("extra", "",
	"Optional suffix extension appended after the variant kind on every generated identifier.")

// internalPool sets the size of the edge→internal client pool. 0 (default) means
// NO pool: Blueprint's default single shared gRPC client, i.e. ONE HTTP/2
// ClientConn for all calls, whose transport writer goroutine serializes framing
// work — the suspected cause of the ~60 MB/s ceiling.
//
// With N>0 the clientpool plugin builds N independent clients, each in its own
// derived namespace, so each performs its own grpc.Dial => N TCP connections.
// NOTE N is ALSO a hard concurrency cap (Pop blocks when all N are checked out),
// so size it above the needed concurrency (rps × latency); ~120 at 60k rps/2 ms.
// Baked in at codegen time, so changing it requires a rebuild.
var internalPool = flag.Int("internal-pool", 0,
	"Size of the edge→internal client pool (0 = no pool, single shared gRPC ClientConn). "+
		"Also caps concurrent outstanding calls at N.")

// numRetries controls the retries modifier. Default 3 matches dsb_sn. Set 0 to
// disable: under saturation, queueing latency exceeding the generated client's
// 1 s timeout causes retries that multiply offered work while still reporting
// 2xx — a candidate cause of the soft ~57-58k ceiling.
var numRetries = flag.Int64("retries", 3,
	"Retries per call (0 disables the retries modifier entirely).")

// Registered build targets — pick one with `-w docker_<suffix>`.
//
//	docker_v / docker_v_es       — vanilla OTel (no bridge), vanilla collector config.
//	docker_pb / docker_pb_es     — path bridge.
//	docker_cgpb / docker_cgpb_es — call-graph path bridge.
//	docker_sb / docker_sb_es     — structural bridge.
//	docker_nt_es                 — NO app-side tracing (SDK omitted entirely);
//	                               collector pods deployed but idle, as in dsb_sn.
//
// Remember: compile with OT_BRIDGE=<kind> so the codegen wrapper template
// matches (build_deploy_dsb.sh does this for dsb_sn; do the equivalent here).
var (
	DockerV      = makeVariant("v", vanillaConfig, false, true)
	DockerPB     = makeVariant("pb", bridgesConfig, false, true)
	DockerCGPB   = makeVariant("cgpb", bridgesConfig, false, true)
	DockerSB     = makeVariant("sb", bridgesConfig, false, true)
	DockerVES    = makeVariant("v_es", vanillaConfig, true, true)
	DockerPBES   = makeVariant("pb_es", bridgesConfig, true, true)
	DockerCGPBES = makeVariant("cgpb_es", bridgesConfig, true, true)
	DockerSBES   = makeVariant("sb_es", bridgesConfig, true, true)
	DockerNTES   = makeVariant("nt_es", vanillaConfig, true, false)
)

func makeVariant(kind, configPath string, useES, instrument bool) cmdbuilder.SpecOption {
	return cmdbuilder.SpecOption{
		Name:        "docker_" + kind,
		Description: fmt.Sprintf("PayloadBench edge→internal, variant %q, collector config %s", kind, configPath),
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			if instrument {
				bridgeKind := strings.TrimSuffix(kind, "_es")
				if _, set := os.LookupEnv("BLUEPRINT_BRIDGE_KIND"); !set {
					os.Setenv("BLUEPRINT_BRIDGE_KIND", bridgeKind)
				}
			}
			return makeDockerSpec(spec, kind+*extraSuffix, configPath, useES, instrument)
		},
	}
}

// A wiring spec deploying the two services into their own containers: the
// EdgeService is the HTTP frontend (driven by workload/payload.lua via wrk),
// the InternalService is a gRPC backend. Per-service defaults (retries=3,
// clientpool=100 on the HTTP frontend, OTel instrumentation) match dsb_sn.
func makeDockerSpec(spec wiring.WiringSpec, suffix, configPath string, useES, instrument bool) ([]string, error) {
	sn := func(name string) string { return name + "_" + suffix }

	var containers []string

	var jaeger_collector string
	if useES {
		jaeger_collector = jaeger.CollectorWithElasticsearch(spec, sn("jaeger"), sn("elasticsearch"))
	} else {
		jaeger_collector = jaeger.Collector(spec, sn("jaeger"))
	}
	trace_collector := otelcol.CollectorWithConfig(
		spec, sn("otelcol"),
		jaeger_collector,
		configPath,
		otelImage,
		8080, "jaeger")

	applyGRPCDefaults := func(serviceName string) string {
		if *numRetries > 0 {
			retries.AddRetries(spec, serviceName, *numRetries)
		}
		// Optional caller-side pool: N independent clients => N grpc.Dial =>
		// N TCP/HTTP2 connections, instead of one shared ClientConn. Must be
		// applied before grpc.Deploy (it is an application-level modifier).
		if *internalPool > 0 {
			clientpool.Create(spec, serviceName, *internalPool)
		}
		if instrument {
			opentelemetry.Instrument(spec, serviceName, trace_collector)
		}
		grpc.Deploy(spec, serviceName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	applyHTTPDefaults := func(serviceName string) string {
		if *numRetries > 0 {
			retries.AddRetries(spec, serviceName, *numRetries)
		}
		clientpool.Create(spec, serviceName, 100)
		if instrument {
			opentelemetry.Instrument(spec, serviceName, trace_collector)
		}
		http.Deploy(spec, serviceName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	internal_service := workflow.Service[payloadbench.InternalService](spec, sn("internal_service"))
	containers = append(containers, applyGRPCDefaults(internal_service))

	edge_service := workflow.Service[payloadbench.EdgeService](spec, sn("edge_service"), internal_service)
	containers = append(containers, applyHTTPDefaults(edge_service))

	containers = append(containers, sn("otelcol"), sn("jaeger"))
	if useES {
		// ES has no pointer wrapper (jaeger talks to it over plain TCP),
		// so add the .ctr IR node name directly — same as dsb_sn.
		containers = append(containers, sn("elasticsearch")+".ctr")
	}

	return containers, nil
}
