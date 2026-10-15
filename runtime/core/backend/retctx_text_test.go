package backend

// Tomislav-RetCtx: the reverse context is raw packed bytes in the process and text only where a
// carrier needs text (reversetruss_packed.go, RetCtxText / RetCtxFromText).

import (
	"encoding/base64"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestRetCtxRawAndText(t *testing.T) {
	a := EncodeCheckpointRetCtxWithTTL(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3, SegPathCheckpoint, []byte{0xff, 0, 7}, 2)
	b := EncodeCheckpointRetCtx(trace.SpanID{9, 9, 9, 9, 9, 9, 9, 9}, 1, SegStructuralCheckpoint, []byte{1})
	if a[0] != packedRetCtxVersion || b[0] != packedRetCtxVersion {
		t.Fatalf("in-process form must be the raw envelope: %q %q", a, b)
	}

	// Text is RawURL base64 of the raw bytes, exactly what the wrappers' field and `_rc` held.
	text := RetCtxText(a)
	if text != base64.RawURLEncoding.EncodeToString([]byte(a)) || RetCtxFromText(text) != a {
		t.Fatalf("text form %q does not round-trip", text)
	}
	if RetCtxText("") != "" || RetCtxFromText("") != "" || RetCtxFromText(a) != a {
		t.Fatal("empty and raw inputs must pass through")
	}
	// The JSON envelope (StdEncoding text) is text already and stays as it is both ways.
	jsonEnv := encodeTruss(trussData{FP: "x", Par: "p", Segs: []TrussSegment{{Kind: SegAMQ, Data: []byte{1}}}})
	if RetCtxText(jsonEnv) != jsonEnv || RetCtxFromText(jsonEnv) != jsonEnv {
		t.Fatal("JSON envelope must pass through")
	}

	// Merging concatenates raw envelopes, and decoding gives back both checkpoints in order, from the
	// raw form and from its text form alike.
	merged := MergeRetCtx(a, b)
	if merged != a+b[1:] {
		t.Fatalf("merge is not a concatenation: %q", merged)
	}
	fromRaw, err := DecodeReturnedCheckpoints(merged)
	if err != nil || len(fromRaw) != 2 || fromRaw[0].Depth != 3 || *fromRaw[0].ReverseTTL != 2 || fromRaw[1].Kind != SegStructuralCheckpoint {
		t.Fatalf("decode of the merged raw envelope: %+v %v", fromRaw, err)
	}
	fromText, err := DecodeReturnedCheckpoints(RetCtxText(merged))
	if err != nil || !reflect.DeepEqual(fromRaw, fromText) {
		t.Fatalf("text form decodes differently: %+v vs %+v (%v)", fromText, fromRaw, err)
	}
	// Decoded trusses are copies: changing one must not change the envelope (a string).
	fromRaw[0].Truss[0] = 0
	if again, _ := DecodeReturnedCheckpoints(merged); again[0].Truss[0] != 0xff {
		t.Fatal("decoded truss aliases the envelope")
	}

	// Routing that changes nothing returns the very same string (no re-encode); a TTL step re-packs.
	if _, fwd := RouteRetCtx(b, false); fwd != b {
		t.Fatal("an untouched bundle must be forwarded as is")
	}
	if _, fwd := RouteRetCtx(a, false); fwd == a || fwd[0] != packedRetCtxVersion {
		t.Fatalf("a TTL step must re-pack the raw envelope: %q", fwd)
	}
	// A corrupt raw envelope is not merged: the valid side is kept as it is.
	if got := MergeRetCtx(a, string([]byte{packedRetCtxVersion, 1, 200})); got != a {
		t.Fatalf("corrupt envelope merged: %q", got)
	}
}
