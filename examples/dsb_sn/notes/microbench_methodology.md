# Lab note: nanosecond-scale microbenchmarks of bridge per-span overhead

**Date:** 2026-07-09
**Artifact:** `runtime/plugins/otelcol/microbench_test.go`
**Runner:** `scratchpad/run_microbench_after_sweep.sh` (queued to fire post-sweep)
**Raw outputs:** `~/runs/microbench_serial.txt`, `~/runs/microbench_parallel.txt` (+ `.stat.txt`)

## 1. Purpose

The macro ramp (DSB-SN, 1k–4k rps) measures *deployed* cost but conflates the
bridge's per-span work with queueing, GC, network, and cross-service coupling —
and in an over-provisioned cluster it hides per-span CPU cost in spare cores
(see the spare-core confound). These microbenchmarks do the complementary
"isolate the instrumentation primitive" evaluation that Dapper and Hindsight
report: measure the *intrinsic* per-span cost of each bridge (PB / CGPB / SB)
directly, in isolation, in nanoseconds and allocations, with confidence
intervals — so we can (a) rank the bridges by intrinsic cost, (b) attribute
that cost to specific operations, and (c) corroborate the macro cycles/req
ordering with a clean, low-variance measurement.

Two sides of the per-span cost are measured separately:

- **Processor side** — what the SpanBridge/PathBridge/CallGraphBridge processor
  does per span in `OnStart`: decode the inbound `_br` baggage, rebuild the
  window state (bloom filter / hash-array / ordinal chain), fold in this span,
  repack, and base64-encode the outbound payload. Vanilla does none of this, so
  it is the *marginal* bridge cost over vanilla on this axis (vanilla ≡ 0).
- **Instrumentation side** — what the generated OT client/server wrappers
  (`plugins/opentelemetry` `ir_ot_client.go` / `ir_ot_server.go` templates) add
  per call. Baggage-map copy and attribute formatting are done by *every*
  variant incl. vanilla (shared, so they cancel in the delta); the
  bridge-specific additions are the `seqNum` atomic, the `context.WithValue`
  node, and (SB only) the `endEvents` drain/append under `childrenMutex`.

## 2. What the code tests, and how

All benchmarks live in an in-package test (`package otelcol`) so they call the
real unexported production functions (`packPathBridgeBR`, `packCGPRBBR`,
`packSBridgeBR`, `unpack*`, `haAppendEntry`, `encodeBR`/`decodeBR`, the
`bloom` package, the `ordEntry` type) — **not reimplementations**. The exact
wire-format code that runs in the deployed processor is what is timed.

### Processor benchmarks
- **`BenchmarkProcessorOnStartCore`** — the headline. Per iteration it runs the
  full per-span pipeline a bridge executes in `OnStart`:
  `decodeBR(inbound) → unpack*(…) → bloom.NewFromBytes + AddPrehashed`
  (PB/CGPB) or ordinal-chain append (SB) → `haAppendEntry` (CGPB) →
  `pack*(…) → encodeBR`. This is the marginal work vs. vanilla. Run for each
  variant at **cpd ∈ {2, 6}**.
- **`BenchmarkProcessorPackOnly`** — `pack* + encodeBR` with the window state
  pre-built, isolating serialization+base64 from the state-rebuild cost.
- **`BenchmarkProcessorPrimitives`** — each sub-op alone: `bloom.NewFromBytes`,
  `bloom.AddPrehashed`, `encodeBR`, `decodeBR`, `haAppendEntry`.
- **`BenchmarkProcessorOnStartCoreParallel`** — the same `OnStartCore` pipeline
  under `b.RunParallel` (GOMAXPROCS goroutines), to exercise the Go allocator +
  GC concurrently — the regime where SB's high alloc count turns into GC
  pressure.

### Instrumentation benchmarks
- **`BenchmarkInstrBaggageCopy` / `…AttrFormat`** — the shared per-call work
  (baggage map copy of a representative traceparent+`_br`+app-key map;
  `strconv.FormatInt` of a couple attrs). Common to all variants incl. vanilla.
- **`BenchmarkInstrSeqAtomicAdd` / `…CtxWithValue` / `…EndEventsAppend`** — the
  bridge-specific per-call additions, uncontended.
- **`BenchmarkInstrBridgeCallDelta`** — the composite marginal add a bridge
  client wrapper makes over vanilla (atomic + WithValue [+ SB endEvents]).
