def sub(path, old, new, count=1):
    s = open(path).read()
    assert s.count(old) == count, (path, s.count(old), old[:90])
    s = s.replace(old, new); open(path, 'w').write(s)

p = 'runtime/core/backend/span_baggage.go'
s = open(p).read()
start = s.index('// Tomislav-RetCtx: a span')
s = s[:start] + """// Tomislav-RetCtx: baggage a tracing SDK keeps beside the span.
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
"""
open(p, 'w').write(s)

sub('runtime/plugins/otelcol/wire_state.go', """	backend.RegisterSpanBaggageSource(func(sc trace.SpanContext) (string, string, bool) {
		if w := bridgeWires.load(sc.SpanID()); w != nil {
			return BaggageBRKey, w.prop, true
		}
		return "", "", false
	})""", """	backend.RegisterSpanBaggageSource(func(sc trace.SpanContext, baggage map[string]string) {
		if w := bridgeWires.load(sc.SpanID()); w != nil {
			baggage[BaggageBRKey] = w.prop
		}
	})""")
sub('runtime/plugins/otelcol/wire_state.go', """// The RPC wrappers take a bridge span's outgoing baggage from here instead of scanning its
// attributes for __bag._br (backend.AppendSpanBaggage).""", """// The RPC wrappers merge a bridge span's outgoing `_br` from here (backend.AppendSpanBaggage)
// before applying the span's `__bag.` attributes, which bridges no longer set.""")

old = ("\t// Tomislav-RetCtx: the SDK hands over its propagation state directly when it keeps it\n"
       "\t// beside the span (runtime/core/backend/span_baggage.go); scan attributes otherwise.\n"
       "\tif rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok && !backend.AppendSpanBaggage(span, baggage) {")
new = ("\t// Tomislav-RetCtx: baggage the SDK keeps beside the span first (runtime/core/backend/\n"
       "\t// span_baggage.go), then the span's \"__bag.\" attributes as always (they take precedence).\n"
       "\tbackend.AppendSpanBaggage(span, baggage)\n"
       "\tif rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {")
for f in ('plugins/opentelemetry/ir_ot_client.go', 'plugins/opentelemetry/ir_ot_server.go'):
    s = open(f).read(); n = s.count(old); assert n == 5, (f, n); s = s.replace(old, new); open(f, 'w').write(s)

sub('runtime/plugins/otelcol/request_cost_test.go', """	if rw, ok := span.(sdktrace.ReadWriteSpan); ok && !backend.AppendSpanBaggage(span, baggage) {""",
    """	backend.AppendSpanBaggage(span, baggage)
	if rw, ok := span.(sdktrace.ReadWriteSpan); ok {""")
print('ok')
