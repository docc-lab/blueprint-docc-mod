package backend

// Tomislav-RetCtx: a span's outgoing baggage without a span-attribute scan.
//
// The generated OT wrappers (plugins/opentelemetry) build each outgoing carrier's baggage
// from the span's "__bag."-prefixed attributes. Reading them means Attributes(), which in the
// OTel SDK locks the span and dedupes the whole attribute set into a fresh map on every call,
// once per client and once per server span. A tracing SDK that keeps its propagation state
// beside the span registers a source here; the wrappers ask it first and scan attributes only
// when it has nothing for the span (vanilla, legacy processors), so their behavior is
// unchanged.

import (
	"go.opentelemetry.io/otel/trace"
)

// SpanBaggageSource returns the baggage entry a span contributes to its outgoing carriers.
// ok=false means the source knows nothing about the span and the caller must fall back to
// the span's "__bag." attributes.
type SpanBaggageSource func(sc trace.SpanContext) (key, value string, ok bool)

// Set once, from the tracing SDK's package init, before any request runs. (A plain variable:
// the Blueprint compiler parses this package and its Go parser does not accept generics.)
var spanBaggageSource SpanBaggageSource

// RegisterSpanBaggageSource installs the process's source (the tracing SDK, at init).
func RegisterSpanBaggageSource(f SpanBaggageSource) { spanBaggageSource = f }

// AppendSpanBaggage adds the span's baggage entry from the registered source and reports
// whether the source handled the span (the caller then skips its attribute scan).
func AppendSpanBaggage(span trace.Span, baggage map[string]string) bool {
	f := spanBaggageSource
	if f == nil {
		return false
	}
	key, value, ok := f(span.SpanContext())
	if !ok {
		return false
	}
	baggage[key] = value
	return true
}
