package otelcol

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
)

// encodeBR wraps a packed _br payload as a base64-URL-no-pad string suitable
// for W3C Baggage / OTLP StringValue transport. Inflates by ceil(4*N/3) with
// no second-order percent-encoding (the URL-safe alphabet avoids `+`, `/`,
// `=`, which some propagators silently re-encode).
func encodeBR(packed []byte) string {
	return base64.RawURLEncoding.EncodeToString(packed)
}

// decodeBR reverses encodeBR. Returns nil/false on malformed input.
func decodeBR(s string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Wire-format constants ported from bridges/bridge/pack.go. Kept bit-exact
// so that bagsize numbers measured by the trace_sim Go port apply to the
// real processor output.
const (
	// "_br" property name overhead.
	BRPropertyNameOverheadBytes = 3

	// Bridge type IDs double as byte counts in the simulator's payload
	// accounting (matching trace_simulator.py).
	PBBridgeTypeID  = 1
	CGPBridgeTypeID = 2
	SBridgeTypeID   = 3

	// Default bloom false-positive rate used by PB / CGPB.
	DefaultBloomFPRate = 0.0001

	// Baggage-key byte size for "_br".
	BaggageKeyBytes = 3
)

// BaggageBRKey is the single baggage key under which the bit-packed _br
// payload travels on the wire (replaces the per-field baggage keys).
const BaggageBRKey = "_br"

// AttrBR is the span attribute name the processor writes the packed _br
// payload into. The opentelemetry plugin wrappers translate `__bag.*`
// attributes into outgoing baggage.
const AttrBR = "__bag._br"

// varintEncode encodes a non-negative integer as a protobuf-style varint.
func varintEncode(n int) []byte {
	if n < 0 {
		n = 0
	}
	return binary.AppendUvarint(nil, uint64(n))
}

// varintLen returns the byte length of varintEncode(n) without allocating.
func varintLen(n int) int {
	if n < 0 {
		n = 0
	}
	switch {
	case n < 1 << 7:
		return 1
	case n < 1 << 14:
		return 2
	case n < 1 << 21:
		return 3
	case n < 1 << 28:
		return 4
	case n < 1 << 35:
		return 5
	case n < 1 << 42:
		return 6
	case n < 1 << 49:
		return 7
	case n < 1 << 56:
		return 8
	}
	return 9
}

// packBR packs the PB baggage payload: varint(depthMod) || bloomBytes.
func packBR(depthMod int, bloomBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depthMod)+len(bloomBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depthMod, 0)))
	out = append(out, bloomBytes...)
	return out
}

// unpackBR reverses packBR. Returns depthMod and the bloomBytes slice
// (sub-slice of buf — copy if you need to retain it past buf's lifetime).
// ok=false on a malformed payload.
func unpackBR(buf []byte) (depthMod int, bloomBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, nil, false
	}
	return int(v), buf[n:], true
}

// packCGPBBR packs the CGPB baggage payload:
//
//	varint(depthMod) || bloomBytes || haBytes
func packCGPBBR(depthMod int, bloomBytes, haBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depthMod)+len(bloomBytes)+len(haBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depthMod, 0)))
	out = append(out, bloomBytes...)
	out = append(out, haBytes...)
	return out
}

// unpackCGPBBR reverses packCGPBBR. bloomLen is the fixed bloom byte width
// (ceil(m/8)); the remainder is the hash-array blob.
func unpackCGPBBR(buf []byte, bloomLen int) (depthMod int, bloomBytes, haBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, nil, nil, false
	}
	rest := buf[n:]
	if len(rest) < bloomLen {
		return 0, nil, nil, false
	}
	return int(v), rest[:bloomLen], rest[bloomLen:], true
}

// haAppendEntry appends one CGPB hash-array entry to ha:
//
//	entry := parent_span_id_bytes(8) || varint(depthMod)
//
// parentSpanID is the 16-char hex span ID. Returns ha unchanged on invalid hex.
func haAppendEntry(ha []byte, parentSpanID string, depthMod int) []byte {
	pid, ok := spanIDHexTo8Bytes(parentSpanID)
	if !ok {
		return ha
	}
	out := make([]byte, 0, len(ha)+8+varintLen(depthMod))
	out = append(out, ha...)
	out = append(out, pid[:]...)
	out = binary.AppendUvarint(out, uint64(maxInt(depthMod, 0)))
	return out
}

// spanIDHexTo8Bytes converts a Jaeger / W3C span-id hex string to 8 raw
// bytes. Shorter inputs are left-padded with zeros; longer inputs keep the
// last 8 bytes. Empty / invalid hex returns ok=false.
func spanIDHexTo8Bytes(s string) ([8]byte, bool) {
	var out [8]byte
	if s == "" {
		return out, false
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return out, false
	}
	switch {
	case len(raw) == 8:
		copy(out[:], raw)
	case len(raw) > 8:
		copy(out[:], raw[len(raw)-8:])
	default:
		copy(out[8-len(raw):], raw)
	}
	return out, true
}

