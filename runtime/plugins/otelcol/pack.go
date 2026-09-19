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

// decodeBRDepth extracts only the depth field (the leading varint) from
// an encoded BR payload string, without unpacking the full message.
// Lets OnEnd recover OnStart's depth decision without having to keep
// an intra-process `__bag.prio` attribute around. Returns (0, false)
// on malformed input.
func decodeBRDepth(s string) (int, bool) {
	raw, ok := decodeBR(s)
	if !ok {
		return 0, false
	}
	depth, n := binary.Uvarint(raw)
	if n <= 0 {
		return 0, false
	}
	return int(depth), true
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

// AttrBR is the SDK span attribute that becomes the OUTGOING `_br`
// baggage on the next downstream RPC. The opentelemetry plugin wrappers
// translate `__bag.*` attributes into outgoing baggage by stripping the
// prefix, so __bag._br → baggage key "_br". The value is the packed
// PROPAGATION snapshot (post-reset if this span is a checkpoint).
const AttrBR = "__bag._br"

// AttrBREmit is the WIRE-EXPORT attribute the processor attaches to a
// span when the span is a checkpoint. Its presence on the exported OTLP
// span IS the priority signal — matching the simulator's model, where
// emit-bytes are produced iff the span is a checkpoint (no separate
// priority bit). The value carries the PRE-RESET packed payload (the
// full inherited chain ending at this span). Stripped at export time
// for non-checkpoint spans by createResourceSpans/convertAttributes.
const AttrBREmit = "_br"

// AttrOC is the ordinal-chain breadcrumb attached to NON-checkpoint spans:
// packed nearest-ancestor checkpoint id (full 8-byte ckpt, pre-reset) + the ordinal
// chain (ordinalGroups), WITHOUT endEvents/dee. Kept on the wire iff the span
// is NON-checkpoint (low-priority); on a checkpoint the full `_br` already
// carries ckpt+chain, so `_oc` is stripped there as redundant. Lets a
// surviving non-checkpoint span self-anchor for reconstruction under PARTIAL
// (duty-cycle) shedding — when its non-checkpoint ancestors were dropped and
// no downstream checkpoint captured its ordinal.
const AttrOC = "_oc"

// AttrO is the MINIMAL non-checkpoint breadcrumb: key "_o" ("ordinal"), value
// = just THIS span's own ordinal (seqNum) as a varint, emitted on the wire as
// a proto BYTES value (not base64). Replaces the full _oc ordinal chain on
// non-checkpoints — a surviving non-checkpoint self-places via its native
// parent_span_id (immediate parent) + this ordinal; the inter-checkpoint chain
// (with parent fingerprints) lives only in the checkpoint's _br. Carried
// OnStart→export as a base64 string (UTF-8-safe intra-process); convertAttributes
// base64-decodes and emits the raw varint bytes.
const AttrO = "_o"

// AttrDepth is the LEAN breadcrumb (experiment): a single int64 depth on
// NON-checkpoint spans, in place of the base64 _oc string. Minimal payload,
// full per-KV alloc overhead — used to isolate whether the _oc collapse was
// payload size or per-attribute allocation cost.
const AttrDepth = "d"

// OCNameDelim separates the clean operation name from the appended base64
// breadcrumb in a non-checkpoint span's NAME ("<name>~<base64>"). It must NOT
// be a base64url char (A-Za-z0-9-_), so the async reconstructor can scan back
// from the end over base64url chars to the first OCNameDelim, split off the
// breadcrumb, and restore the clean name.
const OCNameDelim = "~"

// AttrBagPrio is the INTRA-PROCESS span attribute the SDK uses to
// communicate the OnStart-time priority decision to OnEnd. Stripped at
// export time along with every other __bag.* attribute — does NOT
// reach the wire. The collector-side priorityprocessor does NOT read
// this; it reads AttrBREmit presence instead.
const AttrBagPrio = "__bag.prio"

// AttrForceLP is an intra-process escape hatch for synthetic pressure
// spans. When a span carries `__bag.force_lp = true`, the SB processor's
// OnEnd classifies the span as LP regardless of depth or leaf-server
// status. Stripped at export time (__bag.* prefix), so this attribute
// never reaches the wire.
//
// Use case: a service like TracePressureService creates many child spans
// inside one HTTP request handler via tracer.Start(...). All of those
// children share the same OTel parent context, which means OnStart sees
// identical baggage for each and computes identical depthMod. Without
// this escape hatch, all such children would classify identically to
// the root span, making it impossible to generate pure-LP volume for
// stress-testing the priority-aware shedding policy.
const AttrForceLP = "__bag.force_lp"

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
	case n < 1<<7:
		return 1
	case n < 1<<14:
		return 2
	case n < 1<<21:
		return 3
	case n < 1<<28:
		return 4
	case n < 1<<35:
		return 5
	case n < 1<<42:
		return 6
	case n < 1<<49:
		return 7
	case n < 1<<56:
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

// AttrD is the path-bridge depth breadcrumb attached to interior (has-children)
// NON-checkpoint spans: key "_d", value = varint(ABSOLUTE depth), emitted on
// the wire as a proto BYTES value (not base64) — exactly mirroring how the SB
// processor emits "_o". A surviving interior non-checkpoint self-places via its
// native parent_span_id + this depth; the full anchored payload (`_br`) lives
// only on checkpoints and leaves. Carried OnStart→export as a base64 string
// (UTF-8-safe intra-process); convertAttributes base64-decodes to raw varint bytes.
const AttrD = "_d"

// pbBloomCapacity is the path-bridge bloom sizing population: cpd-1, NOT cpd.
// Threading only ever tests spans STRICTLY between two checkpoints (the anchor
// is named by the ckpt anchor, never membership-tested; a payload's own span is never
// tested against itself), and payloads carry the INHERITED (pre-self) bloom —
// so the deepest payload holds exactly cpd-1 entries. Matches bridges
// bridge/pcrb.go PCRBBloomCapacity; the recon side must derive (m,k) from the
// same capacity.
func pbBloomCapacity(cpd int) int {
	if cpd <= 2 {
		return 1
	}
	return cpd - 1
}

// packPathBridgeBR packs the (canonical, ckpt-anchored) path-bridge payload:
//
//	varint(absolute depth) || ckpt(8) || bloomBytes
//
// Tag-less (PB is the only path bridge). Used for BOTH the emitted `_br`
// (checkpoints + leaves: depth, the PREVIOUS checkpoint's FULL 8-byte span ID,
// and the INHERITED pre-self window bloom) AND the propagation baggage (depth,
// this span's ckpt — own id if checkpoint, inherited otherwise — and the
// propagated bloom). ckpt is the full big-endian nearest-checkpoint span ID
// (full-width checkpoint-root fingerprint); all-zero = root.
func packPathBridgeBR(depth int, ckpt [8]byte, bloomBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depth)+8+len(bloomBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt[:]...)
	out = append(out, bloomBytes...)
	return out
}

