package backend

// Reverse-truss (retCtx) support - split out of trace.go per Raja
import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/trace"
)

// Legacy ancestry-only AMQ geometry, sourced from the fixed-distance SDK or
// environment. These parameters describe SegAMQ payloads, not complete SDK
// checkpoint segments. Ranged PB/CGPB checkpoints carry their own immutable
// window distance inside each truss, so mixed window sizes survive fan-in.
// The legacy AMQ geometry is not a fixed constant:
//  1. SetRTBloomParams(m,k) is called by the otelcol bridge processor when it
//     sizes its own Bloom for the discovered cpd (checkpoint distance) -> reverse == forward geometry
//  2. Standalone fallback (synthetic trees, or any service without bridge
//     processor) reads it from env at startup:
//     RT_BLOOM_CAP expected #elements (default 8)
//     RT_BLOOM_FPR target FP rate (default 0.0001 == otelcol.DefaultBloomFPRate)
//
// Legacy AMQ envelopes record (m,k); windowed checkpoint consumers instead
// derive geometry from the descriptor in each embedded SDK truss.
var (
	rtBloomM atomic.Uint64
	rtBloomK atomic.Uint32
)

func init() {
	capacity := 8
	if n, err := strconv.Atoi(os.Getenv("RT_BLOOM_CAP")); err == nil && n > 0 {
		capacity = n
	}
	fpr := 0.0001
	if f, err := strconv.ParseFloat(os.Getenv("RT_BLOOM_FPR"), 64); err == nil && f > 0 {
		fpr = f
	}
	m, k := bloom.EstimateParameters(uint(capacity), fpr)
	rtBloomM.Store(m)
	rtBloomK.Store(uint32(k))
}

// SetRTBloomParams lets the SDK source reverse-truss Bloom geometry from the
// forward bridge's live geometry (called from runtime/plugins/otelcol).
func SetRTBloomParams(m uint64, k uint) {
	if m > 0 && k > 0 {
		rtBloomM.Store(m)
		rtBloomK.Store(uint32(k))
	}
}

func rtBloom() (uint64, uint) { return rtBloomM.Load(), uint(rtBloomK.Load()) }

// Generalized truss payload
type TrussSegment struct {
	Kind string `json:"k"` // amq | hash | ordinal | ee | dee | ...
	Data []byte `json:"d,omitempty"`
	// Tomislav-RetCtx: each returned truss counts down independently. A nil
	// TTL uses the receiving SDK's probability policy, or waits for an original
	// checkpoint when that SDK uses the default TTL policy.
	ReverseTTL *byte `json:"ttl,omitempty"`
}

const (
	SegAMQ     = "amq"
	SegHash    = "hash"
	SegOrdinal = "ordinal"
	SegEndEv   = "ee"
	SegDelayEE = "dee"

	// Checkpoint segments preserve a complete SDK truss and its intended
	// location: spanID(8 bytes) || uvarint(absolute depth) || truss bytes.
	SegPathCheckpoint       = "checkpoint.pb"
	SegCallGraphCheckpoint  = "checkpoint.cgpb"
	SegStructuralCheckpoint = "checkpoint.sb"
	SegVanillaCheckpoint    = "checkpoint.v"
)

type trussData struct {
	FP   string         `json:"fp"`
	Par  string         `json:"parent,omitempty"`
	M    uint64         `json:"m,omitempty"`
	K    uint           `json:"k,omitempty"`
	Segs []TrussSegment `json:"segs,omitempty"`
}

func encodeTruss(t trussData) string {
	// Tomislav-RetCtx: checkpoint-only envelopes pack like the forward path.
	// Anything carrying a legacy ancestry segment or a parent fingerprint keeps
	// the JSON envelope, which stays readable by older peers.
	if packableRetCtx(t) {
		return encodePackedRetCtx(t.Segs)
	}
	b, _ := json.Marshal(t)
	return base64.StdEncoding.EncodeToString(b)
}

func decodeTruss(s string) (trussData, bool) {
	var t trussData
	if s == "" {
		return t, false
	}
	// Packed envelopes are RawURLEncoding and start with a version byte; JSON
	// envelopes are StdEncoding and start with '{'. Neither can be mistaken for
	// the other, so both wire formats decode without sniffing.
	if packed, ok := decodePackedRetCtx(s); ok {
		return packed, true
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return t, false
	}
	if json.Unmarshal(raw, &t) != nil {
		return t, false
	}
	return t, true
}