- **`BenchmarkInstrContention`** — the same shared state (**one** atomic, **one**
  `sync.Mutex` + `endEvents` slice) hammered by GOMAXPROCS goroutines, swept
  across `-cpu 1,2,4,8`, to measure lock/cache-line contention as concurrency
  rises. `SBCallShared` = full SB path (atomic+WithValue+mutex);
  `PBCGPBCallShared` = PB/CGPB path (atomic+WithValue, no mutex).

### Realistic inputs
Window state is built at the **deepest window position** (`pbBloomCapacity(cpd)`
= cpd−1 accumulated entries), i.e. the worst-case per-span payload just before a
checkpoint resets it. Bloom geometry (m,k) is set exactly as the processor sets
it at deploy (`bloom.EstimateParameters(pbBloomCapacity(cpd), DefaultBloomFPRate)`).
Span IDs / fingerprints are deterministic 8-byte values (reproducible; no RNG).

## 3. Harness & measurement hygiene

- **Go `testing.B`.** Iteration count auto-scaled; `-benchmem` reports
  `allocs/op` and `B/op` alongside `ns/op`.
- **Dead-code elimination defeated** with package-level sinks (`mbSink*`); every
  result is stored, or the compiler deletes the work and you measure ~0.3 ns.
  Parallel benchmarks keep a per-goroutine local and commit once at loop end via
  a mutex-guarded `mbStore*` (avoids a data race while staying `-race`-clean).
- **Setup excluded from timing** via `b.ResetTimer()` after building inputs; no
  `StopTimer`/`StartTimer` inside the hot loop (that itself costs tens of ns and
  would pollute ns-scale numbers).
- **Statistics:** `-count 20` (serial) / `-count 10` (parallel), summarized with
  `benchstat` → mean ± CI (Retro-style error bars). Serial CIs came out ±1%.

### Environment (critical for ns→cycles)
- **Pinned core:** `taskset -c 2` for the serial suite (GOMAXPROCS=1, no
  migration); `taskset -c 0-7` with `-cpu 1,2,4,8` for the parallel/contention
  suite.
- **Clock locked:** the host is pinned via the P-state pin **and**
  `min_perf_pct=100` (the HWP floor fix — without it idle cores fall to ~1.2 GHz;
  see the cpu-pin note). The runner re-asserts this and prints the verified idle
  MHz (observed 2325 MHz on an E5-2630 v3, 2.40 GHz base).
- **Consequence:** with the clock held, **cycles/op = ns/op × 2.4** (or ×2.325
  using the measured delivered clock), so the microbench converts directly into
  the same cycle units as the macro cycles/req axis.
- **Isolation from the macro sweep:** the runner *waits* for the ramp sweep to
  finish (`until grep "…rounds 1-3 complete"`) before running, so a CPU-heavy
  bench never perturbs the wrk load generator on the head node.

## 4. Collection procedure

`run_microbench_after_sweep.sh`:
1. block until the sweep's completion marker appears;
2. re-assert `min_perf_pct=100` + performance governor locally; log idle MHz;
3. `benchstat` install (best-effort);
4. **serial suite** — `taskset -c 2 … -cpu 1 -count 20` over the pack/encode/
   OnStartCore/primitive/uncontended-instrumentation benchmarks;
5. **parallel suite** — `taskset -c 0-7 … -cpu 1,2,4,8 -count 10` over the
   `Parallel` + `Contention` benchmarks;
6. `benchstat` each into `.stat.txt`.

## 5. Interpretation methodology

- **Per-span cost & ranking:** read `OnStartCore` ns/op (× clock → cycles/op) as
  the marginal per-span bridge cost; the vanilla processor does no such work, so
  these numbers *are* the overhead. Compare across variants and across cpd.
- **Allocation as the GC proxy:** `allocs/op` and `B/op` are first-class — they
  are the mechanism behind the macro post-knee CPU blowup (more allocs → more GC
  under load). Alloc counts are iteration-independent, so they are trustworthy
  even from a short run.
- **cpd-scaling:** compare cpd=2 vs cpd=6 per variant; a variant whose ns/op and
  allocs/op grow with cpd has per-window state that scales with window length.
- **Attribution:** allocation sites are attributed exactly with a memory profile
  (`-memprofile -memprofilerate 1`, then `go tool pprof -alloc_objects -list`),
  giving per-source-line allocation counts.
- **Contention:** read the `-cpu 1→8` curve; a flat curve = no shared-state
  contention, a rising curve = lock/cache-line contention that scales with
  concurrency.
