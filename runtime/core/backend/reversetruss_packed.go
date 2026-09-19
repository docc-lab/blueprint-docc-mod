package backend

// Tomislav-RetCtx: the reverse path's wire codec, packed the same way the forward
// path packs `_br` -- explicit bytes and a single base64.RawURLEncoding, no JSON.
//
// The forward path has always done this (otelcol.encodeBR is one RawURLEncoding of
// already-packed varints). The reverse path built the same packed bytes and then
// handed them to encoding/json as a []byte field, so json base64'd the payload, the
// envelope was base64'd again, and every hop and fan-in paid a reflective marshal
// round-trip to append to a list or decrement one byte. This file removes that
// envelope; the payload bytes themselves are unchanged.
//
// Wire format (version 1):
//
//	envelope := 0x01 || record*
//	record   := tag || uvarint(len(body)) || body
//	tag      := kind(low 3 bits) | 0x08 when a reverse TTL is present
//	body     := [ttl] || data        (data == spanID(8) || uvarint(depth) || truss)
//
// `data` is byte-identical to what TrussSegment.Data has always held, so the
// segment payload and its interpretation are untouched -- only the container
// changes. Fields the checkpoint path never reads are simply not transmitted:
//
//   - fp: it is spanID.String(), and those same 8 bytes already head `data`.
//     It is derived on decode instead, so DecodeRetCtx still reports it.
//   - m, k: Bloom geometry belonging to legacy SegAMQ segments, which checkpoint
//     consumers ignore (they read the descriptor inside each embedded SDK truss).
//   - the segment kind as a 13-character string: it is a closed set of four, so
//     one byte carries it.
//
// Envelopes that carry anything else -- a legacy ancestry SegAMQ segment from
// BuildRetCtx, or a parent fingerprint -- keep the JSON envelope. Version 1 is
// therefore checkpoint-only by construction, and decodePackedRetCtx refuses
// anything it does not fully understand so a mixed bundle can never be
// silently truncated.

import (
	"encoding/base64"
	"encoding/binary"

	"go.opentelemetry.io/otel/trace"
)

const packedRetCtxVersion = 0x01

const (
	packedKindMask   = 0x07
	packedTTLPresent = 0x08
)

// Closed set: these are the only kinds version 1 may carry.
var packedKindID = map[string]byte{
	SegPathCheckpoint:       1,
	SegCallGraphCheckpoint:  2,
	SegStructuralCheckpoint: 3,
	SegVanillaCheckpoint:    4,
}

var packedKindName = map[byte]string{
	1: SegPathCheckpoint,
	2: SegCallGraphCheckpoint,
	3: SegStructuralCheckpoint,
	4: SegVanillaCheckpoint,
}

// packableRetCtx reports whether every segment fits version 1. A parent
// fingerprint has no packed representation, so it forces the JSON envelope.
func packableRetCtx(t trussData) bool {
	if t.Par != "" || len(t.Segs) == 0 {
		return false
	}
	for _, segment := range t.Segs {
		if _, ok := packedKindID[segment.Kind]; !ok {
			return false
		}
		if len(segment.Data) < 9 {
			return false
		}
	}
	return true
}

func appendPackedSegment(buf []byte, segment TrussSegment) []byte {
	tag := packedKindID[segment.Kind]
	body := len(segment.Data)
	if segment.ReverseTTL != nil {
		tag |= packedTTLPresent
		body++
	}
	buf = append(buf, tag)
	buf = binary.AppendUvarint(buf, uint64(body))
	if segment.ReverseTTL != nil {
		buf = append(buf, *segment.ReverseTTL)
	}
	return append(buf, segment.Data...)
}

func encodePackedRetCtx(segments []TrussSegment) string {
	buf := make([]byte, 0, 16*len(segments)+16)
	buf = append(buf, packedRetCtxVersion)
	for _, segment := range segments {
		buf = appendPackedSegment(buf, segment)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// packedRecords returns the record region of a packed envelope: everything after
// the version byte. Merging two envelopes is appending one region to the other,
// so this is all a fan-in needs -- no segment is materialised.
func packedRecords(s string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) == 0 || raw[0] != packedRetCtxVersion {
		return nil, false
	}
	if !validPackedRecords(raw[1:]) {
		return nil, false
	}
	return raw[1:], true
}

// validPackedRecords walks the framing without building anything, so a malformed
// or truncated buffer is rejected before it can be concatenated onto another.
func validPackedRecords(raw []byte) bool {
	for len(raw) > 0 {
		tag := raw[0]
		if _, ok := packedKindName[tag&packedKindMask]; !ok {
			return false
		}
		size, n := binary.Uvarint(raw[1:])
		if n <= 0 || uint64(len(raw)) < uint64(1+n)+size {
			return false
		}
		body := size
		if tag&packedTTLPresent != 0 {
			if body < 1 {
				return false
			}
			body--
		}
		if body < 9 { // spanID(8) plus at least one varint byte of depth
			return false
		}
		raw = raw[uint64(1+n)+size:]
	}
	return true
}

func decodePackedRetCtx(s string) (trussData, bool) {
	raw, ok := packedRecords(s)
	if !ok {
		return trussData{}, false
	}
	var t trussData
	var fingerprints []byte
	for len(raw) > 0 {
		tag := raw[0]
		size, n := binary.Uvarint(raw[1:])
		body := raw[1+n : uint64(1+n)+size]
		segment := TrussSegment{Kind: packedKindName[tag&packedKindMask]}
		if tag&packedTTLPresent != 0 {
			ttl := body[0]
			segment.ReverseTTL = &ttl
			body = body[1:]
		}
		segment.Data = body
		t.Segs = append(t.Segs, segment)
		// fp is not transmitted: it is the span ID that already heads Data.
		var id trace.SpanID
		copy(id[:], body[:8])
		if len(fingerprints) > 0 {
			fingerprints = append(fingerprints, ',')
		}
		fingerprints = append(fingerprints, id.String()...)
		raw = raw[uint64(1+n)+size:]
	}
	t.FP = string(fingerprints)
	return t, true
}

// mergePackedRetCtx concatenates two packed envelopes without decoding a single
// segment: both record regions are appended and re-framed. This is the operation
// every fan-in performs, and it is why the envelope must be concatenative.
func mergePackedRetCtx(a, b string) (string, bool) {
	left, ok := packedRecords(a)
	if !ok {
		return "", false
	}
	right, ok := packedRecords(b)
	if !ok {
		return "", false
	}
	buf := make([]byte, 0, 1+len(left)+len(right))
	buf = append(buf, packedRetCtxVersion)
	buf = append(buf, left...)
	buf = append(buf, right...)
	return base64.RawURLEncoding.EncodeToString(buf), true
}
