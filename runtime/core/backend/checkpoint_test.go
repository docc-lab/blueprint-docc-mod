package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestReturnedCheckpointLocationsSurviveMerge(t *testing.T) {
	a := ReturnedCheckpoint{Kind: SegPathCheckpoint, SpanID: trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, Depth: 130, Truss: []byte{0x80, 0, 0xff}}
	b := ReturnedCheckpoint{Kind: SegStructuralCheckpoint, SpanID: trace.SpanID{8, 7, 6, 5, 4, 3, 2, 1}, Depth: 16384, Truss: []byte{1, 0, 2}}
	first := EncodeCheckpointRetCtx(a.SpanID, a.Depth, a.Kind, a.Truss)
	second := EncodeCheckpointRetCtx(b.SpanID, b.Depth, b.Kind, b.Truss)
	merged := MergeRetCtx(first, second)
	got, err := DecodeReturnedCheckpoints(merged)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []ReturnedCheckpoint{a, b}) {
		t.Fatalf("checkpoint origins or trusses changed during merge: %+v", got)
	}
	envelope, _ := decodeTruss(merged)
	for i, cp := range []ReturnedCheckpoint{a, b} {
		data := envelope.Segs[i].Data
		depth, n := binary.Uvarint(data[8:])
		if depth != cp.Depth || n <= 1 || !bytes.Equal(data[:8], cp.SpanID[:]) || !bytes.Equal(data[8+n:], cp.Truss) {
			t.Fatalf("segment %d is not spanID || uvarint(depth) || truss", i)
		}
	}
	if got := MergeRetCtx(merged, ""); got != merged {
		t.Fatal("forwarding rewrote the checkpoint locations")
	}
}

func TestReturnedCheckpointFanInPreservesDescendantBundles(t *testing.T) {
	ctx := WithRetMerge(context.Background())
	want := make(map[trace.SpanID]ReturnedCheckpoint)
	var children []string
	// Each sibling returns a bundle that already includes a deeper descendant.
	for i := byte(1); i <= 8; i++ {
		var bundle string
		for _, id := range []byte{i, i + 8} {
			ttl := id - 1
			cp := ReturnedCheckpoint{Kind: SegPathCheckpoint, SpanID: trace.SpanID{id}, Depth: 128 + uint64(id), Truss: []byte{id, 0xff}, ReverseTTL: &ttl}
			want[cp.SpanID] = cp
			bundle = MergeRetCtx(bundle, EncodeCheckpointRetCtxWithTTL(cp.SpanID, cp.Depth, cp.Kind, cp.Truss, ttl))
		}
		children = append(children, bundle)
	}
	var wg sync.WaitGroup
	for _, bundle := range children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			AddToMerge(ctx, bundle)
		}()
	}
	wg.Wait()
	// The parent can also append its own refused checkpoint before returning.
	own := ReturnedCheckpoint{Kind: SegStructuralCheckpoint, SpanID: trace.SpanID{17}, Depth: 127, Truss: []byte{17, 0xfe}}
	want[own.SpanID] = own
	retCtx := MergeRetCtx(MergedChildren(ctx), EncodeCheckpointRetCtx(own.SpanID, own.Depth, own.Kind, own.Truss))
	cps, err := DecodeReturnedCheckpoints(retCtx)
	if err != nil || len(cps) != len(want) {
		t.Fatalf("fan-in lost or duplicated checkpoints: got %d want %d, %v", len(cps), len(want), err)
	}
	for _, cp := range cps {
		if !reflect.DeepEqual(cp, want[cp.SpanID]) {
			t.Fatalf("fan-in changed an original checkpoint: %+v", cp)
		}
		delete(want, cp.SpanID)
	}
	if cps[len(cps)-1].SpanID != own.SpanID {
		t.Fatal("parent checkpoint was not appended after child bundles")
	}
}

func TestReturnedCheckpointRejectsMalformedLocation(t *testing.T) {
	for _, data := range [][]byte{
		{1, 2, 3},
		{1, 2, 3, 4, 5, 6, 7, 8, 0x80}, // incomplete varint
		make([]byte, 9),                // invalid all-zero span ID
	} {
		encoded := encodeTruss(trussData{Segs: []TrussSegment{{Kind: SegPathCheckpoint, Data: data}}})
		if _, err := DecodeReturnedCheckpoints(encoded); err == nil {
			t.Fatalf("accepted invalid location: %x", data)
		}
	}
	if _, err := DecodeReturnedCheckpoints("not-base64"); err == nil {
		t.Fatal("accepted an invalid envelope")
	}
}