// traceIDHexTo16Bytes converts a W3C trace-id hex string to 16 raw bytes.
// Empty / invalid hex returns 16 zero bytes.
func traceIDHexTo16Bytes(s string) [16]byte {
	var out [16]byte
	if s == "" {
		return out
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return out
	}
	switch {
	case len(raw) == 16:
		copy(out[:], raw)
	case len(raw) > 16:
		copy(out[:], raw[len(raw)-16:])
	default:
		copy(out[16-len(raw):], raw)
	}
	return out
}

// packSBridgeBR packs the S-Bridge baggage payload, matching the Python
// pack_sbridge_br exactly (sorted depth groups, varint counts, varint
// sequences, trailing deeBytes blob). depth is clamped to >= 0.
func packSBridgeBR(
	depth int,
	ckpt8 [8]byte,
	ordinalGroups map[int][]int,
	endEvents []int,
	deeBytes []byte,
) []byte {
	depths := make([]int, 0, len(ordinalGroups))
	for d := range ordinalGroups {
		depths = append(depths, d)
	}
	sortInts(depths)

	size := varintLen(depth) + 8 + varintLen(len(depths))
	for _, d := range depths {
		seqs := ordinalGroups[d]
		size += varintLen(d) + varintLen(len(seqs))
		for _, s := range seqs {
			size += varintLen(s)
		}
	}
	size += varintLen(len(endEvents))
	for _, s := range endEvents {
		size += varintLen(s)
	}
	size += len(deeBytes)

	out := make([]byte, 0, size)
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt8[:]...)
	out = binary.AppendUvarint(out, uint64(len(depths)))
	for _, d := range depths {
		seqs := ordinalGroups[d]
		out = binary.AppendUvarint(out, uint64(d))
		out = binary.AppendUvarint(out, uint64(len(seqs)))
		for _, s := range seqs {
			out = binary.AppendUvarint(out, uint64(maxInt(s, 0)))
		}
	}
	out = binary.AppendUvarint(out, uint64(len(endEvents)))
	for _, s := range endEvents {
		out = binary.AppendUvarint(out, uint64(maxInt(s, 0)))
	}
	out = append(out, deeBytes...)
	return out
}

// unpackSBridgeBR reverses packSBridgeBR. deeBytes is the trailing blob
// (sub-slice of buf — copy if you need to retain it past buf's lifetime).
// ok=false on a malformed payload.
func unpackSBridgeBR(buf []byte) (
	depth int,
	ckpt8 [8]byte,
	ordinalGroups map[int][]int,
	endEvents []int,
	deeBytes []byte,
	ok bool,
) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return
	}
	depth = int(v)
	buf = buf[n:]
	if len(buf) < 8 {
		return
	}
	copy(ckpt8[:], buf[:8])
	buf = buf[8:]

	numDepths, n := binary.Uvarint(buf)
	if n <= 0 {
		return
	}
	buf = buf[n:]
	if numDepths > 0 {
		ordinalGroups = make(map[int][]int, numDepths)
	}
	for i := uint64(0); i < numDepths; i++ {
		dv, dn := binary.Uvarint(buf)
		if dn <= 0 {
			return
		}
		buf = buf[dn:]
		ns, nsn := binary.Uvarint(buf)
		if nsn <= 0 {
			return
		}
		buf = buf[nsn:]
		seqs := make([]int, 0, ns)
		for j := uint64(0); j < ns; j++ {
			sv, sn := binary.Uvarint(buf)
			if sn <= 0 {
				return
			}
			buf = buf[sn:]
			seqs = append(seqs, int(sv))
		}
		ordinalGroups[int(dv)] = seqs
	}

	numEnds, n := binary.Uvarint(buf)
	if n <= 0 {
		return
	}
	buf = buf[n:]
	if numEnds > 0 {
		endEvents = make([]int, 0, numEnds)
	}
	for i := uint64(0); i < numEnds; i++ {
		sv, sn := binary.Uvarint(buf)
		if sn <= 0 {
			return
		}
		buf = buf[sn:]
		endEvents = append(endEvents, int(sv))
	}

	deeBytes = buf
	ok = true
	return
}

// encodeDEETriple encodes one delayed-end-event triple:
//
//	16-byte trace_id || varint(depth) || varint(n) || n * varint(start_seq)
func encodeDEETriple(traceID16 [16]byte, depth int, seqs []int) []byte {
	size := 16 + varintLen(depth) + varintLen(len(seqs))
	for _, s := range seqs {
		size += varintLen(s)
	}
	out := make([]byte, 0, size)
	out = append(out, traceID16[:]...)
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = binary.AppendUvarint(out, uint64(len(seqs)))
	for _, s := range seqs {
		out = binary.AppendUvarint(out, uint64(maxInt(s, 0)))
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// sortInts is a tiny in-place insertion sort to avoid an "import sort" for
// the small depth-group slices (typically O(few)).
func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}
