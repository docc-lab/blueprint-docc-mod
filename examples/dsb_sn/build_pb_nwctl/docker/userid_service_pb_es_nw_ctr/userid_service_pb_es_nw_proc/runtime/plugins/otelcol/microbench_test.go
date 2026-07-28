package otelcol

// Nanosecond-scale microbenchmarks isolating the per-span PRIMITIVE cost each
// bridge adds — the Dapper/Hindsight "isolate the instrumentation primitive"
// evaluation, complementing the macro cycles/req ramp.
//
// Two sides are measured:
//
//   PROCESSOR side (this file, "Processor*" benchmarks): the work the SpanBridge
//   /PathBridge/CallGraphBridge processor does per span in OnStart — decode the
//   inbound `_br` baggage, rebuild the window state (bloom / hash-array / ordinal
//   chain), fold in this span, repack, and base64-encode the outbound payload.
//   This is the marginal bridge work vs vanilla (vanilla's processor does none of
//   it, so vanilla == 0 on this axis).
//
//   INSTRUMENTATION side ("Instr*" benchmarks): the per-CALL work the generated
//   OT client/server wrappers add (from plugins/opentelemetry ir_ot_client.go /
//   ir_ot_server.go templates). Baggage-map copy + attribute formatting are done
//   by EVERY variant incl. vanilla (shared); the bridge-specific additions are
//   the seqNum atomic, the context.WithValue node, and (SB only) the endEvents
//   drain/append under mutex. Those deltas are what the "Instr_*" cases isolate.
//
// HOW TO RUN (on a clock-locked node — min_perf_pct=100, 2.4 GHz — pin one core):
//
//   taskset -c 3 go test ./runtime/plugins/otelcol/ \
//       -run '^$' -bench 'Processor|Instr' -benchmem -count 20 \
//       | tee /users/tomislav/runs/microbench.txt
//   benchstat /users/tomislav/runs/microbench.txt
//
// With the clock pinned at 2.4 GHz, cycles/op = ns/op * 2.4 exactly, so these
// numbers convert directly into the same cycle units as the macro cycles/req.
// -benchmem's allocs/op & B/op are first-class outputs: they are the direct
// evidence for the allocation/GC story behind the post-knee cost (CGPB/SB
// allocate the growing hash-array / ordinal chain; PB's bloom is fixed-width).
//
// CAVEAT: these are warm-cache, single-goroutine, no-contention hot loops — a
// LOWER BOUND on the deployed per-span cost (the macro ramp additionally pays
// cache misses, scheduling, and GC). Report as the primitive's intrinsic cost;
// the macro cycles/req is the deployed cost.

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
)

// Package-level sinks defeat dead-code elimination (otherwise the compiler
// deletes the benchmarked work and you measure ~0.3 ns).
var (
	mbSinkBytes []byte
	mbSinkStr   string
	mbSinkInt   int
	mbSinkU64   uint64
	mbSinkBool  bool
	mbSinkCtx   context.Context
	mbSinkMap   map[string]string
	mbSinkMu    sync.Mutex
)

// mbStoreStr/mbStoreCtx let RunParallel goroutines commit a per-goroutine
// "keep" value to a package sink once (at loop end) without a data race —
// avoids the compiler eliding the benchmarked work while keeping -race clean.
func mbStoreStr(s string) { mbSinkMu.Lock(); mbSinkStr = s; mbSinkMu.Unlock() }
func mbStoreCtx(c context.Context) {
	mbSinkMu.Lock()
	mbSinkCtx = c
	mbSinkMu.Unlock()
}

// cpds under test — must bracket the macro sweep (cpd=2 cheap window, cpd=6 the
// long window where CGPB/SB accumulate the most per-span state).
var mbCPDs = []int{2, 6}

// setBloomForCPD sets the package bloom geometry (m,k) exactly as the PB/CGPB
// processors do at deploy: EstimateParameters over the PCRB window capacity.
func setBloomForCPD(cpd int) {
	m, k := bloom.EstimateParameters(uint(pbBloomCapacity(cpd)), DefaultBloomFPRate)
	BloomFilterM = m
	BloomFilterK = k
}

