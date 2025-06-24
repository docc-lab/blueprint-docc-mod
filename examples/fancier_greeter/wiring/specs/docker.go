// Package specs implements wiring specs for the Greeter example.
//
// The wiring spec can be specified using the -w option when running wiring/main.go
package specs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/clientpool"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/fanciergreeter"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/retries"
	"github.com/blueprint-uservices/blueprint/plugins/zipkin"
)

// A wiring spec that deploys each service in its own Docker container.
var FancierDocker = cmdbuilder.SpecOption{
	Name:        "fancier_docker",
	Description: "Deploys each service in its own Docker container with HTTP endpoints",
	Build:       makeFancierDockerSpec,
}

func makeFancierDockerSpec(spec wiring.WiringSpec) ([]string, error) {
	// Define the trace collector, which will be used by all services
	zipkin_collector := zipkin.Collector(spec, "zipkin")
	trace_collector := otelcol.Collector(spec, "otelcol", zipkin_collector)

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

	// Define the fancier greeter service (exposed via HTTP)
	fancier_greeter := fanciergreeter.Service(spec, "fancier_greeter")
	// fancier_greeter_2 := fanciergreeter.Service(spec, "fancier_greeter_2")

	// Get all services defined in the spec
	services := spec.Defs()

	fmt.Println("All nodes in spec:")
	for _, service := range services {
		fmt.Printf("  - %s\n", service)
	}

	// exposeHTTP := []string{fancier_greeter, fancier_greeter_2}
	exposeHTTP := []string{fancier_greeter}

	// Apply Docker defaults to all service nodes
	for _, service := range services {
		// Only apply defaults to actual service nodes (those ending in .service)
		if strings.HasSuffix(service, ".service") {
			// Get the base service name without the .service suffix
			baseName := strings.TrimSuffix(service, ".service")
			fmt.Printf("Applying defaults to service: %s (base name: %s)\n", service, baseName)
			// Apply defaults to all services
			applyDockerDefaults(baseName, slices.Contains(exposeHTTP, baseName))
		}
	}

	// return []string{fancier_greeter, fancier_greeter_2}, nil
	return []string{fancier_greeter, "zipkin.ctr", "otelcol.ctr"}, nil
}
