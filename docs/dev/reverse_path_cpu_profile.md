# Where the response path's CPU goes (2026-09-19)

Tomislav-RetCtx. Measured, not argued: this replaces three hypotheses that turned
out to be wrong.

## The question

The no-work campaigns show response-path propagation costing throughput. Fitting
application CPU against delivered rps over offered 500..5000 (PB, admission
collectors, R^2 > 0.97):

| build | marginal us/req | fixed cores |
|---|---|---|
| `REVERSE_TRUSS=off` | 3183 | 3.89 |
| on, JSON envelope | 3816 (+20 %) | 4.90 (+26 %) |
| on, packed envelope | 3683 (+16 %) | 4.70 (+21 %) |

The packed wire format (see `reversetruss_packed.go`) accounts for about a fifth
of it. The question was what carries the rest.

## What it is not

Each ruled out by measurement, in the order they were tried and discarded:

- **Wire bytes / serialization CPU.** Packing cut one checkpoint from 160 to 42
  bytes and a five-hop merge from 91.8 us to 7.1 us, and bought +3.5..4.1 %
  throughput. Real, but a fifth of the gap.
- **Collector refusals back-pressuring the application.** At offered 500..3000
  the packed build refuses nothing and still costs +1100 us/req.
- **`BuildRetCtx`'s per-span Bloom.** Dead code; no callers.
- **Extra baggage.** `__rt_depth` is only ever read, never written outbound;
  `AddBaggageToTraceContext` is identical in both modes.
- **Cluster drift.** Two reverse-ON campaigns three days apart differ by
  0.06..1.48 cores; ON vs OFF differs by 1.81..3.36 at every rate.
- **A microbenchmark of the reverse code.** A real recording span through the
  wrapper measured under 1 us/span against ~38 us/span deployed. The benchmark
  was the wrong instrument -- few attributes, single goroutine, no GC
  amplification -- not a refutation. Recorded here because it briefly convinced
  me the attribute hypothesis was dead when it was in fact correct.

## Method

`runtime/plugins/otelcol/pprof.go` adds a listener gated on `BRIDGES_PPROF`, so a
measurement build is bit-identical to a normal one unless the variable is set.
Two single-rate runs (offered 4000, held 120 s) with the response path on and
off, delivering 3989 and 3991 rps respectively, and a 45 s CPU profile from four
services spanning the topology.

Roots: `retctx-prof-{on,off}-multi-20260919T200343Z`.

## Result

45 s of CPU per service, reverse ON minus reverse OFF:

| service | role | off | on | delta | OnStart | PrepareCkpt | Attributes | ReadRevBag | SetAttrs | mallocgc | GC mark |
|---|---|---|---|---|---|---|---|---|---|---|---|
| composepost | fan-in, 7 children | 220.0 | 258.1 | **+17.3 %** | +11.10 | +7.04 | +6.10 | +1.95 | +4.14 | +10.44 | +6.18 |
| text | mid-tier, 2 clients | 104.8 | 119.2 | **+13.8 %** | +6.70 | +3.96 | +2.93 | +0.87 | +2.06 | +2.82 | +4.63 |
| user | pure leaf | 32.1 | 38.1 | **+18.7 %** | +1.37 | +1.69 | +1.27 | +0.30 | +0.69 | +1.29 | +2.90 |
| post-storage | pure leaf | 34.5 | 41.6 | **+20.8 %** | +1.30 | +1.56 | +1.21 | +0.30 | +0.61 | +0.89 | +3.58 |

Two things stand out.

**Allocation dominates.** `mallocgc` plus background GC marking is 43 % of the
delta on composepost and **70 % on the leaves** (user: 4.19 s of 6.0 s). The
response path's cost is mostly the garbage it makes, not the work it does.

**`OnStart` is the largest named cost at every tier**, and it runs on every span
including ones that never see a returned truss.

Callers of `recordingSpan.Attributes()` on composepost (6.10 s):
`ReadReverseBaggage` 1.87 s (31 %), `OnStart` 1.25 s, `PrepareCheckpoint` 1.12 s,
`spanHasChildren` 0.41 s, generated wrappers ~1.29 s.

## Why: span attributes are being used as an intra-process message bus

`recordingSpan.Attributes()` is not a cheap read. It takes the span mutex and
runs `dedupeAttrs()`, which allocates a map and walks every attribute, on every
call. `SetAttributes` takes the same lock.

The reverse path reads and writes that structure repeatedly to pass values
between functions one stack frame apart:

- `OnStart` scans for `AttrBREmit` -- set moments earlier by the bridge processor
  it wraps, which still has the value.
- `OnStart` calls `p.scheduled(span)`, which scans again for `AttrBagPrio` --
  also just set by that same processor.
- `OnStart` writes `__bag.__rt_depth` (via `strconv.FormatUint`, one string
  allocation per span) and `__bag.rev_scheduled`, neither of which is ever
  exported; they exist only so `PrepareCheckpoint` and `isPathCheckpoint` can
  read them back later, each read being another locked scan and another dedupe
  map.
- `PrepareCheckpoint` scans, then calls `spanHasChildren`, which scans again.
- `ReadReverseBaggage` scans a third time for a value `PrepareCheckpoint` wrote
  microseconds earlier. It is the single largest consumer of attribute scans.

None of this is the reverse protocol. It is the OTel SDK's concurrency-safe
attribute machinery -- lock, dedupe pass, map allocation, and the GC that follows
-- used as a channel between callers in the same goroutine.

## Fix (not yet implemented)

Pure refactor, no protocol or semantic change:

1. Carry the reverse state beside the bridge processor's existing per-span
   bookkeeping (`hpBuf`/`lpBuf`/`sbBufEntry`) instead of on span attributes.
2. Fetch the attribute slice once per span and pass it down; `spanHasChildren`
   and `isScheduledPathCheckpoint` take the slice rather than re-fetching.
3. `PrepareCheckpoint` returns the routed value so `ReadReverseBaggage`
   disappears from the hot path.
4. Depth as an int attribute, not a formatted string.

Expected from the table above: most of the attribute traffic and the allocation
behind it, taking the reverse overhead from ~+17 % to roughly +8..10 % of
application CPU on composepost, and proportionally more on the leaves where GC is
70 % of the delta. Verify by re-profiling at the same offered 4000.

It will not make the response path free: `SetAttributes` (+4.14 s) and
`tracer.Start` are real per-span work that the reverse path genuinely adds.
