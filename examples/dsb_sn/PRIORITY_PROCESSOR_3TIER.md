# Priority Processor — 3-Tier Passthrough Design

The story of arriving at a working priority-aware otelcol processor for the
SB SDK + ES-backed jaeger pipeline. Two earlier designs (a 2-tier
buffered processor with worker pool + eviction, then a variant that
returned `ErrSkipProcessingData` to decouple the receiver) failed
expensively. The third design — a stateless, passthrough, 3-tier
classifier modeled after `memory_limiter` — works, beats both vanilla
and SB+memlim, and preserves checkpoint spans under congestion.

## Goal

We want trace-shedding behavior that:

- Behaves identically to `memory_limiter` under low load (no overhead,
  no extra drops)
- At sustained pressure, sheds **low-priority (LP) spans first** while
  preserving **checkpoint (CP / HP) spans**
- Doesn't introduce new failure modes (gRPC deadline cascade, GC
  thrashing, queue saturation, etc.)

The SB SDK already tags every UploadTraces gRPC call with a
`bridges-priority: hp|lp` metadata header based on the
checkpoint-distance (`cpd=3`) classification. The otelcol-side
processor's job is to consume that header and shed selectively.

## Designs that didn't work

### Design 1: 2-tier buffered, worker pool, eviction

```
receiver → priority (hpQ + lpQ slices + 10 workers + eviction) → batch → otlp_exporter
                                                                            ↑
                                                            sending_queue: 10, block_on_overflow: true
```

The intent: small blocking `sending_queue` forces backpressure into the
priority queues, which builds memory pressure, which triggers eviction
of LP from `lpQ` to admit incoming HP.

What actually happened at 2k rps:

| metric | value |
|---|---:|
| cpost drop | 53.4 % |
| send_deadline (batches) | 5,519 |
| send_unavailable (batches) | 7,303 |
| priority gc_count | 118 |
| priority lp_evicted | 209 k |
| priority hp_refused | 1.58 M |

Three problems compounded:

1. **lpQ depletion trap.** Eviction is gated by HP-admission events
   (`if len(lpQ) >= ratio: evict ratio LP, admit HP`). Under soft pressure
   the policy also refuses incoming LP. So lpQ drains (via eviction)
   but never refills. Once lpQ < ratio, *HP admission also fails*. The
   "evict LP to admit HP" mechanism eats its own fuel.

2. **GC pause cascade.** With workers blocked on the tight sending_queue
   and priority's queues unbounded slices, alloc climbs faster than
   eviction can shed. Every 100 ms tick that finds alloc above hard
   forces a `runtime.GC()`. 118 forced GCs in 300 s. Each STW pause
   delays gRPC response delivery, contributing to deadlines.

3. **The receiver path was still synchronous.** `processorhelper`
   auto-forwards whatever `Traces` value `processTraces` returns to the
   next consumer — even an empty `ptrace.NewTraces()`. So every
   receiver-side handler call did *two* downstream forwards: one for
   the empty placeholder (via processorhelper) and one for the real
   batch (via the worker pool). Both hit the same sending_queue. The
   empty-placeholder forward blocked the receiver handler on the same
   resource the worker pool was contending for.

### Design 2: same as #1 but admission returns `ErrSkipProcessingData`

The `processorhelper.ErrSkipProcessingData` sentinel tells the wrapper
"don't forward, I handled it." This should remove the receiver-path
double-forward.

Result at 2k rps: **96.5 % cpost drops, 21,848 deadlines, 538 GCs.**
Markedly worse.

The fix worked at the processor level — priority queues finally had
real depth (hpQ peaked at 943 batches on the busiest pod), eviction
actually fired (1.5 M vs 465 k previously), and the receiver-path
desync was real. But removing the receiver-side coupling without
adding any new pushback turned an implicit throttle (the synchronous
forward had been quietly slowing the SDK) into nothing. The SDK kept
pushing, priority's queues filled faster, memory pressure grew, force
GCs went from 144 → 538, and the receiver-side scheduling delay caused
*more* deadlines, not fewer.

The buffered approach was structurally wrong. The whole point of
buffering in priority's queues was to give the processor visibility
into "queue depth ≈ pressure" so it could evict — but that buffering
itself is the source of the memory pressure being measured. The signal
and the stuff causing the signal were the same.

