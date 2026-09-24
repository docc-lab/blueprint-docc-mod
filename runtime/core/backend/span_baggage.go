package backend

// Tomislav-RetCtx: baggage a tracing SDK keeps beside the span.
//
// The generated OT wrappers (plugins/opentelemetry) build each outgoing carrier's baggage from
// the upstream baggage plus the span's "__bag."-prefixed attributes -- Blueprint's convention for
// arbitrary baggage, which is unchanged. A tracing SDK that keeps its own propagation state
// beside the span instead of in attributes (the bridges, wire_state.go) registers a source here;
// the wrappers merge its entries first and then apply the span's "__bag." attributes, so any
// other producer's attribute baggage still propagates and still takes precedence.

import (
	"go.opentelemetry.io/otel/trace"
)

// SpanBaggageSource adds the baggage entries the SDK keeps for a span to baggage (any number of
// entries; none if it knows nothing about the span).
type SpanBaggageSource func(sc trace.SpanContext, baggage map[string]string)

// Set once, from the tracing SDK's package init, before any request runs. (A plain variable:
// the Blueprint compiler parses this package and its Go parser does not accept generics.)
var spanBaggageSource SpanBaggageSource

// RegisterSpanBaggageSource installs the process's source (the tracing SDK, at init).
func RegisterSpanBaggageSource(f SpanBaggageSource) { spanBaggageSource = f }

// AppendSpanBaggage adds the registered source's entries for span to baggage.
func AppendSpanBaggage(span trace.Span, baggage map[string]string) {
	if f := spanBaggageSource; f != nil {
		f(span.SpanContext(), baggage)
	}
}
