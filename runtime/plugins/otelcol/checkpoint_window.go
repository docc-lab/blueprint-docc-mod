package otelcol

import (
	"context"
	"encoding/binary"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type checkpointBloomGeometry struct {
	m     uint64
	k     uint
	bytes int
}

func (g checkpointBloomGeometry) emptyBytes() []byte { return make([]byte, g.bytes) }

// Tomislav-RetCtx: immutable geometry for every encodable distance. Spans select an entry for
// their own window; concurrent windows never resize a shared filter.
var checkpointBlooms = func() [256]checkpointBloomGeometry {
	var geometries [256]checkpointBloomGeometry
	for i := range geometries {
		m, k := bloom.EstimateParameters(uint(pbBloomCapacity(i+1)), DefaultBloomFPRate)
		geometries[i] = checkpointBloomGeometry{m, k, int((m + 7) / 8)}
	}
	return geometries
}()

// Tomislav-RetCtx: range-mode PB/CGPB truss:
//
//	varint(absolute depth) || checkpoint(8) || byte(window CPD - 1) || bloom || HA
//
// PB has no HA. The immutable window distance determines the exact Bloom m/k
// and byte width, so CGPB can locate its trailing HA without a deployment-wide
// size. Forward baggage adds the mutable TTL byte before this entire payload.
// An emitted checkpoint describes the INCOMING window; its outgoing baggage
// describes the NEW window. A root uses its first selected distance for both.
func packCheckpointWindowBR(depth int, ckpt [8]byte, distance int, bloomBytes, ha []byte) []byte {
	out := make([]byte, 0, varintLen(depth)+9+len(bloomBytes)+len(ha))
	out = binary.AppendUvarint(out, uint64(depth))
	out = append(out, ckpt[:]...)
	out = append(out, byte(distance-1))
	out = append(out, bloomBytes...)
	return append(out, ha...)
}

func unpackCheckpointWindowBR(raw []byte) (depth int, ckpt [8]byte, distance int, bloomBytes, ha []byte, ok bool) {
	d, n := binary.Uvarint(raw)
	if n <= 0 || d > uint64(^uint(0)>>1) || len(raw)-n < 9 {
		return
	}
	copy(ckpt[:], raw[n:n+8])
	distance = int(raw[n+8]) + 1
	geometry := checkpointBlooms[distance-1]
	rest := raw[n+9:]
	if len(rest) < geometry.bytes {
		return 0, ckpt, 0, nil, nil, false
	}
	return int(d), ckpt, distance, rest[:geometry.bytes], rest[geometry.bytes:], true
}

// onStartCheckpointWindow is shared by ranged PB and CGPB. The existing fixed
// distance processors retain their legacy format and classification behavior.
func onStartCheckpointWindow(parent context.Context, s sdktrace.ReadWriteSpan, r checkpointRange, callGraph bool) {
	var (
		hasParent   bool
		parentTTL   byte
		parentDepth int
		anchor      [8]byte
		distance    int
		inherited   []byte
		parentHA    []byte
	)
	if bag := backend.GetBaggageFromContext(parent); bag != nil {
		if raw, valid := decodeBR(bag[BaggageBRKey]); valid {
			parentTTL, raw = r.unwrap(raw)
			var ok bool
			parentDepth, anchor, distance, inherited, parentHA, ok = unpackCheckpointWindowBR(raw)
			hasParent = ok && int(parentTTL) < distance && (callGraph || len(parentHA) == 0)
		}
	}

	isCheckpoint, outgoingTTL := r.next(parentTTL, hasParent)
	depth := 0
	if hasParent {
		depth = parentDepth + 1
	} else {
		anchor = [8]byte{}
		distance = int(outgoingTTL) + 1
		inherited, parentHA = nil, nil
	}
	var ha []byte
	if callGraph && hasParent {
		seq, _ := parent.Value("seqNum").(int)
		if seq == 1 {
			ha = append([]byte(nil), parentHA...)
		} else if seq == 2 {
			ha = haAppendEntry(nil, trace.SpanFromContext(parent).SpanContext().SpanID().String(), depth)
		}
	}

	// Retain the old window and its geometry for OnEnd and reverse rejection.
	sid := s.SpanContext().SpanID()
	emit, wrapped := encodeWindowPayloads(r, depth, anchor, distance, inherited, ha, isCheckpoint, outgoingTTL, sid)
	// Tomislav-RetCtx: nothing goes on the OTel span. The outgoing propagation reaches the RPC
	// wrappers through backend.AppendSpanBaggage; the export payload, depth and scheduling
	// decision stay beside the span as raw bytes (wire_state.go).
	bridgeWires.store(sid, &bridgeWire{emit: emit, depth: depth, priority: isCheckpoint, prop: encodeBR(wrapped)})
}

// encodeWindowPayloads builds the span's export payload (the window it inherited) and its
// outgoing propagation (TTL-wrapped in range mode): a checkpoint re-anchors on itself with an
// empty window of the newly drawn distance; any other span adds itself to the inherited window.
// Tomislav-RetCtx: both are written into one buffer, with the inherited Bloom bits copied once
// and this span's bits set in place -- the same bytes the filter-object path produced
// (packCheckpointWindowBR over NewFromBytes/AddPrehashed/Bytes and r.wrap), which
// checkpoint_window_encode_test.go checks against that path.
func encodeWindowPayloads(r checkpointRange, depth int, anchor [8]byte, distance int, inherited, ha []byte,
	isCheckpoint bool, outgoingTTL byte, sid trace.SpanID) (emit, wrapped []byte) {
	geometry := checkpointBlooms[distance-1]
	if len(inherited) != geometry.bytes { // NewFromBytes: anything but an exact-size window starts empty
		inherited = nil
	}
	propAnchor, propDistance, propGeometry, propInherited, propHA := anchor, distance, geometry, inherited, ha
	if isCheckpoint {
		propAnchor, propDistance, propHA, propInherited = [8]byte(sid), int(outgoingTTL)+1, nil, nil
		propGeometry = checkpointBlooms[propDistance-1]
	}
	head := varintLen(depth) + 9
	emitLen := head + geometry.bytes + len(ha)
	ttlLen := 0
	if r.enabled() {
		ttlLen = 1
	}
	buf := make([]byte, 0, emitLen+ttlLen+head+propGeometry.bytes+len(propHA))
	buf = appendWindowBR(buf, depth, anchor, distance, inherited, geometry.bytes, ha)
	emit = buf[:emitLen:emitLen]
	if ttlLen == 1 {
		buf = append(buf, outgoingTTL)
	}
	bloomAt := len(buf) + head
	buf = appendWindowBR(buf, depth, propAnchor, propDistance, propInherited, propGeometry.bytes, propHA)
	if !isCheckpoint {
		bloom.SetPrehashed(buf[bloomAt:bloomAt+geometry.bytes], geometry.m, geometry.k, sid[:])
	}
	return emit, buf[emitLen:]
}

// appendWindowBR appends the range-mode truss with bloomLen Bloom bytes: the given bits, or
// zeros when bits is nil.
func appendWindowBR(dst []byte, depth int, ckpt [8]byte, distance int, bits []byte, bloomLen int, ha []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(depth))
	dst = append(dst, ckpt[:]...)
	dst = append(dst, byte(distance-1))
	if bits != nil {
		dst = append(dst, bits...)
	} else {
		dst = append(dst, make([]byte, bloomLen)...)
	}
	return append(dst, ha...)
}