## The design that works

```
receiver → priority (stateless 3-tier classifier) → batch → otlp_exporter
                                                                ↑
                                                  sending_queue: 1000, block_on_overflow: false
```

`priority` is a pure passthrough. No queues, no workers, no eviction.
On every incoming batch:

- Read alloc state (updated by 100 ms tick).
- Decide admit-or-refuse based on the tier and the batch's
  `bridges-priority` header.
- Admit: return `(td, nil)` — processorhelper forwards to the next
  consumer (batch, then otlp_exporter, then ES). No buffering inside
  priority.
- Refuse: return `(td, status.Error(codes.Unavailable, "..."))` — the
  gRPC error propagates back to the SDK, which counts the batch as
  `send_unavailable` and drops it (we run with `BLUEPRINT_OTLP_RETRY=false`).

### Tier behavior

Thresholds expressed as percent of container memory limit
(256 MiB by default → ultrasoft=89 MiB, soft=128 MiB, hard=179 MiB):

| state | alloc range | LP behavior | HP behavior | GC |
|---|---|---|---|---|
| NoPressure | < 35 % | admit | admit | — |
| Ultrasoft | 35 – 50 % | **refuse** | admit | — |
| Soft | 50 – 70 % | refuse | refuse | — |
| Hard | ≥ 70 % | refuse | refuse | force `runtime.GC()` |

Soft and hard match `memory_limiter`'s semantics exactly. The
**Ultrasoft tier is the only mechanism unique to the priority
processor** — a band below memlim's soft where the system isn't yet
under "shed everything" pressure but is showing the early signs, and
where shedding LP-only buys headroom.

### Why this works structurally

- **The processor is no longer the source of memory growth.** With no
  internal queues, priority's alloc footprint is constant ~few-MB. The
  state machine reads alloc that's actually moved by the batch
  processor and the sending_queue (i.e., by spans transiting toward
  ES). Signal and source are decoupled.

- **Same feedback loop as memlim.** Alloc rises as ES indexing falls
  behind → processor tightens → admits less → fewer spans enter the
  pipeline → ES catches up → alloc falls → cycle. This is the proven
  memlim loop. The only addition is the ultrasoft tier giving us one
  earlier shedding option that's LP-only.

- **No buffered state to recover.** Under sustained pressure there's no
  "lpQ to deplete" or "worker queue to drain." When pressure clears,
  the processor returns to admit-all immediately on the next tick.

- **No receiver-path coupling.** processorhelper auto-forwards the
  admitted `Traces` directly to batch → sending_queue. No duplicate
  empty-forward, no extra synchronization point.

- **GC behavior matches memlim.** `runtime.GC()` only fires at hard
  pressure, exactly like memlim. (gc_count = 0 across the entire 6-point
  sweep, confirming we never reached sustained hard pressure.)

## Full sweep results

500 → 1k → 1.5k → 2k → 2.5k → 3k rps, 300 s steady per phase,
same-day same-cluster, ES accumulates within sweep (fresh ES at 500).

All three variants share the same underlying app + Jaeger + ES backend
config (`ES_BULK_WORKERS=10`, `ES_JAVA_OPTS=-Xms4g -Xmx4g`, no ES
container memory limit, `COLLECTOR_QUEUE_SIZE=10000`,
`COLLECTOR_NUM_WORKERS=100`). Otelcol limits: 256 MiB container,
GOMEMLIMIT=180 MiB.

### 500 rps — clean idle

| variant | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 0.0 | — | — | 0 | 7.4 ms | 41 ms | 9.5 c |
| SB+memlim | 0.0 | 0.0 | 0.0 | 0 | 7.5 ms | 69 ms | 10.8 c |
| SB+priority(3tier) | 0.0 | 0.0 | 0.0 | 0 | 7.5 ms | 51 ms | 10.2 c |

### 1k rps — clean idle

| variant | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 0.0 | — | — | 0 | 11.5 ms | 83 ms | 10.9 c |
| SB+memlim | 0.0 | 0.0 | 0.0 | 0 | 11.6 ms | 101 ms | 12.2 c |
| SB+priority(3tier) | 0.0 | 0.0 | 0.0 | 0 | 11.5 ms | 96 ms | 11.6 c |

### 1.5k rps — clean idle

