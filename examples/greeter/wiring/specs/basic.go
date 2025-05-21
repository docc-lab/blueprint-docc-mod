// Package specs implements wiring specs for the Greeter example.
//
// The wiring spec can be specified using the -w option when running wiring/main.go
package specs

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/fancygreeter"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/greeter"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/zipkin"
)

// A wiring spec that compiles all services into a single process.
var Basic = cmdbuilder.SpecOption{
	Name:        "basic",
	Description: "Compiles all services into a single process with HTTP endpoints",
	Build:       makeBasicSpec,
}

func makeBasicSpec(spec wiring.WiringSpec) ([]string, error) {
	// Define the trace collector, which will be used by all services
	trace_collector := zipkin.Collector(spec, "zipkin")

	// Modifiers that will be applied to all services
	applyBasicDefaults := func(serviceName string, exposeHTTP bool) {
		// Add OpenTelemetry instrumentation
		opentelemetry.Instrument(spec, serviceName, trace_collector)

		// Deploy with HTTP only if requested (for external access)
		if exposeHTTP {
			http.Deploy(spec, serviceName)
		}

		// Also add to tests
		gotests.Test(spec, serviceName)
	}

	// Define the basic greeter service (internal)
	basic_greeter := greeter.Service(spec, "basic_greeter")
	applyBasicDefaults(basic_greeter, false)

	// Define the fancy greeter service (exposed via HTTP)
	fancy_greeter := fancygreeter.Service(spec, "fancy_greeter", basic_greeter)
	applyBasicDefaults(fancy_greeter, true)

	return []string{basic_greeter, fancy_greeter}, nil
}