// Tomislav-RetCtx: splitting at one emission point must preserve each origin,
// truss, and independent countdown, including zero and the maximum byte TTL.
func TestReverseTTLPartitionsEveryHop(t *testing.T) {
	var pending string
	for i, ttl := range []byte{0, 0, 2, 255} {
		id := trace.SpanID{byte(i + 1)}
		pending = MergeRetCtx(pending, EncodeCheckpointRetCtxWithTTL(id, 16384, SegCallGraphCheckpoint, []byte{ttl, 0xff, 0x80}, ttl))
	}
	original := pending
	want, err := DecodeReturnedCheckpoints(pending)
	if err != nil {
		t.Fatal(err)
	}
	for hop := 1; hop <= 256; hop++ {
		var emitted string
		emitted, pending = RouteRetCtx(pending, false)
		var expected []ReturnedCheckpoint
		for _, cp := range want {
			if int(*cp.ReverseTTL)+1 == hop {
				zero := byte(0)
				cp.ReverseTTL = &zero
				expected = append(expected, cp)
			}
		}
		if len(expected) == 0 {
			if emitted != "" {
				t.Fatalf("hop %d emitted prematurely", hop)
			}
			continue
		}
		got, err := DecodeReturnedCheckpoints(emitted)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatalf("hop %d: got %+v want %+v: %v", hop, got, expected, err)
		}
		fp, _, _, _, _ := DecodeRetCtx(emitted)
		var ids []string
		for _, cp := range expected {
			ids = append(ids, cp.SpanID.String())
		}
		if fp != strings.Join(ids, ",") {
			t.Fatalf("hop %d retained another partition's fingerprint: %s", hop, fp)
		}
	}
	if pending != "" {
		t.Fatal("TTL 255 did not expire on the 256th upstream span")
	}
	got, _ := DecodeReturnedCheckpoints(original)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("routing mutated the input bundle")
	}
}

func TestReverseTTLOriginalCheckpointAndLegacy(t *testing.T) {
	legacy := EncodeCheckpointRetCtx(trace.SpanID{1}, 130, SegPathCheckpoint, []byte{1, 2})
	current := EncodeCheckpointRetCtxWithTTL(trace.SpanID{2}, 131, SegPathCheckpoint, []byte{3, 4}, 255)
	mixed := MergeRetCtx(legacy, current)
	emitted, pending := RouteRetCtx(mixed, false)
	if emitted != "" {
		t.Fatal("legacy or nonexpired truss was emitted at an ordinary span")
	}
	cps, err := DecodeReturnedCheckpoints(pending)
	if err != nil || len(cps) != 2 || cps[0].ReverseTTL != nil || cps[1].ReverseTTL == nil || *cps[1].ReverseTTL != 254 {
		t.Fatalf("mixed countdown: %+v %v", cps, err)
	}
	for _, input := range []string{pending, "opaque-legacy-context", encodeTruss(trussData{FP: "legacy-fingerprint"})} {
		emitted, forwarded := RouteRetCtx(input, true)
		if emitted != input || forwarded != "" {
			t.Fatal("original checkpoint failed to absorb the entire input")
		}
	}
	for _, input := range []string{"opaque-legacy-context", encodeTruss(trussData{FP: "legacy-fingerprint"})} {
		emitted, forwarded := RouteRetCtx(input, false)
		if emitted != "" || forwarded != input {
			t.Fatal("ordinary span lost an opaque context")
		}
	}
}

func TestReverseTTLRejectsInvalidByte(t *testing.T) {
	for _, value := range []string{"-1", "256", "1.5", `"1"`} {
		input := base64.StdEncoding.EncodeToString([]byte(`{"segs":[{"k":"checkpoint.pb","ttl":` + value + `}]}`))
		if _, err := DecodeReturnedCheckpoints(input); err == nil {
			t.Fatalf("accepted invalid byte TTL: %s", value)
		}
		if emitted, pending := RouteRetCtx(input, false); emitted != "" || pending != input {
			t.Fatal("routing discarded malformed context instead of preserving it")
		}
	}
}

func TestReverseDecisionSkipsInvalidAndUnknownSegments(t *testing.T) {
	valid, _ := decodeTruss(EncodeCheckpointRetCtx(trace.SpanID{1}, 130, SegPathCheckpoint, []byte{1, 2}))
	valid.Segs = append(valid.Segs,
		TrussSegment{Kind: SegAMQ, Data: []byte{3}},
		TrussSegment{Kind: SegStructuralCheckpoint, Data: []byte{4}},
	)
	input := encodeTruss(valid)
	for _, accept := range []bool{false, true} {
		calls := 0
		emitted, pending := RouteRetCtxWithDecision(input, false, func(cp ReturnedCheckpoint) bool {
			calls++
			if cp.SpanID != (trace.SpanID{1}) || cp.Depth != 130 {
				t.Fatal("probability callback received malformed location")
			}
			return accept
		})
		if calls != 1 {
			t.Fatalf("expected one valid truss decision, got %d", calls)
		}
		if !accept {
			if emitted != "" || pending != input {
				t.Fatal("failed probability trial rewrote an unchanged carrier")
			}
		} else {
			cps, err := DecodeReturnedCheckpoints(emitted)
			remaining, ok := decodeTruss(pending)
			if err != nil || len(cps) != 1 || !ok || len(remaining.Segs) != 2 {
				t.Fatal("routing lost unknown or malformed segments")
			}
		}
	}
}
