package otelcol

import (
	"context"
	"encoding/binary"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type checkpointBloomGeometry struct {
	m     uint64
	k     uint
	bytes int
}

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
	geometry := checkpointBlooms[distance-1]
	filter := bloom.NewFromBytes(inherited, geometry.m, geometry.k)
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
	emit := packCheckpointWindowBR(depth, anchor, distance, filter.Bytes(), ha)
	priority := 0
	sid := s.SpanContext().SpanID()
	if isCheckpoint {
		priority = 1
		anchor = [8]byte(sid)
		distance = int(outgoingTTL) + 1
		geometry = checkpointBlooms[distance-1]
		filter = bloom.New(geometry.m, geometry.k)
		ha = nil
	} else {
		filter.AddPrehashed(sid[:])
	}
	propagation := packCheckpointWindowBR(depth, anchor, distance, filter.Bytes(), ha)
	s.SetAttributes(
		attribute.Int(AttrBagPrio, priority),
		attribute.String(AttrBR, encodeBR(r.wrap(outgoingTTL, propagation))),
		attribute.String(AttrBREmit, encodeBR(emit)),
		attribute.String(AttrD, encodeBR(varintEncode(depth))),
		attribute.Int("depth", depth),
	)
}
