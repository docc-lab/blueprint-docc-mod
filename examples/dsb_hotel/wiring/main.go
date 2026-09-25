// Package main provides an application for compiling a number of different
// wiring specs for the Hotel Reservation application from the DeathStarBench suite.
//
// To display options and usage, invoke:
//
//	go run main.go -h
package main

import (
	_ "github.com/blueprint-uservices/blueprint/examples/dsb_hotel/tests"
	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/wiring/specs"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
)

func main() {
	workflowspec.AddModule("github.com/blueprint-uservices/blueprint/examples/dsb_hotel/tests")

	name := "Hotel"
	cmdbuilder.MakeAndExecute(
		name,
		specs.Original,
		specs.DockerPB,
		specs.DockerCGPB,
		specs.DockerSB,
		specs.DockerV,
		specs.DockerPBES,
		specs.DockerCGPBES,
		specs.DockerSBES,
		specs.DockerVES,
		specs.DockerRCES,
		specs.DockerNTES,
		// Tomislav-RetCtx: zero-work variants (workflow/hotelnw)
		specs.DockerVESNW,
		specs.DockerPBESNW,
		specs.DockerCGPBESNW,
		specs.DockerSBESNW,
		specs.DockerNTESNW,
	)
}
