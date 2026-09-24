package otelcol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"math/rand/v2"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func permutations(values []int) [][]int {
	if len(values) <= 1 {
		return [][]int{append([]int(nil), values...)}
	}
	var out [][]int
	for i, v := range values {
		rest := append(append([]int(nil), values[:i]...), values[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]int{v}, p...))
		}
	}
	return out
}

func TestLehmerRoundTripSmall(t *testing.T) {
	for n := 0; n <= 6; n++ {
		values := make([]int, n)
		for i := range values {
			values[i] = i + 1
		}
		seen := map[string]bool{}
		for _, perm := range permutations(values) {
			packed := packLehmer(nil, lehmerDigits(perm))
			key := string(packed)
			if seen[key] {
				t.Fatalf("n=%d: two permutations share code %x", n, packed)
			}
			seen[key] = true
			digits, rest, ok := unpackLehmer(packed, n)
			if !ok || len(rest) != 0 {
				t.Fatalf("n=%d: decode failed for %v", n, perm)
			}
			back, ok := lehmerPermutation(values, digits)
			if !ok || len(back) != len(perm) || (n > 0 && !reflect.DeepEqual(back, perm)) {
				t.Fatalf("n=%d: %v decoded as %v", n, perm, back)
			}
		}
		if n > 0 && len(seen) != len(permutations(values)) {
			t.Fatal("codes not unique")
		}
	}
	if !lehmerEnabled {
		t.Skip("built with -tags sb_nolehmer")
	}
	// Six elements: rank < 6! = 720 fits two varint bytes; one byte bitmap.
	group := packEndGroup(nil, []int{6, 1, 4, 2, 5, 3}, 6)
	if len(group) != 1+1+2 {
		t.Fatalf("six ended siblings should pack into 4 bytes, got %d", len(group))
	}
}

