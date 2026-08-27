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

// fixed AMQ shape so every node's ancestry filter is the same size
const (
	rtBloomN = 256 // TO-DO: Make it based on SDK parameters, source from SDK
	// On reject, we can have something like "something_bag.something"
	rtBloomP = 0.001
)

var rtBloomM, rtBloomK = bloom.EstimateParameters(rtBloomN, rtBloomP)

// reverseTruss carries a List of per-node ancestry filters (concatenated on fan-in)
// plus concatenated fingerprints and an optional parent id
type reverseTruss struct {
	FP     string   `json:"fp"`
	Parent string   `json:"parent,omitempty"`
	AMQs   [][]byte `json:"amqs,omitempty"` // TO-DO: Change to be an arbitrary "trusses" as a collection of arbitrary trusses, like fan-out, ordinals, end events
}

// EncodeRetCtx builds the reverse-truss string returned to caller
func EncodeRetCtx(fp, parent string, amqs [][]byte) string {
	b, _ := json.Marshal(reverseTruss{FP: fp, Parent: parent, AMQs: amqs})
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeRetCtx parses a reverse-truss string; empty/invalid -> zero vals
func DecodeRetCtx(s string) (fp, parent string, amqs [][]byte) {
	if s == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return
	}
	var rt reverseTruss
	if json.Unmarshal(raw, &rt) != nil {
		return
	}
	return rt.FP, rt.Parent, rt.AMQs
}

// BuildRetCtx creates this node's reverse-truss: an ancestry Bloom (its own
// fingerprint + its parent's fingerprint), concatenated with children's trusses
func BuildRetCtx(ctx context.Context, traceCtx string, sc trace.SpanContext) string {
	bf := bloom.New(rtBloomM, rtBloomK)
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
	own := EncodeRetCtx(sid.String(), parent, [][]byte{bf.Bytes()})
	return MergeRetCtx(MergedChildren(ctx), own)
}

// MergeRetCtx concatenates two reverse-trusses ("merge" API): Appends their AMQ
// lists and join fingerprints. Empty inputs can pass
func MergeRetCtx(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	fpA, pA, amqsA := DecodeRetCtx(a)
	fpB, _, amqsB := DecodeRetCtx(b)
	return EncodeRetCtx(fpA+","+fpB, pA, append(amqsA, amqsB...))
}

// per-request fan-in accumulator
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

// LeafReject: flagged leaf declines to checkpoint and pushes up. RT_LEAF_REJECT = rate
func LeafReject() bool {
	r, _ := strconv.ParseFloat(os.Getenv("RT_LEAF_REJECT"), 64)
	if r <= 0 {
		return false
	}
	return rand.Float64() < r
}

var reverseTrussCount atomic.Uint64

// ReverseTrussCheckpoint: root always checkpoints; else RT_POLICY "2" depth-random, default "1" round-robin
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

// counters, dumped on Ctrl-C / SIGTERM
var (
	rtCkpt, rtRecv, rtReject, rtLocal atomic.Uint64
	rtDumpOnce               sync.Once
)

func installCounterDump() {
	rtDumpOnce.Do(func() {
		dump := func() {
			slog.Info("BRIDGES_RT",
				"checkpoints", rtCkpt.Load(),
				"received", rtRecv.Load(),
				"leaf_rejects", rtReject.Load(),
				"local_checkpoints", rtLocal.load())
		}
		// final dump on shutdown
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		go func() { <-ch; dump(); os.Exit(0) }()
		// periodic dump so counters are readable mid-run via kubectl logs (RT_DUMP_SEC, default 15; 0=off)
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

func CountCheckpoint()    { installCounterDump(); rtCkpt.Add(1) }
func CountTrussReceived() { installCounterDump(); rtRecv.Add(1) }
func CountLeafReject()    { installCounterDump(); rtReject.Add(1) }

// IsLeaf reports whether this node is a flagged reject-leaf (RT_LEAF_REJECT > 0).
func IsLeaf() bool {
	r, _ := strconv.ParseFloat(os.Getenv("RT_LEAF_REJECT"), 64)
	return r > 0
}

func CountLocalCheckpoint() { installCounterDump(); rtLocal.Add(1) }

var rtSample atomic.Uint64

// SampleLogCheckpoint logs ~1/500 checkpoints in decoded form (fingerprints + AMQ
// segment count) so ancestry can be looked at & raw retCtx can be fed to rt_verify.
func SampleLogCheckpoint(retCtx string) {
	if rtSample.Add(1)%500 != 0 {
		return
	}
	fp, parent, amqs := DecodeRetCtx(retCtx)
	slog.Info("BRIDGES_CKPT", "fp", fp, "parent", parent, "amq_segments", len(amqs), "retctx", retCtx)
}
