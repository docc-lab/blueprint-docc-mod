// Package specs implements wiring specs for the Greeter example.
//
// The wiring spec can be specified using the -w option when running wiring/main.go
package specs

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/fanciergreeter"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/zipkin"
)

// A wiring spec that compiles all services into a single process.
var Fancier = cmdbuilder.SpecOption{
	Name:        "fancier",
	Description: "Compiles all services into a single process with HTTP endpoints",
	Build:       makeFancierSpec,
}

func makeFancierSpec(spec wiring.WiringSpec) ([]string, error) {
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

	// Define the fancier greeter service (exposed via HTTP)
	fancier_greeter := fanciergreeter.Service(spec, "fancier_greeter")

	// Get all services defined in the spec
	services := spec.Defs()

	// Apply defaults to all services
	for _, service := range services {
		// Apply defaults to all services
		applyBasicDefaults(service, service == fancier_greeter)
	}

	return []string{fancier_greeter}, nil
}
