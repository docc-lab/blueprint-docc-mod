# Bloom optimization: use the span ID directly as the hash (skip MurmurHash)

**Date:** 2026-06-26

## Context
Clean same-revision overhead sweep ranks per-span cost **sb < pb < cgpb** (flat-region
mean RT: sbridge 29.1, pbridge 30.0, cgpb 33.2 ms). Code comparison (agent audit of
`{sb,pb,cgpb}_processor.go` + `bloom.go`) showed the difference is entirely the **bloom
filter**, which only PB/CGPB touch: per non-checkpoint span they do `NewFromBytes`
(alloc+copy) + `bf.Add` = **2× MurmurHash3-128 over the span ID** + two `Bytes()` copies
(CGPB adds a hash-array on top). SB has no bloom — varint ordinals only — hence cheapest.

## Insight / hypothesis
OTel span IDs are 8 bytes straight from the SDK IDGenerator — **already uniformly random**.
A bloom only needs uniform, independent, deterministic hash positions. Running an already-
random key through MurmurHash is **redundant whitening**. So derive the double-hashing base
words `(h1,h2)` directly from the span-ID bytes and skip murmur entirely. Same idea as the
fingerprint in modern AMQ filters (cuckoo/quotient).

## Implementation
`runtime/plugins/bloom/bloom.go` — added get/set ops that do NOT re-hash:
- `AddPrehashed(data)` / `TestPrehashed(data)` → `splitHashes(data)` packs the first half of
  the bytes into `h1`, second half into `h2` (8-byte ID → two independent uniform 32-bit
  words), then the existing `pos_i = (h1 + i·h2) mod m` loop (factored into `setBits`/`testBits`).
- `Add`/`Test` (murmur path) unchanged in behavior — refactored to share the same loop.
- Unit test: `bloom_prehashed_test.go`.

## Verification (falsifying tests)
- **No false negatives** — roundtrip on bridge-sized filter (capacity 5, 1% target) passes.
- **FPR unchanged** → zero fidelity cost: prehashed **0.0625** vs murmur **0.0579** (same
  within noise; both inflated only because capacity is tiny). This is the key result — the
  optimization does NOT change reconstruction accuracy.
- **~31% faster**: `AddPrehashed` **115 ns/op** vs `Add` **167.7 ns/op** (~53 ns/span saved),
  **0 allocs** both. The murmur hashing was ~53 ns/span; that is what this removes.

## Caveats
1. Pass the **RAW 8-byte SpanID** (`SpanContext().SpanID()[:]`), NOT its hex string (hex = 4
   bits entropy/byte).
2. Use the same derivation on BOTH sides — emit (`AddPrehashed`) AND reconstruction membership
   query (`TestPrehashed`).
3. Removes the **hashing**, not the bloom **allocations** (`NewFromBytes` / `Bytes()` copies).
   So it narrows but won't close the PB↔SB gap; closing it further needs buffer pooling.
4. 64 bits of entropy is ample for the tiny bridge blooms (k single-digit). Breaks only with a
   non-random IDGenerator (exotic).

## Status / next
Bloom package upgraded + unit-tested. NOT yet wired into the processors. Next: swap
`bf.Add([]byte(spanID))` → `bf.AddPrehashed(sid[:])` (and `Test`→`TestPrehashed` on the
reconstruction side) in `pb_processor.go` + `cgpb_processor.go`, rebuild, re-measure PB
overhead — quantifies how much of PB's bloom tax was hashing vs allocation. See
[[breadcrumb_carrier_finding]], [[bridges_overhead_baseline_confound]].
