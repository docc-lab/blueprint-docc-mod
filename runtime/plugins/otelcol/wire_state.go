package otelcol

// Tomislav-RetCtx: per-span bridge wire state, kept beside the span instead of on the OTel
// attribute store (same approach and rationale as reverse_state.go for the response path).
//
// A deployed CPU profile of the hotel frontend at 12k rps (2026-09-23) put the ranged path
// bridge at +25 s of CPU per 30 s over vanilla. Most of what OnStart and OnEnd paid for was
// attribute bookkeeping, not the bridge protocol: OnStart wrote five attributes, of which only
// `__bag._br` has to be on the span (the generated RPC wrapper propagates `__bag.`-prefixed
// attributes as baggage); `_br` and `_d` were base64-encoded only to be decoded back to raw
// bytes at export, `depth` and `__bag.prio` never reach the wire, and every reader
// (reverse OnStart, PrepareCheckpoint, classification, export) paid a locked dedupe of the
// whole attribute set to find them. The state now lives here as raw bytes: stored once at
// OnStart, read by the response path, taken (and so freed) at OnEnd.

import (
	"encoding/binary"
	"sync"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// bridgeWire is one span's bridge state that never has to be on the OTel span.
type bridgeWire struct {
	emit     []byte // wire payload for `_br` (kept on checkpoints and leaves), raw bytes
	depth    int    // absolute depth; `_d` on the wire for interior non-checkpoints
	priority bool   // scheduled checkpoint (formerly the __bag.prio attribute == 1)
	prop     string // outgoing `_br` baggage value (formerly the __bag._br attribute)
	// SB only: non-checkpoints also carry `_o`, this span's child ordinal.
	structural bool
	ordinal    int
	// rev is the response path's per-span state (reverse_checkpoint.go) for bridge spans, so
	// the reverse wrapper needs no table of its own for them.
	rev reverseSpanState
}

// bridgeWires is process-wide: one bridge processor per service, and span IDs are unique.
var bridgeWires = newWireTable()

// The RPC wrappers merge a bridge span's outgoing `_br` from here (backend.AppendSpanBaggage)
// before applying the span's `__bag.` attributes, which bridges no longer set.
func init() {
	backend.RegisterSpanBaggageSource(func(sc trace.SpanContext, baggage map[string]string) {
		if w := bridgeWires.load(sc.SpanID()); w != nil {
			baggage[BaggageBRKey] = w.prop
		}
	})
}

// wireTable is a sharded map keyed by span ID for store-once / load / take-once state. (Not a
// generic type: the Blueprint compiler parses this package and its Go parser does not accept
// type parameters.)
const wireTableShards = 64

type wireTableShard struct {
	mu sync.Mutex
	m  map[trace.SpanID]*bridgeWire
	_  [64 - (8+8)%64]byte // keep neighbouring shard locks off one cache line
}

type wireTable struct {
	shards [wireTableShards]wireTableShard
}

func newWireTable() *wireTable {
	t := &wireTable{}
	for i := range t.shards {
		t.shards[i].m = make(map[trace.SpanID]*bridgeWire)
	}
	return t
}

func (t *wireTable) shard(id trace.SpanID) *wireTableShard {
	h := id[0] ^ id[1] ^ id[2] ^ id[3] ^ id[4] ^ id[5] ^ id[6] ^ id[7]
	return &t.shards[h&(wireTableShards-1)]
}

func (t *wireTable) store(id trace.SpanID, v *bridgeWire) {
	s := t.shard(id)
	s.mu.Lock()
	s.m[id] = v
	s.mu.Unlock()
}

func (t *wireTable) load(id trace.SpanID) *bridgeWire {
	s := t.shard(id)
	s.mu.Lock()
	v := s.m[id]
	s.mu.Unlock()
	return v
}

// take loads and deletes.
func (t *wireTable) take(id trace.SpanID) *bridgeWire {
	s := t.shard(id)
	s.mu.Lock()
	v := s.m[id]
	delete(s.m, id)
	s.mu.Unlock()
	return v
}

// appendBridgeWire adds the span's wire attribute from its bridge wire state: `_br` (the raw
// emit payload) on checkpoints and leaves, `_d` (the depth varint) on interior non-checkpoints.
func appendBridgeWire(out []*commonpb.KeyValue, w *bridgeWire, highPriority bool) []*commonpb.KeyValue {
	if w == nil {
		return out
	}
	if highPriority {
		return append(out, newBytesKV(AttrBREmit, w.emit, 0))
	}
	if w.structural {
		out = append(out, newBytesKV(AttrO, nil, w.ordinal))
	}
	return append(out, newBytesKV(AttrD, nil, w.depth))
}

// bytesKV is a bytes-valued KeyValue with its value wrappers (and room for a varint) in one
// allocation instead of four.
type bytesKV struct {
	kv  commonpb.KeyValue
	av  commonpb.AnyValue
	bv  commonpb.AnyValue_BytesValue
	buf [binary.MaxVarintLen64]byte
}

// newBytesKV returns key=raw, or key=uvarint(n) when raw is nil.
func newBytesKV(key string, raw []byte, n int) *commonpb.KeyValue {
	b := &bytesKV{}
	if raw == nil {
		raw = b.buf[:binary.PutUvarint(b.buf[:], uint64(n))]
	}
	b.bv.BytesValue = raw
	b.av.Value = &b.bv
	b.kv.Key, b.kv.Value = key, &b.av
	return &b.kv
}
