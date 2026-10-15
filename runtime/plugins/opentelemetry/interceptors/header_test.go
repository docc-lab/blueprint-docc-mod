package interceptors

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/http2/hpack"
)

// The alphabet must be valid header text: 64 different visible ASCII characters, none of the ones
// left out on purpose (header.go).
func TestHeaderAlphabet(t *testing.T) {
	seen := map[rune]bool{}
	for _, c := range headerAlphabet {
		if c < 0x21 || c > 0x7e || strings.ContainsRune("\"'`\\=", c) || seen[c] {
			t.Errorf("bad or repeated character %q", c)
		}
		seen[c] = true
	}
	if len(seen) != 64 {
		t.Errorf("%d characters, want 64", len(seen))
	}
}

// What the alphabet is for: HPACK sends a value as plain bytes when Huffman coding would not make
// it shorter.  Checked on carriers shaped like PB's (random IDs, a base64 `_br`, `__seq`) and on
// reverse contexts; a few Huffman-coded ones would only cost speed.
func TestHeaderTextNotHuffmanCoded(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	huffman := func(s string) bool { return hpack.HuffmanEncodeLength(s) < uint64(len(s)) }
	const n = 5000
	carriers, retCtxs := 0, 0
	for i := 0; i < n; i++ {
		var tid trace.TraceID
		var sid trace.SpanID
		r.Read(tid[:])
		r.Read(sid[:])
		br := make([]byte, 20+r.Intn(16))
		r.Read(br)
		sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: 1})
		if huffman(encodeCarrier(sc, map[string]string{"_br": string(br), "__seq": "3"})) { // raw _br (otelcol pack.go, propBR)
			carriers++
		}
		ret := make([]byte, 30+r.Intn(90))
		r.Read(ret)
		if huffman(retCtxHeader(string(ret))) { // raw reverse context (backend reversetruss_packed.go)
			retCtxs++
		}
	}
	t.Logf("Huffman-coded: %d of %d carriers, %d of %d reverse contexts", carriers, n, retCtxs, n)
	if carriers > n/100 || retCtxs > n/20 {
		t.Errorf("too many values would still be Huffman-coded: %d carriers, %d reverse contexts", carriers, retCtxs)
	}
	// And with ordinary base64 every one of them would be (what this replaces).
	if !huffman(base64.RawURLEncoding.EncodeToString(make([]byte, 40))) {
		t.Error("expected RawURL base64 to be Huffman-coded")
	}
}

// A reverse context comes out of the trailer exactly as the server's backend produced it (raw packed
// bytes, or anything else), and the header text is always printable ASCII.
func TestRetCtxHeaderRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	real := backend.EncodeCheckpointRetCtx(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, 3, "pb", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9})
	cases := []string{real, "", base64.StdEncoding.EncodeToString([]byte(`{"fp":"0102030405060708"}`))}
	for i := 0; i < 2000; i++ {
		b := make([]byte, r.Intn(400)) // also longer than any stack buffer
		r.Read(b)
		cases = append(cases, string(b))
	}
	for _, s := range cases {
		h := retCtxHeader(s)
		for i := 0; i < len(h); i++ {
			if h[i] < 0x21 || h[i] > 0x7e {
				t.Fatalf("header text %q has a byte gRPC or HTTP would reject", h)
			}
		}
		if got := retCtxFromHeader(h); got != s {
			t.Fatalf("round trip changed %q to %q", s, got)
		}
	}
	if got := retCtxFromHeader("ab cd"); got != "" {
		t.Errorf("malformed value: got %q, want \"\"", got)
	}
}
