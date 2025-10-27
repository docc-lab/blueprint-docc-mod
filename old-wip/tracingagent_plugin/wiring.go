// Package tracingagent provides a plugin to generate and include a tracing agent instance in a Blueprint application.
//
// This file is analogous to plugins/jaeger/wiring.go and contains the main wiring functions for the tracing agent plugin.
package tracingagent

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
)

// Service defines a tracing agent that can be deployed in a Blueprint application.
// The agent receives traces from local services, processes them, communicates with other agents,
// and forwards them to a central collector.
//
// This is analogous to jaeger.Collector but creates a tracing agent service instead of a pre-built container.
func Service(spec wiring.WiringSpec, agentName string, centralCollector string) string {
	// The nodes that we are defining
	agentAddr := agentName + ".addr"
	agentCtr := agentName + ".ctr"
	agentClient := agentName + ".client"

	// Define the tracing agent container
	spec.Define(agentCtr, &TracingAgentContainer{}, func(ns wiring.Namespace) (ir.IRNode, error) {
		agent, err := newTracingAgentContainer(agentCtr, centralCollector)
		if err != nil {
			return nil, err
		}
		err = address.Bind[*TracingAgentContainer](ns, agentAddr, agent, &agent.BindAddr)
		return agent, err
	})

	// Create a pointer to the agent
	ptr := pointer.CreatePointer[*TracingAgentClient](spec, agentName, agentCtr)

	// Define the address that points to the agent
	address.Define[*TracingAgentContainer](spec, agentAddr, agentCtr)

	// Add the address to the pointer
	ptr.AddAddrModifier(spec, agentAddr)

	// Define the agent client and add it to the client side of the pointer
	clientNext := ptr.AddSrcModifier(spec, agentClient)
	spec.Define(agentClient, &TracingAgentClient{}, func(ns wiring.Namespace) (ir.IRNode, error) {
		addr, err := address.Dial[*TracingAgentContainer](ns, clientNext)
		if err != nil {
			return nil, err
		}

		return newTracingAgentClient(agentClient, addr.Dial)
	})

	// Return the pointer; anybody who wants to access the tracing agent should do so through the pointer
	return agentName
}

// Instrument adds OpenTelemetry instrumentation to a service, configuring it to send traces
// to the specified tracing agent instead of directly to a central collector.
//
// This is analogous to opentelemetry.Instrument but targets the tracing agent.
func Instrument(spec wiring.WiringSpec, serviceName string, agentName string) {
	// Use the existing OpenTelemetry plugin but configure it to use our agent
	opentelemetry.Instrument(spec, serviceName, agentName)
}
