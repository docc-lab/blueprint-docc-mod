// Package main compiles the PayloadBench application — a minimal two-service
// chain (HTTP EdgeService → gRPC InternalService) for measuring the effect of
// forward-path vs return-path inter-service payload size on throughput and
// response time.
//
// To display options and usage, invoke:
//
//	go run main.go -h
package main

import (
	"github.com/blueprint-uservices/blueprint/apps/payloadbench/wiring/specs"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
)

func main() {
	// Configure the location of our workflow spec
	workflowspec.AddModule("github.com/blueprint-uservices/blueprint/apps/payloadbench/workflow")

	name := "PayloadBench"
	cmdbuilder.MakeAndExecute(
		name,
		specs.DockerV,
		specs.DockerPB,
		specs.DockerCGPB,
		specs.DockerSB,
		specs.DockerVES,
		specs.DockerPBES,
		specs.DockerCGPBES,
		specs.DockerSBES,
		specs.DockerNTES,
	)
}
