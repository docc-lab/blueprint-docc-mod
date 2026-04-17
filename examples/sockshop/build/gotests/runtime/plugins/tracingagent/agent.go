package tracingagent

import (
	"context"
	"fmt"
	"net"
	"os"
)

// TracingAgentService defines the interface that TracingAgent implements
type TracingAgentService interface {
	GetIP(ctx context.Context) (string, error)
	// Add more methods here as needed for tracing functionality
}

type TracingAgent struct {
	ownIP string

	// openSpans      map[string]trace.Span
	// closedSpans    map[string]trace.Span
	// orderedByStart []trace.Span
	// orderedByEnd   []trace.Span
}

// NewTracingAgent is the constructor function required by the workflow system
func NewTracingAgent(ctx context.Context) (*TracingAgent, error) {
	ownIP, err := discoverIP()
	if err != nil {
		return nil, fmt.Errorf("failed to discover own IP: %v", err)
	}

	return &TracingAgent{
		ownIP: ownIP,
	}, nil
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

// GetIP implements the TracingAgentService interface
func (t *TracingAgent) GetIP(ctx context.Context) (string, error) {
	return t.ownIP, nil
}

// func (t *TracingAgent) StartSpan(ctx context.Context, name string) *trace.Span {
// 	span := trace.NewSpan(ctx, name)
// 	t.openSpans[span.SpanID()] = span
// 	t.orderedByStart = append(t.orderedByStart, span)
// 	return span
// }

// func (t *TracingAgent) EndSpan(span *trace.Span) {
// 	span.End()
// }
