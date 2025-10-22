// An application for compiling the Greeter example.
// Provides different wiring specs for compiling
// the application in different configurations.
//
// To display options and usage, invoke:
//
//	go run main.go -h
package main

import (
	"github.com/blueprint-uservices/blueprint/examples/fancier_greeter/wiring/specs"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
)

func main() {
	// Build a supported wiring spec
	name := "Greeter"
	cmdbuilder.MakeAndExecute(
		name,
		specs.Fancier,
		specs.FancierDocker,
	)
}
