package backend

// Reverse-truss (retCtx) support - split out of trace.go per Raja
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/trace"
)

// AMQ (Bloom) geometry, sourced from SDK:
// Reverse-truss ancestry filter must use the same Bloom geometry (m,k) as
// the forward bridge, so the two are inter-operable. The geometry is not a
// fixed constant:
//   1. SetRTBloomParams(m,k) is called by the otelcol bridge processor when it
//      sizes its own Bloom for the discovered cpd (checkpoint distance) -> reverse == forward geometry
//   2. Standalone fallback (synthetic trees, or any service without bridge
//      processor) reads it from env at startup:
//        RT_BLOOM_CAP expected #elements (default 8)
//        RT_BLOOM_FPR target FP rate (default 0.0001 == otelcol.DefaultBloomFPRate)
// Every truss records the (m,k) it was built with, so a verifier never has to guess.
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
}

const (
	SegAMQ = "amq"
	SegHash = "hash"
	SegOrdinal = "ordinal"
	SegEndEv = "ee"
	SegDelayEE = "dee"
)

type trussData struct {
	FP string `json:"fp"`
	Par string `json:"parent,omitempty"`
	M uint64 `json:"m,omitempty"`
	K uint `json:"k,omitempty"`
	Segs []TrussSegment `json:"segs,omitempty"`
}

func encodeTruss(t trussData) string {
	b, _ := json.Marshal(t)
	return base64.StdEncoding.EncodeToString(b)
}

func decodeTruss(s string) (trussData, bool) {
	var t trussData
	if s == "" {
		return t, false
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
		FP: sid.String(),
		Par: parent,
		M: m,
		K: k,
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
	s string
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
func ParentIDEnabled() bool { return os.Getenv("RT_PARENTID") == "on" }
func IsRoot() bool { return os.Getenv("RT_ROOT") == "on" }

// LeafReject: flagged leaf declines to checkpoint and pushes up. RT_LEAF_REJECT = rate
func LeafReject() bool {
	r, _ := strconv.ParseFloat(os.Getenv("RT_LEAF_REJECT"), 64)
	if r <= 0 {
		return false
	}
	return rand.Float64() < r
}

// IsLeaf returns whether this node is a flagged reject-leaf (RT_LEAF_REJECT > 0).
func IsLeaf() bool {
	r, _ := strconv.ParseFloat(os.Getenv("RT_LEAF_REJECT"), 64)
	return r > 0
}

var reverseTrussCount atomic.Uint64

// ReverseTrussCheckpoint: root always checkpoints; else RT_POLICY "2" depth-random, default round-robin
func ReverseTrussCheckpoint() bool {
	if IsRoot() {
		return true
	}
	switch os.Getenv("RT_POLICY") {
	case "2":
		return rand.Intn(rtDepth()) == 0
	default:
		return reverseTrussCount.Add(1)%uint64(rtDepth()) == 0
	}
}

func rtDepth() int {
	if n, err := strconv.Atoi(os.Getenv("RT_DEPTH")); err == nil && n > 0 {
		return n
	}
	return 3
}

// Counters, dumped periodically + on Ctrl-C / SIGTERM
var (
	rtCkpt, rtRecv, rtReject, rtLocal atomic.Uint64
	rtDumpOnce sync.Once
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

// SampleLogCheckpoint logs ~1/RT_SAMPLE checkpoints in decoded form so ancestry
// can be inspected and the raw retCtx fed to TestRTVerify.
func SampleLogCheckpoint(retCtx string) {
	if rtSample.Add(1)%rtSampleN() != 0 {
		return
	}
	fp, parent, m, k, amqs := DecodeRetCtx(retCtx)
	slog.Info("BRIDGES_CKPT", "fp", fp, "parent", parent, "m", m, "k", k, "amq_segments", len(amqs), "retctx", retCtx)
}