func mbSelfID(i int) [8]byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(0x1000+i))
	return b
}

func mbSpanHex(i int) string { return fmt.Sprintf("%016x", 0x2000+i) }

// bloomWith returns a window bloom holding n prehashed span IDs (raw 8-byte,
// matching the processor's AddPrehashed(sid8[:]) fast path).
func mbBloomWith(n int) *bloom.BloomFilter {
	bf := bloom.New(BloomFilterM, BloomFilterK)
	for i := 0; i < n; i++ {
		id := mbSelfID(i)
		bf.AddPrehashed(id[:])
	}
	return bf
}

// haWith builds a CGPB hash-array holding n entries (parent_span_id(8)||varint).
func mbHAWith(n int) []byte {
	var ha []byte
	for i := 0; i < n; i++ {
		ha = haAppendEntry(ha, mbSpanHex(i), i+1)
	}
	return ha
}

// ordGroupsWith builds an SB ordinal chain holding n entries, one per depthMod
// level (each carrying a full 8-byte parent fingerprint) — the deepest-window
// worst case where the chain is longest.
func mbOrdGroupsWith(n int) map[int][]ordEntry {
	g := make(map[int][]ordEntry, n)
	for i := 1; i <= n; i++ {
		var fp [8]byte
		binary.BigEndian.PutUint64(fp[:], uint64(0x3000+i))
		g[i] = []ordEntry{{ord: i, fp: append([]byte(nil), fp[:]...)}}
	}
	return g
}

var mbCkpt = [8]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04}

// ---------------------------------------------------------------------------
// PROCESSOR SIDE — full per-span OnStart core: decode inbound _br, rebuild
// window state, fold in self, repack, base64-encode outbound. This is the
// marginal per-span bridge cost over vanilla (vanilla does none of this).
// ---------------------------------------------------------------------------

func BenchmarkProcessorOnStartCore(b *testing.B) {
	for _, cpd := range mbCPDs {
		setBloomForCPD(cpd)
		n := pbBloomCapacity(cpd) // window entries held at the deepest position
		depth := cpd * 3
		self := mbSelfID(99)

		// PB: unpack -> NewFromBytes -> AddPrehashed -> pack -> encode
		b.Run(fmt.Sprintf("PB/cpd%d", cpd), func(b *testing.B) {
			inbound := encodeBR(packPathBridgeBR(depth-1, mbCkpt, mbBloomWith(maxInt(n-1, 0)).Bytes()))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				raw, _ := decodeBR(inbound)
				d, ck, pbloom, _ := unpackPathBridgeBR(raw)
				bf := bloom.NewFromBytes(pbloom, BloomFilterM, BloomFilterK)
				bf.AddPrehashed(self[:])
				mbSinkStr = encodeBR(packPathBridgeBR(d+1, ck, bf.Bytes()))
			}
		})

		// CGPB: PB work + haAppendEntry (the growing hash array)
		b.Run(fmt.Sprintf("CGPB/cpd%d", cpd), func(b *testing.B) {
			bloomLen := len(mbBloomWith(0).Bytes())
			inbound := encodeBR(packCGPRBBR(depth-1, mbCkpt, mbBloomWith(maxInt(n-1, 0)).Bytes(), mbHAWith(maxInt(n-1, 0))))
			psid := mbSpanHex(99)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				raw, _ := decodeBR(inbound)
				d, ck, pbloom, ha, _ := unpackCGPRBBR(raw, bloomLen)
				bf := bloom.NewFromBytes(pbloom, BloomFilterM, BloomFilterK)
				bf.AddPrehashed(self[:])
				ha2 := haAppendEntry(ha, psid, d+1)
				mbSinkStr = encodeBR(packCGPRBBR(d+1, ck, bf.Bytes(), ha2))
			}
		})

		// SB: unpack ordinal chain -> append self entry -> pack -> encode
		b.Run(fmt.Sprintf("SB/cpd%d", cpd), func(b *testing.B) {
			inbound := encodeBR(packSBridgeBR(depth-1, mbOrdGroupsWith(maxInt(n-1, 0)), []int{1, 2}, nil))
			var fp [8]byte
			binary.BigEndian.PutUint64(fp[:], 0x30FF)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				raw, _ := decodeBR(inbound)
				d, groups, ends, dee, _ := unpackSBridgeBR(raw)
				if groups == nil {
					groups = make(map[int][]ordEntry, 1)
				}
				dm := (d + 1) % cpd
				groups[dm] = append(groups[dm], ordEntry{ord: len(ends) + 1, fp: append([]byte(nil), fp[:]...)})
				mbSinkStr = encodeBR(packSBridgeBR(d+1, groups, ends, dee))
			}
		})
	}
}

