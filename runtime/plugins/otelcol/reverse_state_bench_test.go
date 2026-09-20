package otelcol

// Tomislav-RetCtx: isolate the per-span state table. The access pattern is
// store-once, load-once, delete-once per span, with keys that are never reused --
// the worst possible shape for sync.Map, which is built for read-mostly maps with
// stable keys: every new key misses the read-only map, takes the dirty-map mutex,
// and eventually forces a promotion that copies the whole map.
//
// Run: RETCTX_COST=1 go test ./runtime/plugins/otelcol -run XXX \
//        -bench BenchmarkReverseState -benchmem -cpu 1,8,40

import (
	"math/rand"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// A per-goroutine PRNG, like the SDK's ID generator: a shared atomic counter would
// itself be the contention being measured, and its low entropy would pile every key
// into one shard.
func nextStateID(r *rand.Rand) trace.SpanID {
	var id trace.SpanID
	r.Read(id[:])
	return id
}

func BenchmarkReverseStateSyncMap(b *testing.B) {
	skipUnlessSDKCost(b)
	var m sync.Map
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			id := nextStateID(r)
			m.Store(id, &reverseSpanState{depth: 3})
			if v, ok := m.Load(id); ok {
				_ = v.(*reverseSpanState).depth
			}
			m.Delete(id)
		}
	})
}

func BenchmarkReverseStateSharded(b *testing.B) {
	skipUnlessSDKCost(b)
	t := newReverseStateTable()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			id := nextStateID(r)
			t.store(id, &reverseSpanState{depth: 3})
			if s := t.load(id); s != nil {
				_ = s.depth
			}
			t.delete(id)
		}
	})
}
