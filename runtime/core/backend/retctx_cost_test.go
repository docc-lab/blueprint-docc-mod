package backend

import (
	"encoding/base64"
	"os"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// Tomislav-RetCtx: audit-only. Compares the reverse-path envelope against the
// forward path's encoding (a single base64 of already-packed bytes) at the same
// payload. Gated so it never runs in a normal `go test`.
//   RETCTX_COST=1 go test ./runtime/core/backend -run TestRetCtxCost -bench BenchmarkRetCtx -benchtime 200000x

var benchTruss = []byte{0x06, 0x06, 0x76, 0x86, 0xd5, 0x01, 0x45, 0xd8, 0xfc, 0x5a, 0x03, 0xd1,
	0xc4, 0x91, 0xc5, 0x91, 0xc5, 0x11, 0x03}
var benchSpan = trace.SpanID{0x06, 0x8d, 0x39, 0xa6, 0x44, 0x5d, 0xc0, 0x7e}

func skipUnlessCost(tb testing.TB) {
	if os.Getenv("RETCTX_COST") == "" {
		tb.Skip("set RETCTX_COST=1")
	}
}

// forwardEncode is what the forward path does: base64 of the packed bytes.
func forwardEncode(spanID trace.SpanID, truss []byte) string {
	data := append(append([]byte(nil), spanID[:]...), truss...)
	return base64.RawURLEncoding.EncodeToString(data)
}

func TestRetCtxCost(t *testing.T) {
	skipUnlessCost(t)
	fwd := forwardEncode(benchSpan, benchTruss)
	rev := EncodeCheckpointRetCtx(benchSpan, 20, SegPathCheckpoint, benchTruss)
	t.Logf("WIRE-TTL %s", EncodeCheckpointRetCtxWithTTL(benchSpan, 300, SegStructuralCheckpoint, benchTruss, 4))
	t.Logf("payload %d raw bytes", len(benchSpan)+len(benchTruss))
	t.Logf("forward  %3d wire bytes", len(fwd))
	t.Logf("reverse  %3d wire bytes  (%.2fx)", len(rev), float64(len(rev))/float64(len(fwd)))
	t.Logf("WIRE-ONE %s", rev)
	merged := rev
	for i := 2; i <= 6; i++ {
		merged = MergeRetCtx(merged, EncodeCheckpointRetCtx(benchSpan, uint64(i), SegPathCheckpoint, benchTruss))
		t.Logf("reverse after %d merges: %4d wire bytes (%5.1f B/checkpoint)", i-1, len(merged), float64(len(merged))/float64(i))
		if i == 3 {
			t.Logf("WIRE-THREE %s", merged)
		}
	}
}

func BenchmarkRetCtxForwardEncode(b *testing.B) {
	skipUnlessCost(b)
	for i := 0; i < b.N; i++ {
		_ = forwardEncode(benchSpan, benchTruss)
	}
}

func BenchmarkRetCtxReverseEncode(b *testing.B) {
	skipUnlessCost(b)
	for i := 0; i < b.N; i++ {
		_ = EncodeCheckpointRetCtx(benchSpan, 20, SegPathCheckpoint, benchTruss)
	}
}

// One hop up the tree: the envelope is decoded, appended to and re-encoded.
func BenchmarkRetCtxMerge1(b *testing.B) { benchMerge(b, 1) }
func BenchmarkRetCtxMerge3(b *testing.B) { benchMerge(b, 3) }
func BenchmarkRetCtxMerge5(b *testing.B) { benchMerge(b, 5) }

func benchMerge(b *testing.B, hops int) {
	skipUnlessCost(b)
	one := EncodeCheckpointRetCtx(benchSpan, 20, SegPathCheckpoint, benchTruss)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := one
		for h := 0; h < hops; h++ {
			acc = MergeRetCtx(acc, one)
		}
	}
}
