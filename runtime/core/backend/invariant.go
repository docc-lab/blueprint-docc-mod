package backend

import "fmt"

// Tomislav-RetCtx (user 2026-11-06: assertions in the tracing instrumentation, simple comparisons only:
// bounds checks and structural consistency).  The wrapper templates (plugins/opentelemetry/ir_ot_*.go)
// and the interceptors and HTTP middleware (runtime/plugins/opentelemetry/interceptors) check
// conditions that only a bug in the tracing code itself can break: the bounds of child ordinals and
// counters, and the structure of what one traced service sends another (carriers, reverse contexts,
// one value per header).  A broken one panics instead of letting the span processors build a wrong
// trace; the experiment harness already fails a run on any pod restart.
//
// Each check is one comparison on the hot path.  InvariantBroken is called only in the branch where
// a check failed, so its arguments are only evaluated (and formatted) then.

// InvariantError is the panic value of a broken tracing invariant.
type InvariantError struct {
	Rule   string // what must hold, e.g. "the caller's child ordinal is >= 1"
	Detail string // the values that broke it
}

func (e *InvariantError) Error() string {
	return "tracing invariant broken: " + e.Rule + " (" + e.Detail + ")"
}

// InvariantBroken panics with an *InvariantError.  details are formatted with fmt.Sprint.
//
// Kept out of line so that a check adds only a compare and a call to the code around it.
//
//go:noinline
func InvariantBroken(rule string, details ...any) {
	panic(&InvariantError{Rule: rule, Detail: fmt.Sprint(details...)})
}

// IsPackedRetCtx reports whether a reverse context is in the packed binary envelope
// (reversetruss_packed.go), the only form any traced service sends: what the backend merges and
// PrepareCheckpoint returns, before a transport turns it into text.  Empty means "none" and is not
// packed.
func IsPackedRetCtx(retCtx string) bool {
	return len(retCtx) > 0 && retCtx[0] == packedRetCtxVersion
}
