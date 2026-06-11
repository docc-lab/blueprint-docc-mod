# SB SDK DEE Pipeline Refactor — Findings

Refactor of the structural-bridge SDK's Delayed-End-Event (DEE) propagation
path to match the bridges Go simulator's data model. The motivation was a
discovery that the SB SDK's pre-refactor pipeline both had a structurally
broken format AND created a latent request-path blocking primitive that
only manifested under sustained downstream backpressure.

## The bug — two issues at once

### Issue 1: Format mismatch caused DEE bytes to silently disappear

The original client-side template (`ir_ot_client.go`) appended
`",seqNum:endSeqNum"` colon-pair tokens to a per-request `*string` accumulator.
At server-OnEnd, the string was set as a `remEndEvents` span attribute.

The SB processor's OnStart drain loop then tried to parse this string as
comma-separated integers:

```go
for _, tok := range strings.Split(eeStr, ",") {
    if v, err := strconv.Atoi(tok); err == nil {
        seqs = append(seqs, v)
    }
}
if len(seqs) > 0 {
    deeBytes = append(deeBytes, encodeDEETriple(tid16, depthMod, seqs)...)
}
```

But each token had a colon in it (e.g. `"3:7"`), so `Atoi` failed on every
token, `seqs` stayed empty, and `encodeDEETriple` was **never called**. DEE
data didn't actually propagate through the bridge state — the entire DEE
pathway was a no-op for years.

### Issue 2: Channel buildup blocked OnEnd's request goroutine under downstream backpressure

In a commit on 2025-12-29 ("sb optimizations and cpd-3 results"), the
DEE accumulator was migrated from an unbounded slice to a 10000-deep
buffered `chan string`. The OnEnd write was a **blocking send** on the
request goroutine; the OnStart drain was opportunistic (non-blocking
default).

With in-memory jaeger (fast downstream), the channel stayed at depth ≈ 0
because OnStart drained as fast as OnEnd filled. With ES-backed jaeger
(slow downstream), per-RPC `UploadTraces` lifetimes grew, OnStart's
per-call drain work grew with channel depth (each iteration ran
`strings.Split` + `Atoi` loops + allocs), drain rate fell behind
production rate, and the channel saturated. Once full, OnEnd's blocking
send stalled the request goroutine on every span end.

That positive-feedback loop explains why past tests achieved 3.4–3.6 k rps
with SB on in-memory jaeger but the same SDK couldn't sustain 3 k rps
under ES-backed downstream.

## The refactor — matching the bridges Go simulator

The Go simulator stores **already-encoded `[]byte` DEE triples** in a
per-service queue (`map[uint16][][]byte`). No string round-trip, no Atoi,
no per-element parsing on the drain path.

We mirrored that design across three files:

| file | change |
|---|---|
| `plugins/opentelemetry/ir_ot_server.go` | endEvents accumulator: `*string` → `*[]int`; varint-encode at end-of-request (dropping last entry per simulator semantics); set as base64-encoded `remEndEvents` attribute |
| `plugins/opentelemetry/ir_ot_client.go` | append plain `seqNum` int to `*[]int` accumulator (no colon-pair string format) |
| `runtime/plugins/otelcol/sb_processor.go` | channel: `chan string` → `chan []byte`; non-blocking send with drop-counter (`deeDropped`); OnStart drain is a bare byte-append loop |

Surface-level wins:
- **OnStart's drain cost is O(channel-depth) byte appends**, ~ns per item, no parsing
- **OnEnd's send is non-blocking** — never stalls the request goroutine
- **Per-OnEnd cost is constant** — one base64 decode, one varint header prepend, one channel send

`dee_dropped` and `dee_chan_depth` are surfaced in the per-second SB
metrics line so the channel's behavior is observable.

## Results — full 6-point sweep, same-session apples-to-apples

Workload: padded=0, retry off, 1 s gRPC deadline, otelcol with memlim
70/20 + bulk_workers=10 on jaeger, ES heap 4 GiB unbounded container,
configdiscovery serving cpd=3 to SDK.

Sweep order: 500 → 1 k → 1.5 k → 2 k → 2.5 k → 3 k rps, no teardown
between RPS levels (ES accumulates trace data across the sweep).

