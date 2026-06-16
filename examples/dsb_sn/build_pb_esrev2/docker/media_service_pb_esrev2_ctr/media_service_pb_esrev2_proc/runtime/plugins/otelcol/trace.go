// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// BridgeKindEnv is the env-var name used to pick a bridge processor at
// service startup. Keep in sync with the variant suffix in the wiring spec
// (examples/dsb_sn/wiring/specs/docker.go).
//
// Accepted values:
//
//	pb       — path-bridge (default if unset / unknown)
//	cgpb     — call-graph + path bridge
//	sb       — structural bridge
//	eb       — exact bridge
//	v        — vanilla (no-op bridge, just OTLP export)
//	priority — priority-only span processor
//	realtime — real-time partial-span processor (start/end events)
const BridgeKindEnv = "BRIDGE_KIND"

// OTCollectorTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: A functional backend running the OpenTelemetry collector.
type OTCollectorTracer struct {
	tp *tracesdk.TracerProvider
}

// NewOTCollectorTracer returns a new instance of OTCollectorTracer.
// Configures opentelemetry to export traces to the OpenTelemetry collector
// hosted at address `addr`. The additionalPort parameter is used as the
// config discovery port for fetching configuration.
//
// The bridge processor wired into the tracer provider is selected at
// startup by the BRIDGE_KIND env var (see [BridgeKindEnv]). Default is
// path-bridge; an unrecognized value logs a warning and falls back to
// path-bridge so a typo in the env doesn't crash the service.
func NewOTCollectorTracer(ctx context.Context, addr string, additionalPort string) (*OTCollectorTracer, error) {
	kind := os.Getenv(BridgeKindEnv)
	spanProcessor, resolvedKind, err := newBridgeProcessor(ctx, kind, addr, additionalPort)
	if err != nil {
		return nil, err
	}
	slog.Info("✅ OTCollectorTracer initialized", "bridge_kind", resolvedKind, "addr", addr)

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(spanProcessor),
	)
	return &OTCollectorTracer{tp}, nil
}

// newBridgeProcessor dispatches on the BRIDGE_KIND value. Returns the
// constructed processor and the resolved kind (helpful for logging when the
// env var was empty or unknown).
func newBridgeProcessor(ctx context.Context, kind, addr, port string) (tracesdk.SpanProcessor, string, error) {
	switch kind {
	case "", "pb":
		p, err := NewPathBridgeProcessor(ctx, addr, port)
		if err != nil {
			return nil, "pb", fmt.Errorf("failed to create path-bridge span processor: %w", err)
		}
		return p, "pb", nil
	case "cgpb":
		p, err := NewCallGraphBridgeProcessor(ctx, addr, port)
		if err != nil {
			return nil, "cgpb", fmt.Errorf("failed to create call-graph bridge span processor: %w", err)
		}
		return p, "cgpb", nil
	case "sb":
		p, err := NewStructuralBridgeProcessor(ctx, addr, port)
		if err != nil {
			return nil, "sb", fmt.Errorf("failed to create structural-bridge span processor: %w", err)
		}
		return p, "sb", nil
	case "eb":
		p, err := NewExactBridgeProcessor(ctx, addr, port)
		if err != nil {
			return nil, "eb", fmt.Errorf("failed to create exact-bridge span processor: %w", err)
		}
		return p, "eb", nil
	case "v", "vanilla":
		p, err := NewVanillaProcessor(ctx, addr, port)
		if err != nil {
			return nil, "v", fmt.Errorf("failed to create vanilla span processor: %w", err)
		}
		return p, "v", nil
	case "priority":
		p, err := NewPriorityProcessor(ctx, addr, port)
		if err != nil {
			return nil, "priority", fmt.Errorf("failed to create priority span processor: %w", err)
		}
		return p, "priority", nil
	case "realtime":
		p, err := NewRealTimeSpanProcessor(ctx, addr, port)
		if err != nil {
			return nil, "realtime", fmt.Errorf("failed to create realtime span processor: %w", err)
		}
		return p, "realtime", nil
	default:
		slog.Warn("⚠️ Unknown BRIDGE_KIND, falling back to path-bridge", "value", kind)
		p, err := NewPathBridgeProcessor(ctx, addr, port)
		if err != nil {
			return nil, "pb (fallback)", fmt.Errorf("failed to create path-bridge span processor: %w", err)
		}
		return p, "pb (fallback)", nil
	}
}

// Implements the backend/trace interface.
func (t *OTCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}