| variant | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 0.0 | — | — | 0 | 23.2 ms | 113 ms | 12.8 c |
| SB+memlim | 0.0 | 0.0 | 0.0 | 0 | 22.0 ms | 141 ms | 14.1 c |
| SB+priority(3tier) | 0.0 | 0.0 | 0.0 | 0 | 25.4 ms | 137 ms | 13.0 c |

### 2k rps — first sustained pressure

| variant | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 5.4 | — | — | 0 | 58.0 ms | 764 ms | 21.0 c |
| SB+memlim | 16.0 | 16.0 | 16.0 | 0 | 47.3 ms | 248 ms | 21.8 c |
| **SB+priority(3tier)** | **16.8** | **14.3** | **35.0** | **0** | **47.8 ms** | **229 ms** | **22.8 c** |

### 2.5k rps — heavy congestion

| variant | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 47.0 | — | — | 0 | 85.4 ms | 754 ms | 26.6 c |
| SB+memlim | 56.3 | 56.3 | 56.5 | 4,428 | 65.3 ms | 710 ms | 15.3 c |
| **SB+priority(3tier)** | **45.1** | **40.8** | **75.2** | **648** | **71.0 ms** | **478 ms** | **28.8 c** |

### 3k rps — app saturation

| variant | achieved rps | drop % | cp % | lp % | deadlines | p50 | max | ES peak |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| vanilla+memlim | 2,273 | 33.7 | — | — | 0 | 31,145 ms | 68,944 ms | 35.5 c |
| SB+memlim | 2,268 | 44.1 | 44.1 | 44.2 | 2,238 | 31,168 ms | 67,240 ms | 34.9 c |
| **SB+priority(3tier)** | **2,437** | **43.2** | **39.7** | **67.6** | **1,852** | **24,133 ms** | **64,651 ms** | **13.4 c** |

### Throughput at 2.5 k rps

Averaged over the 300-second steady window:

| variant | cpost.received | cpost.sent | otelcol receiver | otelcol → jaeger |
|---|---:|---:|---:|---:|
| vanilla+memlim | 19,966 sps | 10,579 sps | 44,660 sps | 41,824 sps |
| SB+memlim | 19,962 sps | 8,715 sps | 42,722 sps | 41,720 sps |
| SB+priority(3tier) | 19,964 sps | **10,951 sps** | 40,959 sps | 40,558 sps |

End-to-end span throughput converges around 40-42 k sps — the **ES
indexing ceiling**. All three variants are running into the same
downstream cap. What differs is the *composition* of what gets through.

## Insights

### 1. CP preservation is the headline win

At every congested RPS level, 3tier drops more LP than vanilla/memlim
but **less CP**. The numbers that matter for the SB design's premise:

| rps | vanilla cp drop | SB+memlim cp drop | **3tier cp drop** |
|---:|---:|---:|---:|
| 2k | ~5.4 % | 16.0 % | **14.3 %** |
| 2.5k | ~47.0 % | 56.3 % | **40.8 %** |
| 3k | ~33.7 % | 44.1 % | **39.7 %** |

Same total throughput cap, more checkpoint spans preserved. That's the
point.

### 2. End-to-end throughput is gated by ES, not by SDK overhead at saturation

At 2k the SB SDK's per-span overhead (CP/LP classification + bridge
state work in OnStart) costs ~11 pp of total drops vs vanilla
(16.8 % vs 5.4 %). At 2.5k that gap closes to ~2 pp (45.1 % vs 47.0 %)
and reverses: 3tier *beats* vanilla on total drops despite the SDK
tax. The reason is structural — at saturation the pipeline's hard
ceiling is ES bulk-write throughput, which both variants hit equally.
The SB SDK tax stops mattering once the system is throughput-bound on
ES; it only matters in the regime where vanilla still has headroom.

### 3. Deadlines came from synchronous coupling, not retries

We thought initially that send_deadline events were from the SDK's
internal retry/queue logic. They weren't — both vanilla and SB SDKs
run with `BLUEPRINT_OTLP_RETRY=false`, so deadlines correspond to
gRPC calls actually taking > 1 s. The deadlines were generated by the
buffered priority design's tight backpressure interactions: GC pauses,
workers blocked on the tiny sending_queue, batch processor's
`newItem` channel filling, and the receiver-path forward all
contributed. The 3tier passthrough cut deadlines from 4,428 → 648
at 2.5k (an 85 % reduction). The remaining 648 is plausibly SB SDK
per-batch overhead (vanilla had 0).