// PROCESSOR SIDE — pack() alone (window state pre-built), isolating the
// serialization + base64 cost from the state-rebuild cost.
func BenchmarkProcessorPackOnly(b *testing.B) {
	for _, cpd := range mbCPDs {
		setBloomForCPD(cpd)
		n := pbBloomCapacity(cpd)
		depth := cpd * 3
		pbBloom := mbBloomWith(n).Bytes()
		ha := mbHAWith(maxInt(n-1, 0))
		groups := mbOrdGroupsWith(maxInt(n-1, 0))

		b.Run(fmt.Sprintf("PB/cpd%d", cpd), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mbSinkStr = encodeBR(packPathBridgeBR(depth, mbCkpt, pbBloom))
			}
		})
		b.Run(fmt.Sprintf("CGPB/cpd%d", cpd), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mbSinkStr = encodeBR(packCGPRBBR(depth, mbCkpt, pbBloom, ha))
			}
		})
		b.Run(fmt.Sprintf("SB/cpd%d", cpd), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mbSinkStr = encodeBR(packSBridgeBR(depth, groups, []int{1, 2}, nil))
			}
		})
	}
}

// PROCESSOR SIDE — individual primitives, for the finest-grain breakdown.
func BenchmarkProcessorPrimitives(b *testing.B) {
	setBloomForCPD(6)
	bb := mbBloomWith(pbBloomCapacity(6)).Bytes()
	self := mbSelfID(99)
	packed := packPathBridgeBR(18, mbCkpt, bb)
	enc := encodeBR(packed)

	b.Run("bloom_NewFromBytes", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			bf := bloom.NewFromBytes(bb, BloomFilterM, BloomFilterK)
			mbSinkBytes = bf.Bytes()
		}
	})
	b.Run("bloom_AddPrehashed", func(b *testing.B) {
		bf := bloom.NewFromBytes(bb, BloomFilterM, BloomFilterK)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			bf.AddPrehashed(self[:])
		}
	})
	b.Run("encodeBR", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mbSinkStr = encodeBR(packed)
		}
	})
	b.Run("decodeBR", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mbSinkBytes, mbSinkBool = decodeBR(enc)
		}
	})
	b.Run("haAppendEntry", func(b *testing.B) {
		ha := mbHAWith(4)
		pid := mbSpanHex(7)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mbSinkBytes = haAppendEntry(ha, pid, 3)
		}
	})
}

// ---------------------------------------------------------------------------
// INSTRUMENTATION SIDE — the per-call work the generated OT wrappers add.
// Common-to-all (incl. vanilla): baggage copy + attribute formatting.
// Bridge-only additions: seqNum atomic, context.WithValue, endEvents (SB).
// ---------------------------------------------------------------------------

// representative inbound baggage a wrapper copies each call (traceparent + _br +
// a couple app keys), matching the "make a copy to avoid mutating shared state"
// block at the top of every client template.
func mbBaggage() map[string]string {
	return map[string]string{
		"_br":         "AQIDBAUGBwgJCgsMDQ4PEA",
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		"user":        "alice",
		"region":      "us-east-1",
	}
}

func BenchmarkInstrBaggageCopy(b *testing.B) {
	up := mbBaggage()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bag := make(map[string]string, len(up))
		for k, v := range up {
			bag[k] = v
		}
		mbSinkMap = bag
	}
}