- **Parallel scaling:** compare `OnStartCoreParallel` ns/op at 1 vs 8 cores; a
  variant that scales worse than linearly is allocator/GC-bound.

## 6. Results (this run)

Serial `OnStartCore` (ns/op, allocs/op, B/op):

| variant | cpd2 ns | cpd6 ns | cpd6 allocs | cpd6 B/op |
|--|--|--|--|--|
| PB   | 354 | 391  | 3  | 80  |
| CGPB | 484 | 729  | 6  | 392 |
| SB   | 516 | 1725 | 18 | 888 |

- **PB is cpd-flat** (fixed-width bloom, 3 allocs at both cpd); **CGPB scales**
  (HA chain); **SB scales hardest** (3.3× ns, 18 allocs at cpd6).
- **Allocation attribution (SB cpd6, pprof):** `unpackSBridgeBR` = 58% of allocs
  — `make([]ordEntry)` per depth-group (~4), the `map[int][]ordEntry` (~2), the
  per-entry `fp := append([]byte(nil), …)` copies (~2), `endEvents` (~1); the
  rest is `pack`(~2) + base64 `encode`(~2) + `decode`(~1) + loop body(~2). PB is
  3 because its bloom decodes as a sub-slice (no rebuild, no per-element alloc).
- **Instrumentation (uncontended):** atomic 6 ns, `WithValue` 84 ns / 1 alloc,
  endEvents append 25 ns; bridge marginal add over vanilla ≈ 90 ns.
- **Contention (1→8 cores):** shared atomic 6→22 ns; shared mutex+endEvents
  27→550 ns; SB call 104→943 ns; PB/CGPB call flat 89→111 ns (no mutex).
- **Parallel scaling (cpd6, 1→8 cores):** PB 400→80, CGPB 758→270, SB 1809→648
  (SB scales worst — allocator/GC-bound).

## 7. Threats to validity / caveats

- **Warm-cache lower bound.** These are single-goroutine (serial) or
  tight-loop (parallel) hot loops with warm caches and no cross-service
  coupling. They measure the *intrinsic* primitive cost — a **lower bound** on
  deployed per-span cost. The macro cycles/req is the deployed number; the two
  are reported as a pair (Dapper/Hindsight do the same).
- **Contention is a worst case, not a deployed number.** `InstrContention`
  shares **one** mutex among all goroutines in a **100%-lock-duty** loop. In the
  real app `childrenMutex`/`endEvents` are **per-request**, so contention only
  arises among concurrently-created sibling children of the *same* parent; goroutines
  serving *different* requests use *different* mutexes (no contention), and real
  children spend almost all their time in the downstream RPC, not the lock. So
  the 943 ns / 8× figure is an upper bound on a *latent* liability, not a
  measured deployed cost; whether it ever binds depends on whether DSB service
  handlers issue downstream calls concurrently vs. sequentially (unverified).
- **Head-node vs k8s-node clock.** The bench runs on the head node; its clock is
  locked the same way, but it is a distinct machine from the app nodes — cycle
  numbers are self-consistent for the bench, not literally the app nodes' cycles.
- **`-memprofilerate 1`** perturbs timing (used only for the attribution run, not
  the timing run); the profiled run also included the `Parallel` variant matched
  by the regex, so absolute object counts span two benchmarks — ratios and
  per-line attribution are the interpretable output, not the raw totals.

## 8. Reproduction

```
taskset -c 2 go test ./runtime/plugins/otelcol/ -run '^$' \
  -bench 'ProcessorOnStartCore/|ProcessorPackOnly|ProcessorPrimitives|Instr' \
  -benchmem -cpu 1 -count 20 | tee ~/runs/microbench_serial.txt
taskset -c 0-7 go test ./runtime/plugins/otelcol/ -run '^$' \
  -bench 'Parallel|Contention' -benchmem -cpu 1,2,4,8 -count 10 \
  | tee ~/runs/microbench_parallel.txt
benchstat ~/runs/microbench_serial.txt
# allocation attribution:
go test ./runtime/plugins/otelcol/ -run '^$' -bench 'OnStartCore/SB/cpd6' \
  -memprofile /tmp/sb.prof -memprofilerate 1 -benchtime 20000x
go tool pprof -alloc_objects -list 'unpackSBridgeBR$' /tmp/sb.prof
```
(Run only when the head node is free — a busy bench perturbs the wrk load gen.)
