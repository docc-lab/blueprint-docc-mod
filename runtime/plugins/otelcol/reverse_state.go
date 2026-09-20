package otelcol

// Tomislav-RetCtx: the per-span reverse state table.
//
// The access pattern is store-once, load-once, delete-once, with a fresh key every
// span. sync.Map is the wrong structure for that: it is optimised for read-mostly
// maps with a stable key set, and a never-seen key always misses the lock-free
// read map, takes the dirty-map mutex, and periodically triggers a promotion that
// copies every live entry. On the busiest service that showed up as OnStart rising
// from 11.10s to 14.59s even though the rest of the service got faster.
//
// A fixed array of mutex-guarded maps keyed on a byte of the span ID removes almost
// all of that: span IDs are random, so the load spreads evenly, and each shard's
// lock is only ever held for a single map operation. The shards are padded to a
// cache line so neighbouring locks do not share one.

import (
	"sync"

	"go.opentelemetry.io/otel/trace"
)

// Power of two so the index is a mask. 64 shards keeps contention negligible at the
// concurrency these services run (composepost fans out to seven children per
// request across 40 cores) without holding many maps live.
const reverseStateShards = 64

type reverseStateShard struct {
	mu sync.Mutex
	m  map[trace.SpanID]*reverseSpanState
	// Pad past a 64-byte cache line so adjacent shards' mutexes do not share one
	// and turn independent locks into false sharing.
	_ [64 - (8+8)%64]byte
}

type reverseStateTable struct {
	shards [reverseStateShards]reverseStateShard
}

func newReverseStateTable() *reverseStateTable {
	t := &reverseStateTable{}
	for i := range t.shards {
		t.shards[i].m = make(map[trace.SpanID]*reverseSpanState)
	}
	return t
}

// XOR-fold all eight bytes rather than trusting any single one. OTel span IDs are
// random, but a table whose balance depends on WHERE a generator puts its entropy
// is a trap: pick the wrong byte and every span lands in one shard, which is worse
// than not sharding at all.
func (t *reverseStateTable) shard(id trace.SpanID) *reverseStateShard {
	h := id[0] ^ id[1] ^ id[2] ^ id[3] ^ id[4] ^ id[5] ^ id[6] ^ id[7]
	return &t.shards[h&(reverseStateShards-1)]
}

func (t *reverseStateTable) store(id trace.SpanID, state *reverseSpanState) {
	s := t.shard(id)
	s.mu.Lock()
	s.m[id] = state
	s.mu.Unlock()
}

func (t *reverseStateTable) load(id trace.SpanID) *reverseSpanState {
	s := t.shard(id)
	s.mu.Lock()
	state := s.m[id]
	s.mu.Unlock()
	return state
}

func (t *reverseStateTable) delete(id trace.SpanID) {
	s := t.shard(id)
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}