// ReturnedCheckpoint describes where a truss was meant to be checkpointed.
// Forwarding and fan-in preserve this origin, even when a different span later
// checkpoints the truss. Truss contains the SDK's exact pre-reset _br bytes.
type ReturnedCheckpoint struct {
	Kind       string
	SpanID     trace.SpanID
	Depth      uint64
	Truss      []byte
	ReverseTTL *byte
}

// EncodeCheckpointRetCtx packs the original span ID and a binary varint depth
// with the truss. The existing string retCtx envelope carries the segment.
func EncodeCheckpointRetCtx(spanID trace.SpanID, depth uint64, kind string, truss []byte) string {
	return encodeCheckpointRetCtx(spanID, depth, kind, truss, nil)
}

// Tomislav-RetCtx: the mutable reverse TTL is separate from the immutable
// spanID || depth || truss payload. Distance D starts with TTL D-1; the next
// upstream span consumes zero or forwards a decremented copy.
func EncodeCheckpointRetCtxWithTTL(spanID trace.SpanID, depth uint64, kind string, truss []byte, ttl byte) string {
	return encodeCheckpointRetCtx(spanID, depth, kind, truss, &ttl)
}

func encodeCheckpointRetCtx(spanID trace.SpanID, depth uint64, kind string, truss []byte, ttl *byte) string {
	data := append([]byte(nil), spanID[:]...)
	data = binary.AppendUvarint(data, depth)
	data = append(data, truss...)
	m, k := rtBloom()
	return encodeTruss(trussData{
		FP: spanID.String(), M: m, K: k,
		Segs: []TrussSegment{{Kind: kind, Data: data, ReverseTTL: ttl}},
	})
}

