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
// packed nearest-ancestor checkpoint id (ckpt4, pre-reset) + the ordinal
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
// is named by ckpt4, never membership-tested; a payload's own span is never
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

// packPathBridgeBR packs the (canonical, ckpt4-anchored) path-bridge payload:
//
//	varint(absolute depth) || ckpt4 || bloomBytes
//
// Tag-less (PB is the only path bridge). Used for BOTH the emitted `_br`
// (checkpoints + leaves: depth, the PREVIOUS checkpoint's 4-byte ckpt4, and the
// INHERITED pre-self window bloom) AND the propagation baggage (depth, this
// span's ckpt4 — own id if checkpoint, inherited otherwise — and the propagated
// bloom). ckpt4 is the first 4 bytes of the big-endian nearest-checkpoint span
// ID; all-zero = root.
func packPathBridgeBR(depth int, ckpt4 [4]byte, bloomBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depth)+4+len(bloomBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt4[:]...)
	out = append(out, bloomBytes...)
	return out
}

// unpackPathBridgeBR reverses packPathBridgeBR. bloomBytes is a sub-slice of
// buf (copy if retaining past buf's lifetime). ok=false on a malformed payload.
func unpackPathBridgeBR(buf []byte) (depth int, ckpt4 [4]byte, bloomBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, ckpt4, nil, false
	}
	rest := buf[n:]
	if len(rest) < 4 {
		return 0, ckpt4, nil, false
	}
	copy(ckpt4[:], rest[:4])
	return int(v), ckpt4, rest[4:], true
}

// packCGPRBBR packs the (canonical, ckpt4-anchored) call-graph path-bridge
// payload — CGPRB = PCRB + a window-local hash array:
//
//	varint(absolute depth) || ckpt4 || bloomBytes || haBytes
//
// Tag-less. bloomBytes is fixed-width (ceil(m/8)) so the decoder splits the HA
// off as the trailing remainder. Used for BOTH the emitted `_br` (checkpoints +
// leaves: inherited pre-self bloom + window HA) and the propagation baggage
// (propagated bloom + HA — reset to empty at a checkpoint). HA entries are
// (parent_span_id(8) || varint(absolute depth)), appended via haAppendEntry.
func packCGPRBBR(depth int, ckpt4 [4]byte, bloomBytes, haBytes []byte) []byte {
	out := make([]byte, 0, varintLen(depth)+4+len(bloomBytes)+len(haBytes))
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt4[:]...)
	out = append(out, bloomBytes...)
	out = append(out, haBytes...)
	return out
}

// unpackCGPRBBR reverses packCGPRBBR. bloomLen is the fixed bloom byte width
// (ceil(m/8)); the HA is the trailing remainder. bloomBytes/haBytes are
// sub-slices of buf (copy if retaining past buf's lifetime). ok=false on a
// malformed payload.
func unpackCGPRBBR(buf []byte, bloomLen int) (depth int, ckpt4 [4]byte, bloomBytes, haBytes []byte, ok bool) {
	v, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, ckpt4, nil, nil, false
	}
	rest := buf[n:]
	if len(rest) < 4+bloomLen {
		return 0, ckpt4, nil, nil, false
	}
	copy(ckpt4[:], rest[:4])
	rest = rest[4:]
	return int(v), ckpt4, rest[:bloomLen], rest[bloomLen:], true
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

// ordEntry is one ordinal-chain entry: a span's sibling ordinal plus the
// FINGERPRINT of its immediate parent — the leading bytes of the parent's
// span ID. fp is 4 bytes when the parent is a checkpoint (entry depthMod==1),
// else 2 bytes. Each span fills fp locally from its native parent span ID in
// OnStart; nothing extra is propagated for it.
type ordEntry struct {
	ord int
	fp  []byte
}

// fpLenForDepthMod returns the parent-fingerprint byte width for an ordinal
// entry at the given depthMod: 4 if the parent is a checkpoint (the entry sits
// at depthMod==1, so its parent is at depthMod 0), else 2. The decoder recovers
// this purely from the group key — no per-entry length flag on the wire.
func fpLenForDepthMod(depthMod int) int {
	if depthMod == 1 {
		return 4
	}
	return 2
}

// packSBridgeBR packs the S-Bridge baggage payload. Wire format (shared
// contract with the bridges reconstruction decoder):
//
//	varint(depth) || varint(numGroups) ||
//	  per group: varint(depthMod) || varint(numEntries) ||
//	    numEntries * ( varint(ordinal) || fp )      # fp = 4B if depthMod==1 else 2B
//	  varint(numEnd) || numEnd * varint(seq) || deeBytes (tail)
//
// There is NO explicit checkpoint anchor: it's redundant. A non-checkpoint
// span's nearest-checkpoint anchor is the fp of its depthMod==1 entry (the 4
// bytes of the checkpoint's span ID); a checkpoint span IS its own anchor via
// its native span_id. An all-zero parent fp marks the root (no parent). fp
// lengths are NOT length-prefixed — the decoder derives each from the group's
// depthMod via fpLenForDepthMod. depth is clamped to >= 0.
func packSBridgeBR(
	depth int,
	ordinalGroups map[int][]ordEntry,
	endEvents []int,
	deeBytes []byte,
) []byte {
	depths := make([]int, 0, len(ordinalGroups))
	for d := range ordinalGroups {
		depths = append(depths, d)
	}
	sortInts(depths)

	size := varintLen(depth) + varintLen(len(depths))
	for _, d := range depths {
		entries := ordinalGroups[d]
		size += varintLen(d) + varintLen(len(entries))
		for _, e := range entries {
			size += varintLen(e.ord) + len(e.fp)
		}
	}
	size += varintLen(len(endEvents))
	for _, s := range endEvents {
		size += varintLen(s)
	}
	size += len(deeBytes)

	out := make([]byte, 0, size)
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = binary.AppendUvarint(out, uint64(len(depths)))
	for _, d := range depths {
		entries := ordinalGroups[d]
		out = binary.AppendUvarint(out, uint64(d))
		out = binary.AppendUvarint(out, uint64(len(entries)))
		for _, e := range entries {
			out = binary.AppendUvarint(out, uint64(maxInt(e.ord, 0)))
			out = append(out, e.fp...)
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
	ordinalGroups map[int][]ordEntry,
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

	numDepths, n := binary.Uvarint(buf)
	if n <= 0 {
		return
	}
	buf = buf[n:]
	if numDepths > 0 {
		ordinalGroups = make(map[int][]ordEntry, numDepths)
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
		fpLen := fpLenForDepthMod(int(dv))
		entries := make([]ordEntry, 0, ns)
		for j := uint64(0); j < ns; j++ {
			sv, sn := binary.Uvarint(buf)
			if sn <= 0 {
				return
			}
			buf = buf[sn:]
			if len(buf) < fpLen {
				return
			}
			fp := append([]byte(nil), buf[:fpLen]...)
			buf = buf[fpLen:]
			entries = append(entries, ordEntry{ord: int(sv), fp: fp})
		}
		ordinalGroups[int(dv)] = entries
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