func BenchmarkInstrAttrFormat(b *testing.B) {
	// wrappers stringify a few span attributes into the outgoing baggage map
	// (strconv.FormatInt for ints, direct for strings).
	bag := make(map[string]string, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bag["k1"] = strconv.FormatInt(int64(i), 10)
		bag["k2"] = strconv.FormatInt(int64(i*7+3), 10)
	}
	mbSinkMap = bag
}

func BenchmarkInstrSeqAtomicAdd(b *testing.B) {
	// childCount/eventCount atomic increment (bridge client wrappers).
	var ctr atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbSinkInt = int(ctr.Add(1))
	}
}

func BenchmarkInstrCtxWithValue(b *testing.B) {
	// ctx = context.WithValue(ctx, "seqNum", seqNum) — allocates a context node.
	base := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbSinkCtx = context.WithValue(base, "seqNum", i)
	}
}

func BenchmarkInstrEndEventsAppend(b *testing.B) {
	// SB-only: append the completing span's seq to the shared endEvents slice
	// under childrenMutex (from the SB client-wrapper end path).
	var mu sync.Mutex
	ee := make([]int, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mu.Lock()
		ee = append(ee, i)
		mu.Unlock()
	}
	mbSinkInt = len(ee)
}

// ---------------------------------------------------------------------------
// PARALLEL / CONTENTION — the single-goroutine cases above measure the
// uncontended cost; these exercise the SHARED state under concurrency, which is
// where the real cost lives:
//   * the per-parent seqNum atomic is hammered by all concurrent sibling spans
//     -> cache-line bouncing;
//   * the per-request childrenMutex + endEvents slice serialize concurrent
//     child completions -> lock contention;
//   * the processor's alloc-heavy per-span work runs on every in-flight span at
//     once -> allocator + GC contention (the mechanism behind SB's post-knee
//     cost, where 18 allocs/span * high concurrency saturates the collector/GC).
// Run across -cpu 1,4,8,16,32 to see the contention curve; RunParallel spawns
// GOMAXPROCS goroutines sharing the state below.
// ---------------------------------------------------------------------------

func BenchmarkInstrContention(b *testing.B) {
	// One shared atomic counter, hit by all goroutines (worst-case sibling fan-out).
	b.Run("SeqAtomicShared", func(b *testing.B) {
		var ctr atomic.Uint64
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			var loc uint64
			for pb.Next() {
				loc = ctr.Add(1)
			}
			atomic.AddUint64(&mbSinkU64, loc)
		})
	})
	// One shared mutex + endEvents slice, appended by all goroutines.
	b.Run("EndEventsMutexShared", func(b *testing.B) {
		var mu sync.Mutex
		ee := make([]int, 0, 1<<16)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				mu.Lock()
				ee = append(ee, 1)
				mu.Unlock()
			}
		})
		mbSinkInt = len(ee)
	})
	// Full SB client-call hot path under contention: atomic + WithValue + mutex append.
	b.Run("SBCallShared", func(b *testing.B) {
		base := context.Background()
		var ctr atomic.Uint64
		var mu sync.Mutex
		ee := make([]int, 0, 1<<16)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			var keep context.Context
			for pb.Next() {
				seq := int(ctr.Add(1))
				keep = context.WithValue(base, "seqNum", seq)
				mu.Lock()
				ee = append(ee, seq)
				mu.Unlock()
			}
			mbStoreCtx(keep)
		})
	})
	// PB/CGPB client-call hot path under contention: atomic + WithValue (no mutex).
	b.Run("PBCGPBCallShared", func(b *testing.B) {
		base := context.Background()
		var ctr atomic.Uint64
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			var keep context.Context
			for pb.Next() {
				seq := int(ctr.Add(1))
				keep = context.WithValue(base, "seqNum", seq)
			}
			mbStoreCtx(keep)
		})
	})
}