func TestLehmerChunkingLargePermutations(t *testing.T) {
	if !lehmerEnabled {
		t.Skip("built with -tags sb_nolehmer")
	}
	r := rand.New(rand.NewPCG(7, 11))
	for _, n := range []int{20, 21, 45, 130} {
		values := make([]int, n)
		for i := range values {
			values[i] = i + 1
		}
		perm := append([]int(nil), values...)
		r.Shuffle(n, func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		chunks := mixedRadixChunks(n)
		if chunks[0][0] != 0 || chunks[len(chunks)-1][1] != n {
			t.Fatalf("chunks do not cover %d positions: %v", n, chunks)
		}
		for _, c := range chunks {
			product := uint64(1)
			for i := c[0]; i < c[1]; i++ {
				product *= uint64(n - i)
			}
			if product > mixedRadixLimit {
				t.Fatalf("chunk %v overflows: %d", c, product)
			}
		}
		packed := packEndGroup(nil, perm, n)
		ends, rest, ok := unpackEndGroup(packed, n)
		if !ok || len(rest) != 0 || !reflect.DeepEqual(ends, perm) {
			t.Fatalf("n=%d round trip failed: %v", n, ok)
		}
		plain := 1
		for _, e := range perm {
			plain += varintLen(e)
		}
		if len(packed) >= plain {
			t.Fatalf("n=%d: Lehmer group (%d bytes) is not smaller than plain (%d)", n, len(packed), plain)
		}
	}
}

func TestEndGroupFallbacks(t *testing.T) {
	for _, tc := range []struct {
		ends  []int
		bound int
	}{
		{nil, 0}, {nil, 5}, {[]int{1}, 1}, {[]int{3, 1}, 2}, // 3 exceeds bound -> plain
		{[]int{2, 2}, 4},    // duplicate -> plain
		{[]int{0, 1}, 4},    // zero ordinal -> plain
		{[]int{5, 3, 1}, 5}, // lehmer
	} {
		packed := packEndGroup(nil, tc.ends, tc.bound)
		ends, rest, ok := unpackEndGroup(packed, tc.bound)
		if !ok || len(rest) != 0 {
			t.Fatalf("%v/%d: decode failed", tc.ends, tc.bound)
		}
		if len(ends) != len(tc.ends) || (len(ends) > 0 && !reflect.DeepEqual(ends, tc.ends)) {
			t.Fatalf("%v/%d: decoded %v", tc.ends, tc.bound, ends)
		}
	}
	// A group longer than its bound must not be decoded as Lehmer.
	if _, _, ok := unpackEndGroup([]byte{5<<1 | 1, 0xFF}, 2); ok {
		t.Fatal("accepted impossible Lehmer group")
	}
	if _, _, ok := unpackEndGroup([]byte{1 << 1}, 4); ok {
		t.Fatal("accepted truncated plain group")
	}
}

func TestStructuralPayloadRoundTrip(t *testing.T) {
	var anchor [8]byte
	binary.BigEndian.PutUint64(anchor[:], 0xABCDEF0123456789)
	for _, distance := range []int{1, 2, 6, 256} {
		g := checkpointBlooms[distance-1]
		filter := bloom.New(g.m, g.k)
		filter.AddPrehashed([]byte{1, 2, 3, 4, 5, 6, 7, 8})
		ha := haAppendEntry(nil, "00000000000000aa", 4)
		ha = haAppendEntry(ha, "00000000000000bb", 7)
		var tid [16]byte
		tid[0], tid[15] = 9, 9
		tail := structuralTail{
			ordinals:  []int{0, 3, 1, 2},
			endEvents: [][]int{nil, {2, 1}, nil, {1}},
			delayed:   []structuralDelayed{{traceID: tid, parent: anchor, children: 5, ends: []int{4, 2, 1}}, {children: 1}},
		}
		packed := packStructuralBR(17, anchor, distance, filter.Bytes(), ha, tail)
		depth, ckpt, dist, bb, ha2, tail2, ok := unpackStructuralBR(packed)
		if !ok || depth != 17 || ckpt != anchor || dist != distance || !bytes.Equal(bb, filter.Bytes()) || !bytes.Equal(ha2, ha) {
			t.Fatalf("distance %d: core corrupted (%v)", distance, ok)
		}
		if !reflect.DeepEqual(tail2.ordinals, tail.ordinals) || len(tail2.endEvents) != 4 ||
			!reflect.DeepEqual(tail2.endEvents[1], []int{2, 1}) || len(tail2.endEvents[0]) != 0 ||
			!reflect.DeepEqual(tail2.endEvents[3], []int{1}) || !reflect.DeepEqual(tail2.delayed, tail.delayed) {
			t.Fatalf("distance %d: tail corrupted: %+v", distance, tail2)
		}
		// The CG window core is a byte-compatible prefix.
		d2, a2, dist2, bb2, _, valid := unpackCheckpointWindowBR(packed)
		if !valid || d2 != 17 || a2 != anchor || dist2 != distance || !bytes.Equal(bb2, filter.Bytes()) {
			t.Fatal("SB payload lost CG core compatibility")
		}
		// Truncations never panic and are rejected.
		for cut := 0; cut < len(packed); cut++ {
			if _, _, _, _, _, _, ok := unpackStructuralBR(packed[:cut]); ok {
				t.Fatalf("accepted truncated payload at %d/%d", cut, len(packed))
			}
		}
	}
	if _, ok := structuralDelayedFromServer([16]byte{}, [8]byte{}, []byte{3}); ok {
		t.Fatal("accepted truncated server summary")
	}
	d, ok := structuralDelayedFromServer([16]byte{1}, [8]byte{2}, []byte{4, 2, 3, 1})
	if !ok || d.children != 4 || !reflect.DeepEqual(d.ends, []int{3, 1}) || d.traceID[0] != 1 || d.parent[0] != 2 {
		t.Fatalf("server summary decoded wrongly: %+v", d)
	}
}

// sbParentContext mimics the SB server wrapper: baggage from the parent span
// plus the per-request child counter, end-event list and mutex, and the
// client wrapper's start ordinal for the next child.
type sbRequestState struct {
	children atomic.Uint64
	ends     []int
	mu       sync.Mutex
}

func sbChildContext(parentSpan trace.Span, state *sbRequestState) context.Context {
	ctx := trace.ContextWithSpanContext(context.Background(), parentSpan.SpanContext())
	ctx = backend.SetBaggageInContext(ctx, map[string]string{BaggageBRKey: checkpointSpanAttribute(parentSpan, AttrBR)})
	ctx = context.WithValue(ctx, "childCount", &state.children)
	ctx = context.WithValue(ctx, "endEvents", &state.ends)
	ctx = context.WithValue(ctx, "childrenMutex", &state.mu)
	return context.WithValue(ctx, "seqNum", int(state.children.Add(1)))
}

func TestStructuralBridgeFanOutTrusses(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	tp, snapshot := ttlTestProvider(t, "sb", checkpointRange{4, 4})
	tracer := tp.Tracer("sb")
	_, root := tracer.Start(context.Background(), "root", trace.WithSpanKind(trace.SpanKindServer))
	root.SetAttributes(attribute.Bool("hasChildren", true))
	state := &sbRequestState{}
	// Three children start in order 1,2,3; child 2 ends before child 3 starts.
	_, c1 := tracer.Start(sbChildContext(root, state), "c1", trace.WithSpanKind(trace.SpanKindClient))
	_, c2 := tracer.Start(sbChildContext(root, state), "c2", trace.WithSpanKind(trace.SpanKindClient))
	// Tomislav-RetCtx: the wire state is freed at OnEnd; read c2's payload first.
	c2Emit := checkpointSpanAttribute(c2, AttrBREmit)
	c2.End()
	state.mu.Lock()
	state.ends = append(state.ends, 2)
	state.mu.Unlock()
	ctx3 := sbChildContext(root, state)
	_, c3 := tracer.Start(ctx3, "c3", trace.WithSpanKind(trace.SpanKindClient))

	decode := func(span trace.Span, key attribute.Key) (int, [8]byte, int, []byte, []byte, structuralTail) {
		encoded := checkpointSpanAttribute(span, key)
		if span == c2 && key == AttrBREmit {
			encoded = c2Emit
		}
		raw, ok := decodeBR(encoded)
		if !ok {
			t.Fatalf("missing %s", key)
		}
		if key == AttrBR {
			raw = raw[1:] // forward TTL byte
		}
		depth, ckpt, distance, bb, ha, tail, ok := unpackStructuralBR(raw)
		if !ok {
			t.Fatalf("%s does not decode", key)
		}
		return depth, ckpt, distance, bb, ha, tail
	}
	// c1: first child inherits an empty HA and empty end events.
	depth, ckpt, distance, _, ha, tail := decode(c1, AttrBREmit)
	if depth != 1 || ckpt != [8]byte(root.SpanContext().SpanID()) || distance != 4 || len(ha) != 0 {
		t.Fatalf("c1 core wrong: depth %d distance %d ha %d", depth, distance, len(ha))
	}
	if !reflect.DeepEqual(tail.ordinals, []int{1}) || len(tail.endEvents) != 1 || len(tail.endEvents[0]) != 0 {
		t.Fatalf("c1 tail wrong: %+v", tail)
	}
	// c2: second child records the fan-out witness (root, depth 1).
	_, _, _, _, ha, tail = decode(c2, AttrBREmit)
	rootID := root.SpanContext().SpanID()
	if len(ha) != 9 || !bytes.Equal(ha[:8], rootID[:]) || ha[8] != 1 {
		t.Fatalf("c2 must witness the root fan-out: %x", ha)
	}
	if !reflect.DeepEqual(tail.ordinals, []int{2}) {
		t.Fatalf("c2 ordinal wrong: %+v", tail)
	}
	// c3: third child carries no witness, ordinal 3, and end event {2}.
	_, _, _, _, ha, tail = decode(c3, AttrBREmit)
	if len(ha) != 0 || !reflect.DeepEqual(tail.ordinals, []int{3}) || !reflect.DeepEqual(tail.endEvents[0], []int{2}) {
		t.Fatalf("c3 tail wrong: ha %x %+v", ha, tail)
	}
	// c3's propagation carries the same tail forward; its Bloom contains c3.
	_, _, _, bb, _, prop := decode(c3, AttrBR)
	g := checkpointBlooms[3]
	sid := c3.SpanContext().SpanID()
	if !bloom.NewFromBytes(bb, g.m, g.k).TestPrehashed(sid[:]) || !reflect.DeepEqual(prop.ordinals, []int{3}) {
		t.Fatal("c3 propagation lost its own span or ordinal")
	}
	// A grandchild of c3 extends the window: ordinals [3, 1].
	gstate := &sbRequestState{}
	_, g1 := tracer.Start(sbChildContext(c3, gstate), "g1", trace.WithSpanKind(trace.SpanKindClient))
	_, _, _, _, _, tail = decode(g1, AttrBREmit)
	if !reflect.DeepEqual(tail.ordinals, []int{3, 1}) || !reflect.DeepEqual(tail.endEvents[0], []int{2}) || len(tail.endEvents[1]) != 0 {
		t.Fatalf("grandchild tail wrong: %+v", tail)
	}

	// Server end: children 3, ends in order [2, 1, 3] -> summary keeps [2, 1].
	summary := binary.AppendUvarint(nil, 3)
	summary = binary.AppendUvarint(summary, 2)
	summary = binary.AppendUvarint(summary, 2)
	summary = binary.AppendUvarint(summary, 1)
	root.SetAttributes(attribute.String("remEndEvents", base64.RawURLEncoding.EncodeToString(summary)))
	root.End()
	// The next outgoing span of this process carries the delayed truss.
	_, next := tracer.Start(context.Background(), "next-request", trace.WithSpanKind(trace.SpanKindServer))
	_, _, _, _, _, tail = decode(next, AttrBREmit)
	if len(tail.delayed) != 1 || tail.delayed[0].traceID != [16]byte(root.SpanContext().TraceID()) ||
		tail.delayed[0].parent != [8]byte(root.SpanContext().SpanID()) || tail.delayed[0].children != 3 ||
		!reflect.DeepEqual(tail.delayed[0].ends, []int{2, 1}) {
		t.Fatalf("delayed truss missing or wrong: %+v", tail.delayed)
	}
	// A root is a checkpoint: it persisted the delayed truss and propagates none.
	_, _, _, _, _, prop = decode(next, AttrBR)
	if len(prop.delayed) != 0 || len(prop.ordinals) != 0 {
		t.Fatalf("checkpoint propagated captured trusses: %+v", prop)
	}
	next.End()
	c1.End()
	c3.End()
	g1.End()
	hp, lp := snapshot()
	// Roots are checkpoints (depth 0); the interior clients and g1 are ordinary;
	// c2 was a childless client, not a server leaf, so it is ordinary too.
	if len(hp) != 2 || len(lp) != 4 {
		t.Fatalf("classification: %d checkpoints, %d ordinary", len(hp), len(lp))
	}
	for _, entry := range lp {
		var hasO, hasD, hasBR bool
		for _, attr := range entry.span.Attributes {
			switch attr.Key {
			case AttrO:
				hasO = true
			case AttrD:
				hasD = true
			case AttrBREmit:
				hasBR = true
			}
		}
		if !hasO || !hasD || hasBR {
			t.Fatalf("ordinary span wire attributes wrong: o=%v d=%v br=%v", hasO, hasD, hasBR)
		}
	}
}

func TestStructuralBridgeFixedDistance(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	p := &StructuralBridgeProcessor{checkpointDistance: 3}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p))
	ctx := context.Background()
	var anchor [8]byte
	for depth := 0; depth < 7; depth++ {
		_, span := tp.Tracer("fixed").Start(ctx, "span", trace.WithSpanKind(trace.SpanKindServer))
		span.SetAttributes(attribute.Bool("hasChildren", true))
		raw, _ := decodeBR(checkpointSpanAttribute(span, AttrBR))
		d, ckpt, distance, _, _, tail, ok := unpackStructuralBR(raw) // no TTL byte in fixed mode
		if !ok || d != depth || distance != 3 {
			t.Fatalf("fixed-distance payload wrong at %d: %v %d %d", depth, ok, d, distance)
		}
		if depth%3 == 0 {
			if ckpt != [8]byte(span.SpanContext().SpanID()) || len(tail.ordinals) != 0 {
				t.Fatalf("checkpoint at %d did not re-anchor", depth)
			}
			anchor = ckpt
		} else if ckpt != anchor || len(tail.ordinals) != depth%3 {
			t.Fatalf("ordinary span at %d lost anchor or ordinals: %+v", depth, tail)
		}
		ctx = backend.SetBaggageInContext(trace.ContextWithSpanContext(context.Background(), span.SpanContext()),
			map[string]string{BaggageBRKey: checkpointSpanAttribute(span, AttrBR)})
		span.End()
	}
	if len(p.hpBuf) != 3 || len(p.lpBuf) != 4 {
		t.Fatalf("fixed distance classification: %d/%d", len(p.hpBuf), len(p.lpBuf))
	}
}
