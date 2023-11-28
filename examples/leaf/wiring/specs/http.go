package specs

import (
	"fmt"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/wiring"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/goproc"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/http"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/mongodb"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/opentelemetry"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/simple"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/wiringcmd"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/workflow"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/zipkin"
)

var HTTP = wiringcmd.SpecOption{
	Name:        "http",
	Description: "Deploys each service in a separate process, communicating using HTTP.  Wraps each service in Zipkin tracing.",
	Build:       makeHTTPSpec,
}

func makeHTTPSpec(spec wiring.WiringSpec) ([]string, error) {
	trace_collector := zipkin.DefineZipkinCollector(spec, "zipkin")

	leaf_db := mongodb.PrebuiltContainer(spec, "leaf_db")
	leaf_cache := simple.Cache(spec, "leaf_cache")
	leaf_service := workflow.Define(spec, "leaf_service", "LeafServiceImpl", leaf_cache, leaf_db)
	leaf_proc := applyHTTPDefaults(spec, leaf_service, trace_collector)

	nonleaf_service := workflow.Define(spec, "nonleaf_service", "NonLeafService", leaf_service)
	nonleaf_proc := applyHTTPDefaults(spec, nonleaf_service, trace_collector)

	return []string{leaf_proc, nonleaf_proc}, nil
}

func applyHTTPDefaults(spec wiring.WiringSpec, serviceName string, collectorName string) string {
	procName := fmt.Sprintf("%s_process", serviceName)
	opentelemetry.InstrumentUsingCustomCollector(spec, serviceName, collectorName)
	http.Deploy(spec, serviceName)
	return goproc.CreateProcess(spec, procName, serviceName)
}
