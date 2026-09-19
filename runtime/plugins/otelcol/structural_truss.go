package otelcol

import (
	"encoding/binary"
	"sort"
)

// Tomislav-RetCtx: S-Bridge truss = the CG-Bridge window core followed by the
// S-Bridge's own vertical ordinals and orthogonal end-event trusses (paper §3.4,
// Figure 4). Wire layout of one packed payload (before the optional forward
// TTL byte that checkpointRange.wrap adds to propagation baggage):
//
//	varint(absolute depth) || checkpoint(8) || byte(window distance - 1)
//	|| bloom[geometry(distance)]                       # CG core: ancestors AMQ
//	|| varint(len HA) || HA                            # CG core: fan-out witnesses
//	|| varint(n) || n * ( varint(ordinal) || endGroup(bound = ordinal-1) )
//	|| varint(m) || m * ( trace_id(16) || parent(8) || varint(k) || endGroup(bound = k) )
//
// The first four fields are byte-identical to the ranged PB/CGPB core, so
// unpackCheckpointWindowBR reads an S-Bridge payload's depth, anchor, distance
// and Bloom unchanged. The HA is length-prefixed here (CGPB leaves it as the
// trailing remainder) so the orthogonal tail can follow it.
//
// Ordinals: one start ordinal per span in the window, in path order, ending
// with the span that carries the payload. Position i's endGroup is the list of
// sibling start ordinals that had ENDED before span i started, as observed by
// span i's parent: an orthogonal truss recorded vertically. Delayed entries
// are end events a parent observed after its last child started (minus the
// implied last one), stamped with the parent's trace and span ID and attached
// to the next outgoing call from that service instance (paper Figure 6).
//
// endGroup: varint(count<<1 | lehmer). With lehmer=1 the group is a bitmap of
// `bound` bits selecting which ordinals ended, followed by the permutation of
// that set in end order as Lehmer digits packed into mixed-radix uvarint words.
// With lehmer=0 it is count plain varints (used when the binary was built with
// -tags sb_nolehmer, or when the ends are not a distinct subset of 1..bound).

type structuralDelayed struct {
	traceID  [16]byte
	parent   [8]byte
	children int
	ends     []int
}

type structuralTail struct {
	ordinals  []int
	endEvents [][]int // len == len(ordinals); endEvents[i] belongs to ordinals[i]
	delayed   []structuralDelayed
}

// mixedRadixLimit bounds one packed word so the arithmetic stays in uint64.
const mixedRadixLimit = uint64(1) << 62