// unpackPathBridgeBR reverses packPathBridgeBR. bloomBytes is a sub-slice of
// buf (copy if retaining past buf's lifetime). ok=false on a malformed payload.
func unpackPathBridgeBR(buf []byte) (depth int, ckpt [8]byte, bloomBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, ckpt, nil, false
	}
	rest := buf[n:]
	if len(rest) < 8 {
		return 0, ckpt, nil, false
	}
	copy(ckpt[:], rest[:8])
	return int(v), ckpt, rest[8:], true
}

// packCGPRBBR packs the (canonical, ckpt-anchored) call-graph path-bridge
// payload — CGPRB = PCRB + a window-local hash array:
//
//	varint(absolute depth) || ckpt(8) || bloomBytes || haBytes
//
// Tag-less. bloomBytes is fixed-width (ceil(m/8)) so the decoder splits the HA
// off as the trailing remainder. Used for BOTH the emitted `_br` (checkpoints +
// leaves: inherited pre-self bloom + window HA) and the propagation baggage
// (propagated bloom + HA — reset to empty at a checkpoint). HA entries are
// (parent_span_id(8) || varint(absolute depth)), appended via haAppendEntry.
func packCGPRBBR(depth int, ckpt [8]byte, bloomBytes, haBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depth)+8+len(bloomBytes)+len(haBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt[:]...)
	out = append(out, bloomBytes...)
	out = append(out, haBytes...)
	return out
}

// unpackCGPRBBR reverses packCGPRBBR. bloomLen is the fixed bloom byte width
// (ceil(m/8)); the HA is the trailing remainder. bloomBytes/haBytes are
// sub-slices of buf (copy if retaining past buf's lifetime). ok=false on a
// malformed payload.
func unpackCGPRBBR(buf []byte, bloomLen int) (depth int, ckpt [8]byte, bloomBytes, haBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, ckpt, nil, nil, false
	}
	rest := buf[n:]
	if len(rest) < 8+bloomLen {
		return 0, ckpt, nil, nil, false
	}
	copy(ckpt[:], rest[:8])
	rest = rest[8:]
	return int(v), ckpt, rest[:bloomLen], rest[bloomLen:], true
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
