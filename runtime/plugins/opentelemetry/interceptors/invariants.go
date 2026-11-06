package interceptors

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
)

// Tomislav-RetCtx (user 2026-11-06): the failure paths of the interceptors' invariant checks.  They
// format their typed arguments and call backend.InvariantBroken (runtime/core/backend/invariant.go),
// which panics.  Calling backend.InvariantBroken directly with boxed values would make every checking
// function reserve stack space for that boxing on every call (setup's frame grew from 48 to 536
// bytes); out of line, a check costs a compare and a call that passes a few words.

// checkServerSpan checks that the span the SDK started for a server call is valid and continues the
// trace of the caller (the remote parent on caller, if there is one).  Out of line, so that the two
// span contexts it compares do not sit in setup's frame (server.go).
//
//go:noinline
func checkServerSpan(span trace.Span, caller context.Context) {
	sc := span.SpanContext()
	if !sc.IsValid() {
		brokenSpanContext("the server span has a valid span context", &sc)
	}
	if parent := trace.SpanContextFromContext(caller); parent.IsValid() && parent.TraceID() != sc.TraceID() {
		brokenTraces("the server span continues the caller's trace", parent.TraceID(), sc.TraceID())
	}
}

// checkClientSpan checks that a client span is valid before its context goes to the callee, which
// would otherwise have no parent to continue.  Out of line for the same reason as checkServerSpan
// (carrierFor's frame, client.go).
//
//go:noinline
func checkClientSpan(span trace.Span) {
	if sc := span.SpanContext(); !sc.IsValid() {
		brokenSpanContext("the client span has a valid span context", &sc)
	}
}

//go:noinline
func brokenSpanContext(rule string, sc *trace.SpanContext) {
	brokenIDs(rule, sc.TraceID(), sc.SpanID())
}

//go:noinline
func brokenIDs(rule string, tid trace.TraceID, sid trace.SpanID) {
	backend.InvariantBroken(rule, "trace=", tid.String(), " span=", sid.String())
}

//go:noinline
func brokenTraces(rule string, caller, span trace.TraceID) {
	backend.InvariantBroken(rule, "caller=", caller.String(), " span=", span.String())
}

//go:noinline
func brokenCount(rule, what string, n, bound int) {
	backend.InvariantBroken(rule, what, "=", n, " bound=", bound)
}

//go:noinline
func brokenValue(rule, what, value string) {
	backend.InvariantBroken(rule, what, "=", value)
}