// lehmerDigits maps a permutation of distinct values to its Lehmer digits:
// digit i is the rank of perm[i] among the values not yet placed.
func lehmerDigits(perm []int) []int {
	// One allocation holds both the sorted working set and the digits.
	scratch := make([]int, 2*len(perm))
	remaining, digits := scratch[:len(perm)], scratch[len(perm):]
	copy(remaining, perm)
	sort.Ints(remaining)
	for i, value := range perm {
		index := sort.SearchInts(remaining, value)
		digits[i] = index
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return digits
}

func lehmerPermutation(sorted []int, digits []int) ([]int, bool) {
	if len(digits) == 0 {
		return nil, true
	}
	remaining := append([]int(nil), sorted...)
	perm := make([]int, 0, len(digits))
	for _, digit := range digits {
		if digit < 0 || digit >= len(remaining) {
			return nil, false
		}
		perm = append(perm, remaining[digit])
		remaining = append(remaining[:digit], remaining[digit+1:]...)
	}
	return perm, true
}

// mixedRadixChunks splits digit positions of a permutation of n elements into
// runs whose radix product stays below mixedRadixLimit. Encoder and decoder
// derive identical boundaries from n alone.
func mixedRadixChunks(n int) [][2]int {
	var chunks [][2]int
	start := 0
	product := uint64(1)
	for i := 0; i < n; i++ {
		radix := uint64(n - i)
		if radix > mixedRadixLimit/product && i > start { // overflow-safe product bound
			chunks = append(chunks, [2]int{start, i})
			start, product = i, 1
		}
		product *= radix
	}
	if n > 0 {
		chunks = append(chunks, [2]int{start, n})
	}
	return chunks
}

func packLehmer(out []byte, digits []int) []byte {
	n := len(digits)
	for _, chunk := range mixedRadixChunks(n) {
		word := uint64(0)
		for i := chunk[0]; i < chunk[1]; i++ {
			word = word*uint64(n-i) + uint64(digits[i])
		}
		out = binary.AppendUvarint(out, word)
	}
	return out
}

func unpackLehmer(buf []byte, n int) (digits []int, rest []byte, ok bool) {
	digits = make([]int, n)
	for _, chunk := range mixedRadixChunks(n) {
		word, read := binary.Uvarint(buf)
		if read <= 0 {
			return nil, nil, false
		}
		buf = buf[read:]
		for i := chunk[1] - 1; i >= chunk[0]; i-- {
			radix := uint64(n - i)
			digits[i] = int(word % radix)
			word /= radix
		}
		if word != 0 {
			return nil, nil, false
		}
	}
	return digits, buf, true
}

func lehmerApplicable(ends []int, bound int) bool {
	if bound <= 0 || len(ends) > bound {
		return false
	}
	var small uint64 // allocation-free membership for the common bound <= 64
	var large []bool
	if bound > 64 {
		large = make([]bool, bound+1)
	}
	for _, e := range ends {
		if e < 1 || e > bound {
			return false
		}
		if large != nil {
			if large[e] {
				return false
			}
			large[e] = true
		} else {
			if small&(1<<uint(e-1)) != 0 {
				return false
			}
			small |= 1 << uint(e-1)
		}
	}
	return true
}

// packEndGroup appends one end-event group. bound is the largest ordinal the
// group may contain (own ordinal - 1 for inherited groups, child count for
// delayed groups).
func packEndGroup(out []byte, ends []int, bound int) []byte {
	if len(ends) == 0 {
		return append(out, 0) // empty group: one byte, no bitmap
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

func unpackEndGroup(buf []byte, bound int) (ends []int, rest []byte, ok bool) {
	header, read := binary.Uvarint(buf)
	if read <= 0 {
		return nil, nil, false
	}
	buf = buf[read:]
	count := int(header >> 1)
	if count == 0 {
		return nil, buf, header&1 == 0
	}
	if header&1 == 0 {
		ends = make([]int, 0, count)
		for i := 0; i < count; i++ {
			v, n := binary.Uvarint(buf)
			if n <= 0 {
				return nil, nil, false
			}
			buf = buf[n:]
			ends = append(ends, int(v))
		}
		return ends, buf, true
	}
	if bound <= 0 || count > bound {
		return nil, nil, false
	}
	width := (bound + 7) / 8
	if len(buf) < width {
		return nil, nil, false
	}
	sorted := make([]int, 0, count)
	for e := 1; e <= bound; e++ {
		if buf[(e-1)/8]&(1<<uint((e-1)%8)) != 0 {
			sorted = append(sorted, e)
		}
	}
	buf = buf[width:]
	if len(sorted) != count {
		return nil, nil, false
	}
	digits, buf, ok := unpackLehmer(buf, count)
	if !ok {
		return nil, nil, false
	}
	ends, ok = lehmerPermutation(sorted, digits)
	return ends, buf, ok
}

func packStructuralDelayed(out []byte, d structuralDelayed) []byte {
	out = append(out, d.traceID[:]...)
	out = append(out, d.parent[:]...)
	out = binary.AppendUvarint(out, uint64(maxInt(d.children, 0)))
	return packEndGroup(out, d.ends, d.children)
}

func unpackStructuralDelayed(buf []byte) (d structuralDelayed, rest []byte, ok bool) {
	if len(buf) < 24 {
		return d, nil, false
	}
	copy(d.traceID[:], buf[:16])
	copy(d.parent[:], buf[16:24])
	buf = buf[24:]
	k, read := binary.Uvarint(buf)
	if read <= 0 {
		return d, nil, false
	}
	d.children = int(k)
	d.ends, rest, ok = unpackEndGroup(buf[read:], d.children)
	return d, rest, ok
}

func packStructuralTail(out []byte, tail structuralTail) []byte {
	out = binary.AppendUvarint(out, uint64(len(tail.ordinals)))
	for i, ordinal := range tail.ordinals {
		out = binary.AppendUvarint(out, uint64(maxInt(ordinal, 0)))
		var ends []int
		if i < len(tail.endEvents) {
			ends = tail.endEvents[i]
		}
		out = packEndGroup(out, ends, ordinal-1)
	}
	out = binary.AppendUvarint(out, uint64(len(tail.delayed)))
	for _, d := range tail.delayed {
		out = packStructuralDelayed(out, d)
	}
	return out
}

func unpackStructuralTail(buf []byte) (tail structuralTail, ok bool) {
	n, read := binary.Uvarint(buf)
	if read <= 0 || n > uint64(len(buf)) {
		return tail, false
	}
	buf = buf[read:]
	tail.ordinals = make([]int, 0, n)
	tail.endEvents = make([][]int, 0, n)
	for i := uint64(0); i < n; i++ {
		ordinal, r := binary.Uvarint(buf)
		if r <= 0 {
			return tail, false
		}
		var ends []int
		ends, buf, ok = unpackEndGroup(buf[r:], int(ordinal)-1)
		if !ok {
			return tail, false
		}
		tail.ordinals = append(tail.ordinals, int(ordinal))
		tail.endEvents = append(tail.endEvents, ends)
	}
	m, read := binary.Uvarint(buf)
	if read <= 0 || m > uint64(len(buf)) {
		return tail, false
	}
	buf = buf[read:]
	for i := uint64(0); i < m; i++ {
		var d structuralDelayed
		d, buf, ok = unpackStructuralDelayed(buf)
		if !ok {
			return tail, false
		}
		tail.delayed = append(tail.delayed, d)
	}
	return tail, len(buf) == 0
}

// packStructuralBR packs the complete S-Bridge payload described above.
func packStructuralBR(depth int, ckpt [8]byte, distance int, bloomBytes, ha []byte, tail structuralTail) []byte {
	out := make([]byte, 0, varintLen(depth)+9+len(bloomBytes)+varintLen(len(ha))+len(ha)+8*(len(tail.ordinals)+1)+40*len(tail.delayed))
	out = binary.AppendUvarint(out, uint64(maxInt(depth, 0)))
	out = append(out, ckpt[:]...)
	out = append(out, byte(distance-1))
	out = append(out, bloomBytes...)
	out = binary.AppendUvarint(out, uint64(len(ha)))
	out = append(out, ha...)
	return packStructuralTail(out, tail)
}

// unpackStructuralBR reverses packStructuralBR. bloomBytes and ha are
// sub-slices of raw; the tail is copied out.
func unpackStructuralBR(raw []byte) (depth int, ckpt [8]byte, distance int, bloomBytes, ha []byte, tail structuralTail, ok bool) {
	d, n := binary.Uvarint(raw)
	if n <= 0 || d > uint64(^uint(0)>>1) || len(raw)-n < 9 {
		return
	}
	copy(ckpt[:], raw[n:n+8])
	distance = int(raw[n+8]) + 1
	geometry := checkpointBlooms[distance-1]
	rest := raw[n+9:]
	if len(rest) < geometry.bytes {
		return 0, ckpt, 0, nil, nil, tail, false
	}
	bloomBytes, rest = rest[:geometry.bytes], rest[geometry.bytes:]
	haLen, read := binary.Uvarint(rest)
	if read <= 0 || haLen > uint64(len(rest)-read) {
		return 0, ckpt, 0, nil, nil, tail, false
	}
	rest = rest[read:]
	ha, rest = rest[:haLen], rest[haLen:]
	tail, ok = unpackStructuralTail(rest)
	if !ok {
		return 0, ckpt, 0, nil, nil, structuralTail{}, false
	}
	return int(d), ckpt, distance, bloomBytes, ha, tail, true
}

// structuralDelayedFromServer converts the server wrapper's end-of-request
// summary (varint(children) || varint(count) || count*varint(ordinal)) into a
// delayed truss stamped with the parent's identity.
func structuralDelayedFromServer(traceID [16]byte, parent [8]byte, summary []byte) (structuralDelayed, bool) {
	k, n := binary.Uvarint(summary)
	if n <= 0 {
		return structuralDelayed{}, false
	}
	summary = summary[n:]
	count, n := binary.Uvarint(summary)
	if n <= 0 || count > uint64(len(summary)) {
		return structuralDelayed{}, false
	}
	summary = summary[n:]
	ends := make([]int, 0, count)
	for i := uint64(0); i < count; i++ {
		v, r := binary.Uvarint(summary)
		if r <= 0 {
			return structuralDelayed{}, false
		}
		summary = summary[r:]
		ends = append(ends, int(v))
	}
	return structuralDelayed{traceID: traceID, parent: parent, children: int(k), ends: ends}, true
}
