// Package main provides an application for compiling a number of different
// wiring specs for the Hotel Reservation application from the DeathStarBench suite.
//
// To display options and usage, invoke:
//
//	go run main.go -h
package main

import (
	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/wiring/specs"
	"github.com/blueprint-uservices/blueprint/plugins/wiringcmd"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
)

func main() {
	workflow.Init("../workflow", "../tests")

	name := "Hotel"
	wiringcmd.MakeAndExecute(
		name,
		specs.Original,
	)
}