// BenchmarkProcessorOnStartCoreParallel runs the alloc-heavy per-span bridge
// core concurrently (RunParallel) so the allocator + GC are exercised the way
// they are in a live service under load — the regime where SB's 18 allocs/span
// (cpd=6) turns into real GC pressure. Compare ns/op here vs the serial
// OnStartCore: a variant whose parallel ns/op degrades faster is alloc/GC-bound.
func BenchmarkProcessorOnStartCoreParallel(b *testing.B) {
	for _, cpd := range mbCPDs {
		setBloomForCPD(cpd)
		n := pbBloomCapacity(cpd)
		depth := cpd * 3
		self := mbSelfID(99)
		m, k := BloomFilterM, BloomFilterK

		b.Run(fmt.Sprintf("PB/cpd%d", cpd), func(b *testing.B) {
			inbound := encodeBR(packPathBridgeBR(depth-1, mbCkpt, mbBloomWith(maxInt(n-1, 0)).Bytes()))
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				var keep string
				for pb.Next() {
					raw, _ := decodeBR(inbound)
					d, ck, pbloom, _ := unpackPathBridgeBR(raw)
					bf := bloom.NewFromBytes(pbloom, m, k)
					bf.AddPrehashed(self[:])
					keep = encodeBR(packPathBridgeBR(d+1, ck, bf.Bytes()))
				}
				mbStoreStr(keep)
			})
		})
		b.Run(fmt.Sprintf("CGPB/cpd%d", cpd), func(b *testing.B) {
			bloomLen := len(mbBloomWith(0).Bytes())
			inbound := encodeBR(packCGPRBBR(depth-1, mbCkpt, mbBloomWith(maxInt(n-1, 0)).Bytes(), mbHAWith(maxInt(n-1, 0))))
			psid := mbSpanHex(99)
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				var keep string
				for pb.Next() {
					raw, _ := decodeBR(inbound)
					d, ck, pbloom, ha, _ := unpackCGPRBBR(raw, bloomLen)
					bf := bloom.NewFromBytes(pbloom, m, k)
					bf.AddPrehashed(self[:])
					ha2 := haAppendEntry(ha, psid, d+1)
					keep = encodeBR(packCGPRBBR(d+1, ck, bf.Bytes(), ha2))
				}
				mbStoreStr(keep)
			})
		})
		b.Run(fmt.Sprintf("SB/cpd%d", cpd), func(b *testing.B) {
			inbound := encodeBR(packSBridgeBR(depth-1, mbOrdGroupsWith(maxInt(n-1, 0)), []int{1, 2}, nil))
			var fp [8]byte
			binary.BigEndian.PutUint64(fp[:], 0x30FF)
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				var keep string
				for pb.Next() {
					raw, _ := decodeBR(inbound)
					d, groups, ends, dee, _ := unpackSBridgeBR(raw)
					if groups == nil {
						groups = make(map[int][]ordEntry, 1)
					}
					dm := (d + 1) % cpd
					groups[dm] = append(groups[dm], ordEntry{ord: len(ends) + 1, fp: append([]byte(nil), fp[:]...)})
					keep = encodeBR(packSBridgeBR(d+1, groups, ends, dee))
				}
				mbStoreStr(keep)
			})
		})
	}
}

// INSTRUMENTATION SIDE — composite marginal work a BRIDGE client wrapper adds
// over the vanilla wrapper for one outgoing call: atomic seq + WithValue
// (+ SB endEvents). Vanilla baseline = baggage copy only (shared, so it cancels
// in the delta and is benchmarked separately above).
func BenchmarkInstrBridgeCallDelta(b *testing.B) {
	base := context.Background()
	var ctr atomic.Uint64
	var mu sync.Mutex
	ee := make([]int, 0, 1024)

	b.Run("PB_CGPB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			seq := int(ctr.Add(1))
			mbSinkCtx = context.WithValue(base, "seqNum", seq)
		}
	})
	b.Run("SB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			seq := int(ctr.Add(1))
			ctx := context.WithValue(base, "seqNum", seq)
			mu.Lock()
			ee = append(ee, seq)
			mu.Unlock()
			mbSinkCtx = ctx
		}
	})
}
