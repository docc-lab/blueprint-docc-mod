package core

import (
	"context"
)

// TraceProvider is an interface that must be implemented by trace providers
// that want to receive pings from the coordinator
type TraceProvider interface {
	// Ping is called by the coordinator to ping the provider
	Ping(ctx context.Context) error
}
