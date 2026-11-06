package backend

import (
	"errors"
	"strings"
	"testing"
)

// Tomislav-RetCtx: InvariantBroken panics with an *InvariantError naming the rule and the values.
func TestInvariantBroken(t *testing.T) {
	defer func() {
		r := recover()
		err, ok := r.(error)
		var ie *InvariantError
		if !ok || !errors.As(err, &ie) {
			t.Fatalf("panic value %#v is not an *InvariantError", r)
		}
		if ie.Rule != "ordinal >= 1" || ie.Detail != "seqNum=0" {
			t.Errorf("got rule %q detail %q", ie.Rule, ie.Detail)
		}
		if !strings.Contains(err.Error(), "ordinal >= 1") || !strings.Contains(err.Error(), "seqNum=0") {
			t.Errorf("message %q lacks the rule or the values", err.Error())
		}
	}()
	InvariantBroken("ordinal >= 1", "seqNum=", 0)
	t.Fatal("InvariantBroken returned")
}

// Tomislav-RetCtx: IsPackedRetCtx accepts exactly what the backend's packed envelope starts with.
func TestIsPackedRetCtx(t *testing.T) {
	merged := MergeRetCtx(string([]byte{packedRetCtxVersion}), "")
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"", false},
		{string([]byte{packedRetCtxVersion}), true},
		{merged, merged != ""},
		{"eyJ0cnVzcyI6W119", false},                                 // the legacy JSON envelope's text
		{RetCtxText(string([]byte{packedRetCtxVersion, 0})), false}, // text form: transports only
	} {
		if got := IsPackedRetCtx(c.in); got != c.want {
			t.Errorf("IsPackedRetCtx(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
