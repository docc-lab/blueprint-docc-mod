package otelcol

// Tomislav-RetCtx: the allocation-free Lehmer packing and the raw (undecoded) handling of inherited
// S-Bridge tails (structural_truss.go) must produce exactly the bytes, and accept exactly the
// payloads, that the decode-and-re-encode code did.

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/rand"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// refPackEndGroup is packEndGroup as it was before the in-place bitmap and appendLehmerSmall.
func refPackEndGroup(out []byte, ends []int, bound int) []byte {
	if len(ends) == 0 {
		return append(out, 0)
	}
	if lehmerEnabled && lehmerApplicable(ends, bound) {
		out = binary.AppendUvarint(out, uint64(len(ends))<<1|1)
		bitmap := make([]byte, (bound+7)/8)
		for _, e := range ends {
			bitmap[(e-1)/8] |= 1 << uint((e-1)%8)
		}
		out = append(out, bitmap...)
		return packLehmer(out, lehmerDigits(ends))
	}
	out = binary.AppendUvarint(out, uint64(len(ends))<<1)
	for _, e := range ends {
		out = binary.AppendUvarint(out, uint64(maxInt(e, 0)))
	}
	return out
}

// randomEnds returns an end-event group for bound: usually a distinct subset of 1..bound in random
// order (the Lehmer case), sometimes values a plain list has to carry (repeats, out of range).
func randomEnds(r *rand.Rand, bound int) []int {
	if r.Intn(10) == 0 {
		ends := make([]int, r.Intn(4))
		for i := range ends {
			ends[i] = r.Intn(bound+3) - 1
		}
		return ends
	}
	if bound <= 0 {
		return nil
	}
	perm := r.Perm(bound)
	ends := make([]int, r.Intn(bound+1))
	for i := range ends {
		ends[i] = perm[i] + 1
	}
	return ends
}

func randomTail(r *rand.Rand) structuralTail {
	var tail structuralTail
	for i, n := 0, r.Intn(6); i < n; i++ {
		ordinal := 1 + r.Intn(9)
		if r.Intn(8) == 0 {
			ordinal = 60 + r.Intn(20) // bounds above 64: the general Lehmer path
		}
		tail.ordinals = append(tail.ordinals, ordinal)
		tail.endEvents = append(tail.endEvents, randomEnds(r, ordinal-1))
	}
	for i, n := 0, r.Intn(3); i < n; i++ {
		var d structuralDelayed
		r.Read(d.traceID[:])
		r.Read(d.parent[:])
		d.children = r.Intn(12)
		d.ends = randomEnds(r, d.children)
		tail.delayed = append(tail.delayed, d)
	}
	return tail
}

func TestLehmerWithoutAllocationsMatches(t *testing.T) {
	for n := 0; n < 200; n++ {
		chunks := mixedRadixChunks(n)
		var got [][2]int
		for start, end := 0, 0; start < n; start = end {
			end, _ = mixedRadixChunkEnd(n, start)
			got = append(got, [2]int{start, end})
		}
		if len(got) != len(chunks) || (len(got) > 0 && !equalChunks(got, chunks)) {
			t.Fatalf("n=%d: chunks %v, want %v", n, got, chunks)
		}
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		bound := 1 + r.Intn(80)
		ends := randomEnds(r, bound)
		want := refPackEndGroup(nil, ends, bound)
		if got := packEndGroup(nil, ends, bound); !bytes.Equal(got, want) {
			t.Fatalf("bound %d ends %v: %x, want %x", bound, ends, got, want)
		}
	}
	ends := []int{3, 1, 4, 2, 7}
	if n := testing.AllocsPerRun(100, func() { packEndGroup(make([]byte, 0, 64), ends, 8) }); n > 1 {
		t.Errorf("packEndGroup allocates %.0f times for a small group, want only the output buffer", n)
	}
}

