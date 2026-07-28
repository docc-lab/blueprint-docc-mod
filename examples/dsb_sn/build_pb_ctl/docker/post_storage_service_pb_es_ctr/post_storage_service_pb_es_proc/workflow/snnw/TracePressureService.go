package snnw

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TracePressureService generates synthetic tracing pressure on top of
// the real DSB-SN workload. Each Spam(n) call creates `n` child spans
// inside one HTTP request handler, letting an external wrk loop drive
// arbitrary span volume at the local node's otelcol independent of the
// social-graph application's throughput ceiling.
//
// All generated child spans are forced to LP via the `__bag.force_lp`
// escape hatch read by the SB processor's OnEnd. This ensures the
// pressure pump does not artificially inflate CP volume — if you want
// to see how the collector behaves under heavy LP flooding while
// preserving CP for the real workload, point wrk at this service in
// parallel with the dsb_sn wrk.
//
// Deployment: HTTP-exposed via http.Deploy (same pattern as Wrk2API);
// converted to a DaemonSet via d2k8s --daemon-services, and stripped
// of internalTrafficPolicy: Local via d2k8s --no-local-policy so an
// external client targeting any node's NodePort reaches that node's
// local pod (pressuring that node's otelcol DaemonSet pod).
type TracePressureService interface {
	// Spam emits n LP-forced child spans within the request handler,
	// then returns n. The HTTP-wrapper-generated root Spam span itself
	// is the usual CP/LP classification (will be CP for an unbaggaged
	// external caller).
	Spam(ctx context.Context, n int64) (int64, error)
}

// TracePressureServiceImpl is a stateless span generator. The tracer
// is resolved per-call from the active span's TracerProvider so we use
// the same provider Blueprint's HTTP wrapper wired in (otel.Tracer
// would return a no-op tracer here — Blueprint never calls
// otel.SetTracerProvider, it returns the TP via backend.Tracer.GetTracerProvider
// and wires it directly into the http/grpc instrumentation).
type TracePressureServiceImpl struct{}

// NewTracePressureServiceImpl creates a TracePressureService.
func NewTracePressureServiceImpl(ctx context.Context) (TracePressureService, error) {
	return &TracePressureServiceImpl{}, nil
}

// Spam creates n child spans inside the current request context. Each
// child is tagged with __bag.force_lp=true so the SB processor's OnEnd
// classifies it as LP — keeps the pressure pump from polluting CP
// counters.
func (p *TracePressureServiceImpl) Spam(ctx context.Context, n int64) (int64, error) {
	// Resolve the tracer from the active span — this gets us Blueprint's
	// real TracerProvider (the one with the SB span processor) rather
	// than the global no-op default.
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("tracepressure")
	forceLP := attribute.Bool("__bag.force_lp", true)
	for i := int64(0); i < n; i++ {
		_, span := tracer.Start(ctx, "spam_child")
		span.SetAttributes(forceLP)
		span.End()
	}
	return n, nil
}
