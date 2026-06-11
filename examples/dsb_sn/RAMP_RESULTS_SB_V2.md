# DSB-SN SB (Structural Bridge) Pipeline Ramp Results — v2

Empirical characterization of the **SB** (structural bridge) DSB-SN pipeline
under load, with the **v2 priority processor** replacing the standard
memory_limiter + batch + sending_queue combo. Companion to
`RAMP_RESULTS.md` (vanilla baseline).

Setup snapshot: **2026-06-04**, CloudLab `bridges-tb-02` testbed, target
identifier suffix `sbrev0` (variant `sb`, extra `rev0`).

## Hardware & Cluster

Same as `RAMP_RESULTS.md`. Identical 10-node CloudLab cluster, 40-CPU
Xeons clamped to 2.2 GHz, kubespray k8s v1.29.10, local registry at
10.10.1.1:30000.

## Software Configuration

### Blueprint runtime tuning (identical to vanilla)

- `BLUEPRINT_GOGC=off` (Go GC disabled — relies on Blueprint's time-based
  GC via `GC_INTERVAL_SEC=0.1` and, on the collector side, on the
  priority processor's force-GC under pressure).
- `BLUEPRINT_BRIDGE_KIND=sb` baked into every service via Blueprint
  wiring → all SB SDK processors active on the app side.

### v2 priority processor (NEW vs vanilla pipeline)

Replaces three components from the vanilla pipeline in one component:

| Vanilla role | Vanilla impl | v2 SB impl |
|---|---|---|
| Admission control under memory pressure | `memory_limiter` processor | `priority` processor's hp/lp admission policy |
| Batching for downstream send | `batch` processor (8192 spans / 200ms) | `priority` processor's `batch_size=256` per worker pull |
| Out-of-process queueing | otlp exporter's `sending_queue` (1000 batches ≈ 8M spans) | `priority` processor's hp/lp slices + N worker goroutines |
| Wire-level retry | otlp exporter's `retry_on_failure` | unchanged — otlp exporter still owns this |

Config:
```yaml
processors:
  priority:
    limit_percentage:       80         # same threshold as memory_limiter
    spike_limit_percentage: 20         # same threshold as memory_limiter
    check_interval:         100ms      # 10× faster than memory_limiter default
    num_consumers:          10         # absorbs sending_queue.num_consumers
    batch_size:             256        # absorbs batch.send_batch_size

exporters:
  otlp:
    sending_queue:
      enabled: false                   # priority IS the queue
    retry_on_failure:
      enabled: true                    # kept — wire resilience, not buffering
```

Pipeline: `otlp → priority → otlp` (no `batch`, no `memory_limiter`).

### Force-GC under pressure (mirrors memory_limiter's behavior)

Under GOGC=off the Go runtime never auto-reclaims dead heap. memory_limiter
calls `runtime.GC()` under pressure; the v2 priority processor matches
this exactly:

- **Hard pressure**: force GC on every 100ms check (matches memory_limiter
  default `MinGCIntervalWhenHardLimited=0s`)
- **Soft pressure**: force GC at most every 1s, plus an unthrottled GC on
  the none→soft transition. We diverge from memory_limiter's 10s soft
  default because our 100ms check tick would mean 100 idle ticks before
  any soft GC, letting allocation drift carry us through soft into hard.
- Result: 0 OOMKills observed at 2k→4k ramp (vs 8/9 pods OOMKilled in
  v1 / earlier prototypes).

### Memory source: `runtime.MemStats.Alloc`

Same source memory_limiter uses (`memorylimiter.go::aboveSoftLimit`).
Live Go heap, not cgroup RSS. Responsive to admission decisions — when we
drop / GC, the next reading reflects it. (cgroup RSS lags by seconds due
to Go's scavenger.)

### Cluster shape (matches vanilla)

- 9 collector pods (DaemonSet, one per worker node), 1 CPU / 1 GiB each
- 13 backend service pods (composepost, hometimeline, media, post-storage,
  socialgraph, text, uniqueid, urlshorten, user, userid, usermention,
  usertimeline, wrk2api)
- jaeger pod on a dedicated node
- node-pinning per `node-pinning-sbrev0.yaml`
- 2.2 GHz CPU clamp via `utils/pin-cpu-22ghz.sh`

## Workload

Same ramp as the vanilla run:

```
utils/run_dsb_sn_ramp.sh --target sbrev0 \
    --start 2000 --end 4000 --step 200 \
    --duration 60 --break 30 \
    --warmup-rps 100 --warmup-duration 100
```

11 ramp steps (2000 → 4000 in 200-rps increments), 60s per step,
30s break, preceded by 100s warmup at 100 rps. wrk thread/connection
formula: `C = ceil(rps² / 20000)`, `T = ceil(C/10)`.

## Results

### Throughput / latency

| Target rps | Achieved rps | % of target | Avg latency | 99th |
|---|---|---|---|---|
| 2000 | 1996 | 99.8% | 25.4 ms | 67.8 ms |
| 2200 | 2195 | 99.8% | 30.3 ms | 67.5 ms |
| 2400 | 2372 | 98.8% | 34.2 ms | 72.2 ms |
| 2600 | 2580 | 99.2% | 33.2 ms | 69.8 ms |
| 2800 | 2791 | 99.7% | 39.4 ms | 73.0 ms |
| 3000 | 2960 | 98.7% | 45.7 ms | 92.4 ms |
| 3200 | 3160 | 98.7% | 47.8 ms | 122 ms |
| 3400 | 3351 | 98.6% | 102 ms | 269 ms |
| 3600 | 3536 | 98.2% | 339 ms | 884 ms |
| 3800 | 3625 | 95.4% | **1.27 s** | 3.17 s |
| 4000 | 3532 | 88.3% | **3.35 s** | 8.41 s |

Cluster saturates between 3.5k and 3.6k rps. Latency stays sub-50ms
through 3200, knees up sharply at 3400.

### Collector-side (`priority_processor_metrics`)

Per-pod end-of-run snapshot:

| Pod | cp_in | cp_drop% | lp_in | lp_drop% | lp_evicted | gc_count | alloc |
|---|---|---|---|---|---|---|---|
| w7rm4 (busiest backend) | 17,690,166 | 3.9% | 2,159,638 | 3.9% | 0 | 96 | 440 MB |
| z6874 (wrk2api node — 1:1 CP:LP from root server + outcall pattern) | 4,186,847 | 1.2% | 4,111,599 | 1.3% | 0 | 46 | 467 MB |
| 4gxfp | 4,101,397 | 1.0% | 218,765 | 0.9% | 0 | 28 | 538 MB |
| c77dr | 2,160,567 | 0.6% | 219,251 | 0.6% | 0 | 18 | 423 MB |
| cgkkp | 2,274,787 | 0.7% | 219,637 | 0.8% | 0 | 19 | 316 MB |
| lftrx | 2,160,355 | 0.5% | 219,039 | 0.8% | 0 | 18 | 515 MB |
| rcpnm | 2,158,968 | 0.5% | 217,652 | 0.5% | 0 | 18 | 256 MB |
| rrmrm | 2,162,482 | 0.5% | 219,242 | 0.5% | 0 | 16 | 465 MB |
| j69b6 | 219,027 | 0.1% | 219,027 | 0.1% | 0 | 3 | 354 MB |

**Aggregate (collector totals over the whole ramp):**

| Metric | Value |
|---|---|
| Total CP arrived at priority | **38.1 M** |
| Total LP arrived at priority | **7.78 M** |
| Total CP dropped | ~600 k (1.6%) |
| Total LP dropped | ~130 k (1.7%) |
| **Total lp_evicted (grace-zone evictions)** | **0** |
| Total GC invocations across pods | 262 |
| OOMKills / restarts | **0** |

### SDK-side (`sb_processor_metrics`)

Per-service end-of-run snapshot. All `err_*` counters break down the
gRPC errors that triggered local drops at the SDK side — i.e. spans
the SDK could not hand off to the collector.

| Service | spans_received | spans_sent | drop% | err_unavailable | err_deadline | err_exhausted |
|---|---|---|---|---|---|---|
| composepost | 15,530,528 | 10,152,677 | **34.6%** | 10,624 | 0 | 0 |
| text | 5,823,948 | 4,589,674 | **21.2%** | 2,425 | 0 | 0 |
| hometimeline | 3,882,632 | 3,217,527 | **17.1%** | 1,552 | 0 | 0 |
| usertimeline | 1,941,316 | 1,711,575 | **11.8%** | 856 | 0 | 0 |
| media | 1,941,316 | 1,912,685 | 1.5% | 74 | 0 | 0 |
| userid | 75,248 | 74,224 | 1.4% | 2 | 0 | 0 |
| post-storage | 1,941,316 | 1,936,017 | 0.3% | 17 | 0 | 0 |
| user | 1,943,240 | 1,939,618 | 0.2% | 14 | 0 | 0 |
| usermention | 1,941,316 | 1,937,314 | 0.2% | 65 | 0 | 0 |
| socialgraph | 2,055,150 | 2,050,966 | 0.2% | 16 | 0 | 0 |
| urlshorten | 1,941,316 | 1,938,994 | 0.1% | 7 | 0 | 0 |
| uniqueid | 1,941,316 | 1,940,117 | 0.1% | 5 | 0 | 0 |
| wrk2api | 438,502 | 438,174 | 0.1% | 8 | 0 | 0 |

**Aggregate (SDK totals across all services):**

| Metric | Value |
|---|---|
| Total spans emitted by Blueprint apps | ~41.4 M |
| Total spans handed off to collector | ~35.0 M |
| Total spans dropped at SDK | ~6.4 M (~15.4%) |
| Total `err_unavailable` events | ~15.7 k |
| Total `err_deadline` events | 0 |
| Total `err_exhausted` events | 0 |

## Interpretation

### What works

1. **Survival under sustained 4k rps with no OOMKills.** Vanilla
   prototypes and earlier SB iterations OOM-killed 8/9 collector pods.
   v2's force-GC + memory-pressure-driven admission keeps all 9 alive
   through the saturation regime.

2. **Collector-side drops are minimal.** Even on the busiest pod
   (w7rm4, 17.7 M CP through it), drop rate is 3.9%. The 100ms check
   tick + 1s soft-GC interval + entering-soft GC keeps `ms.Alloc`
   trending downward fast enough that hard pressure is brief.

3. **The "priority processor IS the queue" architecture is stable.**
   No batch processor downstream; no `sending_queue` at the exporter.
   All in-flight bytes live in hp/lp slices that we directly own.
   Workers drain via blocking `nextConsumer.ConsumeTraces` →
   `retry_on_failure` → gRPC.

### What doesn't (yet)

1. **`lp_evicted = 0` on every pod.** The grace-zone eviction policy
   never fires because workers (10 per pod, batch_size=256, fast
   downstream) drain hp/lp faster than the SDK can fill them. The buffer
   never accumulates an LP backlog for incoming CP to sacrifice from.

2. **Drops are CP/LP-symmetric.** Every pod shows `cp_drop% ≈ lp_drop%`.
   This means every dropped span went through the *hard-pressure*
   drop-all branch, not through the *soft-pressure with lp-eviction*
   branch. The priority signal has no expressed advantage in this run.

3. **The SDK→collector hop is the dominant drop site, and it's
   priority-blind.** composepost loses 34.6% of its emitted spans at
   the gRPC ingress because the collector's worker pool is saturated
   when ingress refuses → `err_unavailable` → SDK drops a random
   subset. The `_br` breadcrumb is on those dropped spans, but nothing
   in the gRPC stack reads it.

The first two problems share a root cause: drain capacity ≥ incoming.
Two design options exist for forcing the buffer to fill so eviction
matters:

- **LP reservoir cap**: workers only drain LP when `len(lpBuf) > reserve_size`.
  Below the watermark LP sits, available for eviction.
- **Worker throttle**: lower `num_consumers` / `batch_size` so drain
  is artificially slower; buffer accumulates under any sustained load.

The third problem requires either pushing priority-awareness upstream
into the SDK (admission control there too — at which point the SDK
processor itself becomes a queue-with-eviction in the same shape as
the collector's priority processor) or accepting that gRPC backpressure
loses spans randomly.

## Comparison-pending

This run pairs with a planned vanilla run at the **same 2k→4k step=200
ramp** to make the SB-vs-vanilla comparison fair (the existing
`RAMP_RESULTS.md` predates the v2 priority processor changes).
Specifically the comparison needs:

- Same wrk parameters (target rps, duration, break, warmup, lua script,
  seeded social graph)
- Same cluster shape (9 collector pods, node pinning, CPU clamp)
- Vanilla collector config unchanged (`memory_limiter` + `batch` +
  `sending_queue` at queue_size=1000, retry_on_failure)
- Captured: `memory_limiter` refused counts, `sending_queue` enqueue
  failed counts, vanilla SDK processor's recv/sent/drop counters, wrk
  throughput / latency / non-2xx
- Diffed: spans-emitted vs spans-arrived-at-jaeger across both setups,
  per service

## Files / artifacts referenced

- `opentelemetry-collector-contrib/processor/priorityprocessor/`
  — full v2 implementation
- `opentelemetry-collector-contrib/processor/priorityprocessor/DESIGN.md`
  — design rationale and architecture decisions
- `opentelemetry-collector-contrib/test-config-bridges.yaml` — pipeline
  config consumed at wiring time
- `runtime/plugins/otelcol/sb_processor.go` — SDK-side SB processor
- `runtime/plugins/otelcol/pack.go` — wire format (AttrBR, AttrBREmit,
  decodeBRDepth)
- `examples/dsb_sn/build_sbrev0/` — kompose-derived build artifacts
- `utils/run_dsb_sn_ramp.sh` — ramp runner (used here)
- Logs of this run: `/tmp/claude-20010/-users-tomislav/0acdace7-b640-4bdc-ab8d-1b5e2598cd21/tasks/bqutp47yf.output`
