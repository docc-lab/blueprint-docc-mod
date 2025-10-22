// Package specs implements wiring specs for the Greeter example.
//
// The wiring spec can be specified using the -w option when running wiring/main.go
package specs

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/clientpool"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/fancygreeter"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/greeter"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/retries"
	"github.com/blueprint-uservices/blueprint/plugins/zipkin"
)

// A wiring spec that deploys each service in its own Docker container.
var Docker = cmdbuilder.SpecOption{
	Name:        "docker",
	Description: "Deploys each service in its own Docker container with HTTP endpoints",
	Build:       makeDockerSpec,
}

func makeDockerSpec(spec wiring.WiringSpec) ([]string, error) {
	// Define the trace collector, which will be used by all services
	trace_collector := zipkin.Collector(spec, "zipkin")

	// Modifiers that will be applied to all services
	applyDockerDefaults := func(serviceName string, exposeHTTP bool) {
		// Golang-level modifiers that add functionality
		retries.AddRetries(spec, serviceName, 3)
		clientpool.Create(spec, serviceName, 10)
		opentelemetry.Instrument(spec, serviceName, trace_collector)

		// Deploy with gRPC by default, HTTP if requested
		if exposeHTTP {
			http.Deploy(spec, serviceName)
		} else {
			grpc.Deploy(spec, serviceName)
		}

		// Deploying to namespaces
		goproc.Deploy(spec, serviceName)
		linuxcontainer.Deploy(spec, serviceName)

		// Also add to tests
		gotests.Test(spec, serviceName)
	}

	// Define the basic greeter service (internal, uses gRPC)
	basic_greeter := greeter.Service(spec, "basic_greeter")
	applyDockerDefaults(basic_greeter, false)

	// Define the fancy greeter service (exposed via HTTP)
	fancy_greeter_0 := fancygreeter.Service(spec, "fancy_greeter_0", basic_greeter)
	applyDockerDefaults(fancy_greeter_0, true)

	fancy_greeter_1 := fancygreeter.Service(spec, "fancy_greeter_1", basic_greeter)
	applyDockerDefaults(fancy_greeter_1, true)

	return []string{basic_greeter, fancy_greeter_0, fancy_greeter_1}, nil
}
