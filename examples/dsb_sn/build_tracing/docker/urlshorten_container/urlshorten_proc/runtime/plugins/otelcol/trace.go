// Package otelcol implements a tracer [backend.Tracer] client interface that
// exports the spans of an instrumented Blueprint service to an OpenTelemetry
// Collector over OTLP (gRPC by default, or HTTP).
//
// Used through the otelcol plugin: otelcol.Collector(spec, name) in a wiring
// spec, passed to opentelemetry.Instrument(spec, service, name). The services
// use the OpenTelemetry Go SDK; this file decides how their spans are
// processed and exported.
//
// Every setting below can be changed per Deployment with environment
// variables, so one image covers all SDK-side experiments.
//
// Read by this file (standard OpenTelemetry names where one exists):
//
//	OTEL_TRACES_EXPORTER          otlp (default) | console | none
//	                              none: spans are still created and sampled
//	                              but never exported (instrumentation cost only)
//	                              console: printed to the pod log
//	OTEL_EXPORTER_OTLP_TRACES_PROTOCOL (or OTEL_EXPORTER_OTLP_PROTOCOL)
//	                              grpc (default, to addr, normally port 4317) |
//	                              http/protobuf (to the same host, port 4318)
//	DSB_OTEL_SPAN_PROCESSOR       batch (default) | simple
//	                              simple: every span is exported on its own,
//	                              synchronously, when it ends
//
// Read by the OpenTelemetry Go SDK itself (defaults in brackets):
//
//	OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG   [parentbased_always_on]
//	    e.g. traceidratio + 0.1, always_off, parentbased_traceidratio
//	OTEL_BSP_SCHEDULE_DELAY [5000 ms]  OTEL_BSP_MAX_EXPORT_BATCH_SIZE [512]
//	OTEL_BSP_MAX_QUEUE_SIZE [2048]     OTEL_BSP_EXPORT_TIMEOUT [30000 ms]
//	OTEL_EXPORTER_OTLP_TRACES_COMPRESSION [none]  (gzip)
//	OTEL_EXPORTER_OTLP_TRACES_TIMEOUT [10000 ms]
//	OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT, OTEL_SPAN_EVENT_COUNT_LIMIT, ... [128]
//	OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES
//
// The endpoint and "no TLS" are set in code, so the OTEL_*_ENDPOINT and
// *_INSECURE variables are ignored.
package otelcol

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// OTelCollectorTracer implements the runtime backend instance that implements the backend/trace.Tracer interface.
// REQUIRED: an OpenTelemetry Collector that accepts OTLP at the given address
// (gRPC, normally port 4317) or the same host on port 4318 (HTTP).
type OTelCollectorTracer struct {
	tp *tracesdk.TracerProvider
}

// firstEnv returns the first non-empty variable among keys, lower-cased.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

// httpAddr turns the OTLP/gRPC address host:4317 into the OTLP/HTTP one, host:4318.
func httpAddr(addr string) string {
	if strings.HasSuffix(addr, ":4317") {
		return strings.TrimSuffix(addr, ":4317") + ":4318"
	}
	return addr
}

// Returns a new instance of OTelCollectorTracer, configured from the environment (see above).
func NewOTelCollectorTracer(ctx context.Context, addr string) (*OTelCollectorTracer, error) {
	var exp tracesdk.SpanExporter
	var err error

	switch exporter := firstEnv("OTEL_TRACES_EXPORTER"); exporter {
	case "", "otlp":
		switch proto := firstEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"); proto {
		case "", "grpc":
			exp, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(addr), otlptracegrpc.WithInsecure())
		case "http/protobuf", "http":
			exp, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(httpAddr(addr)), otlptracehttp.WithInsecure())
		default:
			return nil, fmt.Errorf("unsupported OTLP protocol %q (use grpc or http/protobuf)", proto)
		}
	case "console":
		exp, err = stdouttrace.New()
	case "none":
		// no exporter: spans are created, sampled and ended, then dropped
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q (use otlp, console or none)", exporter)
	}
	if err != nil {
		return nil, err
	}

	var opts []tracesdk.TracerProviderOption
	if exp != nil {
		switch proc := firstEnv("DSB_OTEL_SPAN_PROCESSOR"); proc {
		case "", "batch":
			opts = append(opts, tracesdk.WithBatcher(exp)) // batch size, delay, queue from OTEL_BSP_*
		case "simple":
			opts = append(opts, tracesdk.WithSyncer(exp))
		default:
			return nil, fmt.Errorf("unsupported DSB_OTEL_SPAN_PROCESSOR %q (use batch or simple)", proc)
		}
	}
	tp := tracesdk.NewTracerProvider(opts...) // sampler and span limits from OTEL_* (SDK)
	return &OTelCollectorTracer{tp}, nil
}

// Implements the backend/trace interface.
func (t *OTelCollectorTracer) GetTracerProvider(ctx context.Context) (trace.TracerProvider, error) {
	return t.tp, nil
}

