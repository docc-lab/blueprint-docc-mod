// Package tracingagent implements the runtime agent logic for the distributed tracing agent.
//
// This file is analogous to runtime/plugins/jaeger/trace.go and will contain the agent implementation.
package tracingagent

import (
	"context"
	"fmt"
	"net"
	"os"

	"go.opentelemetry.io/otel/sdk/trace"
)

// TracingAgent implements the runtime backend instance for the distributed tracing agent.
// This is the service that receives traces from local services, processes them,
// communicates with other agents, and forwards to the central collector.
type TracingAgent struct {
	tp               *trace.TracerProvider
	ownIP            string
	centralCollector string
	bindAddr         string
}

// Returns a new instance of TracingAgent.
// Configures the agent to receive traces and forward them to the central collector.
func NewTracingAgent(ctx context.Context, bindAddr string, centralCollector string) (*TracingAgent, error) {
	// Discover own IP address
	ownIP, err := discoverIP()
	if err != nil {
		return nil, fmt.Errorf("failed to discover own IP: %w", err)
	}

	// Create a basic tracer provider for the agent
	tp := trace.NewTracerProvider()

	agent := &TracingAgent{
		tp:               tp,
		ownIP:            ownIP,
		centralCollector: centralCollector,
		bindAddr:         bindAddr,
	}

	return agent, nil
}

// getOwnIP returns the agent's own IP address
func (agent *TracingAgent) getOwnIP() string {
	return agent.ownIP
}

// discoverIP discovers the agent's own IP address
func discoverIP() (string, error) {
	// Try Kubernetes environment first
	if podIP := os.Getenv("POD_IP"); podIP != "" {
		return podIP, nil
	}

	// Fall back to Go's standard library for Docker/other environments
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no local IP found")
}

// Implements the backend/trace interface.
func (agent *TracingAgent) GetTracerProvider(ctx context.Context) (*trace.TracerProvider, error) {
	return agent.tp, nil
}

// Start starts the tracing agent service
func (agent *TracingAgent) Start(ctx context.Context) error {
	// TODO: Implement agent startup logic
	// - Start HTTP/gRPC server to receive traces
	// - Set up communication with central collector
	// - Initialize agent-to-agent communication
	return nil
}

// Stop stops the tracing agent service
func (agent *TracingAgent) Stop(ctx context.Context) error {
	// TODO: Implement agent shutdown logic
	return nil
}