// DecodeReturnedCheckpoints decodes checkpoint locations and trusses. Legacy
// ancestry-only segments can coexist in the envelope and are skipped here.
func DecodeReturnedCheckpoints(retCtx string) ([]ReturnedCheckpoint, error) {
	t, ok := decodeTruss(retCtx)
	if !ok {
		return nil, fmt.Errorf("invalid reverse-context envelope")
	}
	var checkpoints []ReturnedCheckpoint
	for _, segment := range t.Segs {
		checkpoint, known, err := decodeCheckpointSegment(segment)
		if err != nil {
			return nil, err
		}
		if known {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	return checkpoints, nil
}

func decodeCheckpointSegment(segment TrussSegment) (ReturnedCheckpoint, bool, error) {
	switch segment.Kind {
	case SegPathCheckpoint, SegCallGraphCheckpoint, SegStructuralCheckpoint, SegVanillaCheckpoint:
	default:
		return ReturnedCheckpoint{}, false, nil
	}
	if len(segment.Data) < 9 {
		return ReturnedCheckpoint{}, true, fmt.Errorf("truncated %s checkpoint location", segment.Kind)
	}
	var spanID trace.SpanID
	copy(spanID[:], segment.Data[:8])
	depth, n := binary.Uvarint(segment.Data[8:])
	if !spanID.IsValid() || n <= 0 {
		return ReturnedCheckpoint{}, true, fmt.Errorf("invalid %s checkpoint location", segment.Kind)
	}
	return ReturnedCheckpoint{
		Kind: segment.Kind, SpanID: spanID, Depth: depth,
		Truss: segment.Data[8+n:], ReverseTTL: segment.ReverseTTL,
	}, true, nil
}

// RouteRetCtx partitions one upstream hop into checkpointed and forwarded
// segments. Tomislav-RetCtx: only an original checkpoint consumes the whole
// bundle. A TTL-created checkpoint consumes just the expired segments, leaving
// other TTLs independent. Merging siblings itself never spends a hop.
func RouteRetCtx(retCtx string, originalCheckpoint bool) (checkpointed, forwarded string) {
	return RouteRetCtxWithDecision(retCtx, originalCheckpoint, nil)
}

// Tomislav-RetCtx: the SDK may independently accept each valid TTL-free truss.
// Existing TTL segments keep their countdown, including in mixed bundles.
// Nil accept retains the default TTL/legacy behavior; malformed or unknown
// TTL-free segments wait for an original checkpoint to absorb the whole bundle.
func RouteRetCtxWithDecision(retCtx string, originalCheckpoint bool, accept func(ReturnedCheckpoint) bool) (checkpointed, forwarded string) {
	if retCtx == "" || originalCheckpoint {
		return retCtx, ""
	}
	t, ok := decodeTruss(retCtx)
	if !ok || len(t.Segs) == 0 {
		return "", retCtx // Preserve opaque/legacy context until a checkpoint.
	}
	var emitted, pending []TrussSegment
	changedTTL := false
	for _, segment := range t.Segs {
		if segment.ReverseTTL != nil && *segment.ReverseTTL == 0 {
			emitted = append(emitted, segment)
			continue
		}
		if segment.ReverseTTL != nil {
			ttl := *segment.ReverseTTL - 1
			segment.ReverseTTL = &ttl
			changedTTL = true
		} else if accept != nil {
			checkpoint, known, err := decodeCheckpointSegment(segment)
			if known && err == nil && accept(checkpoint) {
				emitted = append(emitted, segment)
				continue
			}
		}
		pending = append(pending, segment)
	}
	// Tomislav-RetCtx: failed probability trials leave the carrier untouched;
	// avoid reserializing a potentially large fan-in bundle at every such hop.
	if len(emitted) == 0 && !changedTTL {
		return "", retCtx
	}
	return encodeTrussSubset(t, emitted), encodeTrussSubset(t, pending)
}

func encodeTrussSubset(envelope trussData, segments []TrussSegment) string {
	if len(segments) == 0 {
		return ""
	}
	envelope.Segs = segments
	// Tomislav-RetCtx: a checkpoint-only partition packs, and its fingerprints are
	// derived from each segment's own span ID on decode rather than transmitted.
	if packableRetCtx(envelope) {
		return encodePackedRetCtx(segments)
	}
	// Checkpoint origins belong only to their partition. Older ancestry-only
	// segments have envelope-level fingerprints, so retain those when present.
	var fingerprints []string
	for _, segment := range segments {
		switch segment.Kind {
		case SegPathCheckpoint, SegCallGraphCheckpoint, SegStructuralCheckpoint, SegVanillaCheckpoint:
		default:
			return encodeTruss(envelope)
		}
		if len(segment.Data) < 9 {
			return encodeTruss(envelope)
		}
		var id trace.SpanID
		copy(id[:], segment.Data[:8])
		fingerprints = append(fingerprints, id.String())
	}
	envelope.FP = strings.Join(fingerprints, ",")
	return encodeTruss(envelope)
}

// DecodeRetCtx exposes a truss's fingerprints, parent, Bloom geometry, and its
// AMQ segments (used by ancestry verifier / sample logger).
func DecodeRetCtx(s string) (fp, parent string, m uint64, k uint, amqs [][]byte) {
	t, ok := decodeTruss(s)
	if !ok {
		return
	}
	for _, seg := range t.Segs {
		if seg.Kind == SegAMQ {
			amqs = append(amqs, seg.Data)
		}
	}
	return t.FP, t.Par, t.M, t.K, amqs
}

// BuildRetCtx creates this node's truss: an ancestry Bloom (self + parent
// fingerprints) as one AMQ segment, concatenated with the children's merged truss.
func BuildRetCtx(ctx context.Context, traceCtx string, sc trace.SpanContext) string {
	m, k := rtBloom()
	bf := bloom.New(m, k)
	sid := sc.SpanID()
	bf.AddPrehashed(sid[:]) // this node's fingerprint
	parent := ""
	if traceCtx != "" {
		if cfg, _, err := GetSpanContext(traceCtx); err == nil {
			pid := cfg.SpanID
			bf.AddPrehashed(pid[:]) // parent's fingerprint (ancestor)
			if ParentIDEnabled() {
				parent = pid.String()
			}
		}
	}
	own := encodeTruss(trussData{
		FP:   sid.String(),
		Par:  parent,
		M:    m,
		K:    k,
		Segs: []TrussSegment{{Kind: SegAMQ, Data: bf.Bytes()}},
	})
	return MergeRetCtx(MergedChildren(ctx), own)
}

// MergeRetCtx concatenates two trusses on fan-in: append segment lists, join
// fingerprints, keep geometry. Empty inputs can pass through.
func MergeRetCtx(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	// Tomislav-RetCtx: two packed envelopes concatenate as byte regions. This is
	// the fan-in path, and it must not cost a decode-and-reserialize per hop.
	if merged, ok := mergePackedRetCtx(a, b); ok {
		return merged
	}
	ta, oka := decodeTruss(a)
	tb, okb := decodeTruss(b)
	if !oka {
		return b
	}
	if !okb {
		return a
	}
	m, k := ta.M, ta.K
	if m == 0 {
		m, k = tb.M, tb.K
	}
	fp := ta.FP
	if tb.FP != "" {
		if fp != "" {
			fp += ","
		}
		fp += tb.FP
	}
	par := ta.Par
	if par == "" {
		par = tb.Par
	}
	segs := append(append([]TrussSegment{}, ta.Segs...), tb.Segs...) // fresh slice, no aliasing
	return encodeTruss(trussData{FP: fp, Par: par, M: m, K: k, Segs: segs})
}

// Per-request fan-in accumulator
type retMergeKey struct{}
type retMerge struct {
	mu sync.Mutex
	s  string
}

func WithRetMerge(ctx context.Context) context.Context {
	return context.WithValue(ctx, retMergeKey{}, &retMerge{})
}

func AddToMerge(ctx context.Context, child string) {
	if m, ok := ctx.Value(retMergeKey{}).(*retMerge); ok {
		m.mu.Lock()
		m.s = MergeRetCtx(m.s, child)
		m.mu.Unlock()
	}
}

func MergedChildren(ctx context.Context) string {
	if m, ok := ctx.Value(retMergeKey{}).(*retMerge); ok {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.s
	}
	return ""
}

// toggles (runtime env)
func ReverseTrussEnabled() bool { return os.Getenv("REVERSE_TRUSS") == "on" }
func ParentIDEnabled() bool     { return os.Getenv("RT_PARENTID") == "on" }
func IsRoot() bool              { return os.Getenv("RT_ROOT") == "on" }

// ReverseTrussCheckpointKey marks a span carrying a consumed reverse truss.
// Bridge processors give these spans checkpoint priority at export.
//
// Tomislav-RetCtx: this is a WIRE key, written on every reverse checkpoint, so it
// is named like the forward path's `_br`/`_o`/`_d` rather than spelled out. The
// old "bridges.checkpoint" cost 18 bytes of key text per checkpoint span -- more
// than the whole packed payload now costs -- for a name no consumer parses.
const ReverseTrussCheckpointKey = "_rc"

// Tomislav-RetCtx: a "bridges.forward_up" marker used to be written beside the
// carrier on every span that passed a truss upward. Nothing ever read it -- not
// the audit, which checks the BRIDGES_RT counters instead -- so it was removed
// rather than shortened. Captures taken before 2026-09-19 still carry it;
// utils/retctx_wire.py knows the key so those traces stay interpretable.

// Counters, dumped periodically + on Ctrl-C / SIGTERM
var (
	rtCkpt, rtRecv, rtReject, rtLocal atomic.Uint64
	rtDumpOnce                        sync.Once
)

func installCounterDump() {
	rtDumpOnce.Do(func() {
		dump := func() {
			slog.Info("BRIDGES_RT",
				"checkpoints", rtCkpt.Load(),
				"received", rtRecv.Load(),
				"leaf_rejects", rtReject.Load(),
				"local_checkpoints", rtLocal.Load())
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		go func() { <-ch; dump(); os.Exit(0) }()
		sec := 15
		if n, err := strconv.Atoi(os.Getenv("RT_DUMP_SEC")); err == nil {
			sec = n
		}
		if sec > 0 {
			go func() {
				t := time.NewTicker(time.Duration(sec) * time.Second)
				for range t.C {
					dump()
				}
			}()
		}
	})
}

func CountCheckpoint()      { installCounterDump(); rtCkpt.Add(1) }
func CountTrussReceived()   { installCounterDump(); rtRecv.Add(1) }
func CountLeafReject()      { installCounterDump(); rtReject.Add(1) }
func CountLocalCheckpoint() { installCounterDump(); rtLocal.Add(1) }

var rtSample atomic.Uint64

func rtSampleN() uint64 {
	if n, err := strconv.Atoi(os.Getenv("RT_SAMPLE")); err == nil && n > 0 {
		return uint64(n)
	}
	return 500
}

// SampleLogCheckpoint logs ~1/RT_SAMPLE checkpoints with their original
// locations so the raw retCtx can be inspected using TestRTVerify.
func SampleLogCheckpoint(retCtx string) {
	if rtSample.Add(1)%rtSampleN() != 0 {
		return
	}
	fp, parent, m, k, amqs := DecodeRetCtx(retCtx)
	checkpoints, err := DecodeReturnedCheckpoints(retCtx)
	locations := make([]string, 0, len(checkpoints))
	for _, cp := range checkpoints {
		locations = append(locations, fmt.Sprintf("%s:%s@%d", cp.Kind, cp.SpanID, cp.Depth))
	}
	slog.Info("BRIDGES_CKPT", "fp", fp, "parent", parent, "m", m, "k", k,
		"amq_segments", len(amqs), "locations", locations, "decode_error", err, "retctx", retCtx)
}
