// Package tracingagent implements the runtime client logic for communicating with the distributed tracing agent.
//
// This file will contain the client implementation for sending traces to the tracing agent.
package tracingagent

import (
	"context"
	"fmt"
)

// TracingAgentClient implements the client interface for communicating with the tracing agent.
// This is used by services to send traces to their local tracing agent.
type TracingAgentClient struct {
	agentAddr string
}

// Returns a new instance of TracingAgentClient.
// Configures the client to send traces to the specified agent address.
func NewTracingAgentClient(ctx context.Context, agentAddr string) (*TracingAgentClient, error) {
	client := &TracingAgentClient{
		agentAddr: agentAddr,
	}
	return client, nil
}

// SendTrace sends a trace to the tracing agent
func (client *TracingAgentClient) SendTrace(ctx context.Context, traceData []byte) error {
	// TODO: Implement trace sending logic
	// - Send trace data to the agent at client.agentAddr
	// - Handle communication protocol (HTTP/gRPC)
	// - Handle errors and retries
	return fmt.Errorf("SendTrace not yet implemented")
}

// GetAgentAddress returns the address of the agent this client communicates with
func (client *TracingAgentClient) GetAgentAddress() string {
	return client.agentAddr
}