func equalChunks(a, b [][2]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScanAcceptsWhatUnpackAccepts(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 20000; i++ {
		tail := randomTail(r)
		distance := 1 + r.Intn(6)
		bloomBytes := make([]byte, checkpointBlooms[distance-1].bytes)
		r.Read(bloomBytes)
		ha := make([]byte, r.Intn(30))
		r.Read(ha)
		payload := packStructuralBR(r.Intn(20), [8]byte{byte(i)}, distance, bloomBytes, ha, tail)
		if i%2 == 1 { // corrupt it: truncate, or change a byte
			if r.Intn(2) == 0 {
				payload = payload[:r.Intn(len(payload))]
			} else {
				payload[r.Intn(len(payload))] ^= byte(1 + r.Intn(255))
			}
		}
		d1, c1, dist1, b1, h1, decoded, ok1 := unpackStructuralBR(payload)
		d2, c2, dist2, b2, h2, raw, ok2 := scanStructuralBR(payload)
		if ok1 != ok2 {
			t.Fatalf("case %d: unpack ok=%v, scan ok=%v for %x", i, ok1, ok2, payload)
		}
		if !ok1 {
			continue
		}
		if d1 != d2 || c1 != c2 || dist1 != dist2 || !bytes.Equal(b1, b2) || !bytes.Equal(h1, h2) {
			t.Fatalf("case %d: window core differs", i)
		}
		if raw.n != uint64(len(decoded.ordinals)) || raw.m != uint64(len(decoded.delayed)) {
			t.Fatalf("case %d: counts %d/%d, want %d/%d", i, raw.n, raw.m, len(decoded.ordinals), len(decoded.delayed))
		}
	}
}

func TestChildTailMatchesRepack(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 20000; i++ {
		parent := randomTail(r)
		packed := packStructuralTail(nil, parent)
		raw, ok := scanStructuralTail(packed)
		if !ok {
			t.Fatalf("case %d: scan rejected a packed tail", i)
		}
		ordinal := 1 + r.Intn(9)
		ends := randomEnds(r, ordinal-1)
		var drained []structuralDelayed
		for j, n := 0, r.Intn(3); j < n; j++ {
			drained = append(drained, randomTail(r).delayed...)
		}
		// What OnStart used to do: decode the parent's tail, extend it, encode it again.
		decoded, ok := unpackStructuralTail(packed)
		if !ok {
			t.Fatalf("case %d: unpack rejected a packed tail", i)
		}
		want := packStructuralTail(nil, structuralTail{
			ordinals:  append(append([]int(nil), decoded.ordinals...), ordinal),
			endEvents: append(append([][]int(nil), decoded.endEvents...), ends),
			delayed:   append(append([]structuralDelayed(nil), decoded.delayed...), drained...),
		})
		if got := appendChildTail(nil, raw, ordinal, ends, drained); !bytes.Equal(got, want) {
			t.Fatalf("case %d:\n got %x\nwant %x", i, got, want)
		}
	}
}

// refStructuralWindow is SB's OnStart as it was (commit 7abfa16e): decode the parent's payload,
// extend its tail, encode the export payload and the propagation separately, wrap the TTL.  In
// range mode the checkpoint decision and outgoing TTL are drawn at random, so they are passed in
// (taken from the span's actual wire state).
func refStructuralWindow(p *StructuralBridgeProcessor, br string, seqNum int, ends []int, parentID trace.SpanID,
	drained []structuralDelayed, sid trace.SpanID, isCheckpoint bool, outgoingTTL byte) (emit []byte, prop string, depth int) {
	var (
		hasParent   bool
		parentTTL   byte
		parentDepth int
		anchor      [8]byte
		distance    int
		inherited   []byte
		parentHA    []byte
		tail        structuralTail
	)
	if raw, valid := parsePropBR(br); valid {
		parentTTL, raw = p.checkpointRange.unwrap(raw)
		var ok bool
		parentDepth, anchor, distance, inherited, parentHA, tail, ok = unpackStructuralBR(raw)
		hasParent = ok && (!p.checkpointRange.enabled() || int(parentTTL) < distance)
	}
	cpd := int(p.checkpointDistance)
	if cpd < 1 {
		cpd = 1
	}
	if hasParent {
		depth = parentDepth + 1
	}
	if !p.checkpointRange.enabled() {
		isCheckpoint, distance = depth%cpd == 0, min(cpd, 256)
	}
	if !hasParent {
		anchor = [8]byte{}
		if p.checkpointRange.enabled() {
			distance = int(outgoingTTL) + 1
		}
		inherited, parentHA, tail = nil, nil, structuralTail{}
	}
	geometry := checkpointBlooms[distance-1]
	filter := bloom.NewFromBytes(inherited, geometry.m, geometry.k)
	var ha []byte
	if hasParent && seqNum == 1 {
		ha = append([]byte(nil), parentHA...)
	} else if hasParent && seqNum == 2 {
		ha = haAppendEntry(nil, parentID.String(), depth)
	}
	emitTail := structuralTail{
		ordinals:  append(append([]int(nil), tail.ordinals...), seqNum),
		endEvents: append(append([][]int(nil), tail.endEvents...), ends),
		delayed:   append(tail.delayed, drained...),
	}
	emit = packStructuralBR(depth, anchor, distance, filter.Bytes(), ha, emitTail)
	propTail := emitTail
	if isCheckpoint {
		anchor = [8]byte(sid)
		if p.checkpointRange.enabled() {
			distance = int(outgoingTTL) + 1
			geometry = checkpointBlooms[distance-1]
		}
		filter = bloom.New(geometry.m, geometry.k)
		ha = nil
		propTail = structuralTail{}
	} else {
		filter.AddPrehashed(sid[:])
	}
	propagation := packStructuralBR(depth, anchor, distance, filter.Bytes(), ha, propTail)
	return emit, propBR(p.checkpointRange.wrap(outgoingTTL, propagation)), depth
}

// The new OnStart (structural inputs, raw tails, one pass) stores exactly what the old one did.
func TestStructuralOnStartMatchesOld(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for _, mode := range []struct {
		name string
		cpd  int64
		rng  checkpointRange
	}{{"fixed-3", 3, checkpointRange{}}, {"range-2-6", 6, checkpointRange{2, 6}}, {"range-3-3", 3, checkpointRange{3, 3}}} {
		t.Run(mode.name, func(t *testing.T) {
			p := &StructuralBridgeProcessor{checkpointDistance: mode.cpd, checkpointRange: mode.rng}
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p))
			for i := 0; i < 3000; i++ {
				ctx := context.Background()
				br := ""
				if r.Intn(10) != 0 { // a parent window, sometimes past its TTL
					distance := 1 + r.Intn(6)
					bloomBytes := make([]byte, checkpointBlooms[distance-1].bytes)
					r.Read(bloomBytes)
					ha := make([]byte, r.Intn(20))
					r.Read(ha)
					payload := packStructuralBR(r.Intn(15), [8]byte{byte(i), 1}, distance, bloomBytes, ha, randomTail(r))
					br = propBR(mode.rng.wrap(byte(r.Intn(distance+1)), payload))
				}
				ctx = backend.SetBaggageInContext(ctx, map[string]string{BaggageBRKey: br})
				parentID := trace.SpanID{9, byte(i)}
				ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
					TraceID: trace.TraceID{1}, SpanID: parentID, TraceFlags: trace.FlagsSampled}))
				seqNum := 1 + r.Intn(5)
				ctx = context.WithValue(ctx, "seqNum", seqNum)
				ends := randomEnds(r, seqNum-1)
				endsCopy := append([]int(nil), ends...)
				ctx = context.WithValue(ctx, "endEvents", &endsCopy)
				ctx = context.WithValue(ctx, "childrenMutex", &sync.Mutex{})
				var drained []structuralDelayed
				for j, n := 0, r.Intn(3); j < n; j++ {
					drained = append(drained, randomTail(r).delayed...)
				}
				for _, d := range drained {
					p.delayedQueue() <- d
				}
				_, span := tp.Tracer("t").Start(ctx, "child")
				sid := span.SpanContext().SpanID()
				w := bridgeWires.take(sid)
				if w == nil {
					t.Fatal("no wire state stored")
				}
				var outgoingTTL byte
				if mode.rng.enabled() {
					outgoingTTL = w.prop[0]
				}
				if len(ends) == 0 {
					ends = nil // the old code copied only a non-empty list
				}
				emit, prop, depth := refStructuralWindow(p, br, seqNum, ends, parentID, drained, sid, w.priority, outgoingTTL)
				if !bytes.Equal(w.emit, emit) || w.prop != prop || w.depth != depth || w.ordinal != seqNum {
					t.Fatalf("case %d: new OnStart differs\n emit %x\n want %x\n prop %x\n want %x\n depth %d/%d",
						i, w.emit, emit, w.prop, prop, w.depth, depth)
				}
			}
		})
	}
}