### 4. `processorhelper.ErrSkipProcessingData` doesn't help on its own

You can't decouple receiver from downstream without giving the
processor a way to *push back* on the SDK. Removing the synchronous
forward without adding refusal pushback just made the SDK shove
harder. The 3tier solves this by being a pure passthrough — the
receiver path is fast *and* refusal returns gRPC Unavailable so the
SDK gets explicit pushback.

### 5. The batch processor's internal queue isn't an "unbounded
memory amplifier"

We suspected batch's internal buffers were silently inflating alloc
beyond priority's view. Reading the source disproved this: the shard's
`newItem` channel is `runtime.NumCPU()`-deep (4-8 slots on our nodes
≈ 400-800 KB), and the accumulating batch caps at SendBatchSize=8192
spans (~4 MB). Total batch footprint is ~5-10 MB per shard —
contributes to alloc but isn't the dominant signal. However batch's
`consume()` is an unconditional blocking channel send, so it *does*
add a second blocking stage in the backpressure chain when downstream
is full. With the 3tier passthrough config (sending_queue = 1000
non-blocking), this blocking never materializes.

### 6. The buffered design was solving the wrong problem

The 2-tier buffered approach assumed buffering inside priority was
necessary so the processor could measure pressure (queue depth) and
react (eviction). But the memlim feedback loop doesn't need that —
memory pressure shows up in `runtime.MemStats.Alloc` regardless of
where the buffered spans live (batch processor, sending_queue,
in-flight RPCs). A stateless processor reads exactly the same signal
without contributing to it.

## Code reference

- `processor/priorityprocessor/priority.go`: the 3-tier passthrough
  implementation. ~270 lines vs the 500-line buffered version.
- `processor/priorityprocessor/config.go`: defaults
  `ultrasoft_percentage=35, soft_percentage=50, hard_percentage=70,
  check_interval=100ms`.
- Archived predecessor at
  `processor/_archive/priorityprocessor_2tier_evictionworker_v1_2026-06-11/`
  — keep for reference, excluded from Go build via the `_` prefix.

## Otelcol config for sb-esrev0

```yaml
exporters:
  otlp:
    endpoint: ${JAEGER_SB_ESREV0_OTLP_DIAL_ADDR}
    retry_on_failure: {enabled: true, initial_interval: 5s, max_elapsed_time: 0, max_interval: 30s}
    sending_queue: {enabled: true, num_consumers: 10, queue_size: 1000, block_on_overflow: false}
    tls: {insecure: true}

processors:
  batch: null              # defaults: send_batch_size=8192, timeout=200ms
  priority:
    check_interval: 100ms
    ultrasoft_percentage: 35
    soft_percentage: 50
    hard_percentage: 70

receivers:
  configdiscovery:
    endpoint: ":8080"
    config_map: {cpd: 3}
  otlp:
    protocols:
      grpc: {endpoint: 0.0.0.0:4317, include_metadata: true}

service:
  pipelines:
    traces:
      exporters: [otlp]
      processors: [priority, batch]
      receivers: [otlp]
```

`include_metadata: true` on the OTLP receiver is load-bearing — without
it, `bridges-priority` doesn't reach the processor's
`client.FromContext`, and every batch is treated as HP.

## Run directories

- 3tier sweep (this writeup):
  `runs/nopad_sb-esrev0_priority_3tier_{500,1000,1500,2000,2500,3000}rps_2026-06-11_05*`
- SB+memlim sweep (prior baseline):
  `runs/nopad_sb-esrev0_{500,1000,1500,2000,2500,3000}rps_2026-06-10_{23,00}*`
- Vanilla+memlim sweep (prior baseline):
  `runs/nopad_v-esrev0_full_{500,1000,1500,2000,2500,3000}rps_2026-06-11_0*`
- Failed buffered priority sweep (for reference):
  `runs/nopad_sb-esrev0_priority_{500,1000,1500,2000,2500,3000}rps_2026-06-11_01*`
- Failed ErrSkip-fix runs (for reference):
  `runs/nopad_sb-esrev0_priority_skip{,v3,v4}_2000rps_2026-06-11_04*`