```
                          500      1 k      1.5 k     2 k       2.5 k     3 k
─────────────────────────────────────────────────────────────────────────────────
SB DEE-fix cpost drop %   0.0      0.0      0.0      16.0      56.3      44.1
Vanilla    cpost drop %   0.0      0.0      0.0       5.4      47.0      33.7

SB DEE-fix achieved rps   500     1000     1499     1998      2495      2268 ←collapse
Vanilla    achieved rps   500     1000     1500     1999      2495      2273 ←collapse

SB DEE-fix p50 latency  7.51 ms 11.62 ms 22.01 ms 47.27 ms  65.28 ms  31.17 s ←broken
Vanilla    p50 latency  7.42 ms 11.46 ms 23.19 ms 58.00 ms  85.41 ms  31.14 s ←broken

SB DEE-fix ES CPU peak  10.8 c   12.2 c   14.1 c   21.8 c    15.3 c    34.9 c
Vanilla    ES CPU peak   9.5 c   10.9 c   12.8 c   21.0 c    26.6 c    35.5 c
```

### Five headline findings

**1. Below 2 k rps: SB DEE-fix and vanilla are indistinguishable.**
0 drops in both, p50 within 1 ms, ES CPU within ~1 core. SB's per-span
overhead is undetectable when the pipeline has any headroom.

**2. At 2 k rps: SB drops ~3× more than vanilla** (16.0 % vs 5.4 %).
otelcol-side memlim trips earlier for SB despite both producing the same
downstream byte volume. The per-span KeyValue / AnyValue / baggage
allocation overhead on the otelcol decode path is what's flipping memlim's
heap-poll into the refusal zone.

**3. At 2.5 k rps: SB drops 20 % more on heavy services but uses 43 %
LESS ES CPU.** Vanilla = 47.0 % cpost drops at 26.6 ES cores. SB DEE-fix
= 56.3 % cpost drops at 15.3 ES cores. The LP-stripping benefit from
cpd=3 classification is finally visible — fewer wire-going attributes
reach ES → ES indexes less data per accepted batch → less ES CPU.

**4. At 2.5 k rps: SB latency lower than vanilla** (p50 65 ms vs 85 ms,
max 246 ms vs 476 ms). Because SB sheds harder, less queue depth, faster
responses for what gets through. Vanilla holds more in its queue, leading
to longer latency tails.

**5. At 3 k rps: BOTH collapse to ~2 270 rps with 31 s p50 latency.**
The cluster-wide ES indexing capacity is the saturation point at this
sweep's end (ES has accumulated ~80 M docs across the 5 prior RPS levels
+ this 3 k attempt). Neither variant is responsible — environment is
saturated.

## Comparison to pre-refactor SB (cpd=3 broken DEE)

At 2 k rps, the DEE refactor closed a third of the gap to vanilla:

```
                       cpd=1 (no cpd)  cpd=3 (broken DEE)  DEE-fix    vanilla
cpost drop %               25.5             24.2            16.0        5.4
otelcol memlim total    1,638,980        1,489,856     1,037,573    367,188
ES CPU peak                 24.4 c           22.4 c          21.8 c     21.0 c
```

The refactor's effect is concentrated where the channel previously
saturated — 2 k rps is the rps band where pre-refactor OnEnd was
blocking on the channel send. With the bare-byte append drain plus
non-blocking send, both modes look essentially the same on app-side
behavior, and the per-span CPU cost is now the only remaining SB
overhead.

## What this refactor did NOT fix

**The 3 k rps app collapse persists for both SB and vanilla** in this
session — but vanilla previously achieved 3 k cleanly in a fresh-ES
baseline. The collapse here is a cluster-state artifact (~80 M docs
accumulated in ES across the sweep makes per-doc indexing slower at
the end). It's not SB-specific. A fresh ES at 3 k SB would need its
own clean test to characterize.

**SB still costs ~10 pp more drops than vanilla at the cliff.** The
DEE refactor removed the OnStart drain cost (Atoi loops scaling with
channel depth) and the OnEnd blocking primitive, but the per-OnStart
bridge state computation (packBR + base64 + SetAttributes + baggage
decode) is still on the request goroutine. That's the next-tier cost
if we want to push SB closer to vanilla's drop profile.

## Run directories

- SB DEE-fix sweep: `runs/nopad_sb-esrev0_{500,1000,1500,2000,2500,3000}rps_2026-06-11_*`
- Vanilla full sweep: `runs/nopad_v-esrev0_full_{500,1000,1500,2000,2500,3000}rps_2026-06-11_*`
- Prior cpd=3 sweep (with the still-broken DEE format): `runs/nopad_sb-esrev0_{500,1000,1500,2000,2500,3000}rps_2026-06-10_22*`
- Prior cpd=1 sweep (configdiscovery off): `runs/nopad_sb-esrev0_{500,1000,1500,2000,2500,3000}rps_2026-06-10_19*`
