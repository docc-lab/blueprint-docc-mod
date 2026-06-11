# DSB-SN Tracing Pressure Experiments — Findings

Pressure-testing the SB SDK + memory_limiter collector at 256 MiB / 500m CPU using a TracePressureService DaemonSet alongside the real dsb_sn workload.

Setup:
- 9 otelcol DaemonSet pods, 256 MiB / 500m CPU each
- Real workload: dsb_sn compose-post at 2000-3000 rps (target)
- Synthetic pressure: tracepressure-service (HTTP `/Spam?n=N` endpoint) generating N force-LP child spans per request
- All otelcol pods: `memory_limiter` collector config (no priority processor)

## Run summary table

| Run dir | RPS target | Spam config | Result |
|---|---|---|---|
| sbrev0_steady_spam_clean | 3000 | N=10, rps=820 → 1k/pod | 0 drops, 0 OOMs |
| sbrev0_2k_spam1k | 2000 | N=10, rps=820 → 1k/pod | 0 drops, 0 OOMs |
| sbrev0_2k_spam5k | 2000 | N=10, rps=4091 → 5k/pod | 0 drops, 0 OOMs |
| sbrev0_2k_spam46k | 2000 | N=100, rps=4091 → 46k/pod | **8/9 OOMs, crashloop** |
| sbrev0_2k_spamN50 | 2000 | N=50, rps=4091 → 23k/pod | ~1% drops, 0 OOMs |
| sbrev0_2k_spamN100_soft50 | 2000 | N=100, soft=50%, 1s check | 9/9 OOMs (soft mark didn't help) |
| sbrev0_2k_spamN100_100ms | 2000 | N=100, soft=50%, **100ms check** | 6+/9 OOMs (100ms alone didn't help) |
| sbrev0_2k_spamN100_gomemlim | 2000 | N=100, soft=50%, 100ms, **GOMEMLIMIT=200MiB** | **0 OOMs, 19% drops** |
| sbrev0_2k_spamN100_cpusnap | 2000 | same + CPU snapshots | 0 OOMs, 29% drops, app capped at 1766 rps |
| sbrev0_2k_spamN100_freshdb | 2000 | same, fresh DB | 1 OOM, 22% drops, **1999 rps achieved** |
| sbrev0_2k_spamN100_70pct | 2000 | N=100, **70% / GOMEMLIMIT=180MiB** | 1 OOM still, 31% drops, 1999 rps |
| sbrev0_2k_spamN50_70pct | 2000 | N=50, 70% / 180MiB | **0 OOMs, 0 drops** (too conservative) |
| sbrev0_2k_spamN75_70pct | 2000 | **N=75**, 70% / 180MiB | **0 OOMs, 4.7% drops** ← sweet spot |
| vrev0_2k_spamN75_70pct | 2000 | vanilla SDK, N=75, 70%/180MiB | 0 OOMs, composepost 0% drops, tracepressure 3.6% drops |

## Headline findings

### 1. The earlier "1k/pod OOM" was a wrk-client artifact, not span pressure

The first big-burst test (`SPAM_RPS=90, SPAM_N=100`) caused all 9 collectors to OOM. We attributed it to span volume. But the spam formula `C = (SPAM_RPS² + 4)/5` set wrk to **1620 connections × 162 threads** for a 90 rps target — the driver was slamming all collector pods with connection-establishment churn. After fixing wrk to `9t × 18c`, 1000/pod (`N=10, RPS=820`) and even 5000/pod (`N=10, RPS=4091`) ran totally clean with zero drops.

### 2. Real OOMs need extreme allocation rates (~50 MB/sec per pod)

Drop-without-OOM regime sits in a narrow band:
- N=50 fanout (23k spans/sec/pod, ~23 MB/sec alloc): drops happen at ~1%, no OOMs
- N=100 fanout (46k spans/sec/pod, ~46 MB/sec alloc): OOMs cascade across the fleet

At 46 MB/sec, between memory_limiter's 1s checks the heap can grow ~46 MB. RSS = alloc × ~1.3 (Go runtime overhead), and the cgroup OOM-killer trips before the next check can react.

### 3. memory_limiter alone can't be tuned out of this

Tried in order, *none* fixed the OOM at N=100:
- `spike_limit_percentage: 30` (soft at 50% instead of 60%) — didn't help
- `check_interval: 100ms` (10× tighter than default 1s) — still OOMed
- Both combined — still OOMed

The problem is reactive polling vs synchronous cgroup OOM. memory_limiter measures `ms.Alloc`; the kernel measures RSS. They diverge during in-flight allocation bursts, and memory_limiter cannot stop the divergence — it can only refuse *new* batches *after* alloc has already crossed.

### 4. `GOMEMLIMIT=200MiB` fixes the OOM — completely

Setting `GOMEMLIMIT=200MiB` on the otelcol pods kept all 9 alive through the exact load that crashlooped them before. The Go runtime self-paces against the budget continuously (not on a 100ms tick), aggressively triggering GC and madvising idle pages back to the OS as heap approaches the limit. This is the modern, intended mechanism for bounding RSS in Go containers.

Combined config that survives N=100 spam at 256 MiB:
- `GOMEMLIMIT=200MiB` (env var on the container)
- `memory_limiter check_interval: 100ms, limit_percentage: 80, spike_limit_percentage: 30`
- `GOGC` left default (we removed Blueprint's `GOGC=off` injection from otelcol containers earlier — see `RAMP_COMPARISON_256MI.md`)

### 5. memory_limiter is priority-blind (well-known, but now measured at scale)

In the GOMEMLIMIT-stabilized N=100 run, composepost SDK reported:
```
cp_dropped / cp_received = 19.3%
lp_dropped / lp_received = 19.3%
```
The two rates were *identical to four significant figures*. Under memory pressure, memory_limiter sheds a random subset of incoming batches — CP and LP get refused at the same rate. This is the baseline the priority processor is designed to break (priority pass-through would refuse LP at multiple × the CP rate).

### 6. DB state bloat caps app throughput, not the collector

Running N=100 spam against a long-running deployment: app achieved 1766 rps (12% below the 2000 target). Same load against a fresh redeploy of MongoDB: **1999 rps**. usertimeline-db on node-4 was at 17.5 cores under bloated state (saturating its node to 91% CPU); on fresh state it dropped to <5 cores. Cause: dsb_sn's compose-post fanout includes usertimeline writes, and Mongo gets slower as documents accumulate.

### 7. tracepressure pods themselves cost ~12 cores cluster-wide

At N=100/RPS=4086, each tracepressure pod uses 1.2–2.0 CPU cores generating + sending synthetic spans. Across 9 DaemonSet pods that's ~12 cores cluster-wide. This is non-trivial but **wasn't** the cap on app rps — node-4's usertimeline saturation was. Per-node, tracepressure consumed only ~1.3 cores on the composepost-heavy node-1, leaving plenty of headroom for the app.

### 8. Jaeger all-in-one in-memory storage will OOM-kill the host node

`jaeger-all-in-one` defaults to in-memory storage with no eviction. At our spam load (~412k spans/sec cluster-wide × 7-minute run), jaeger held **~91 GB of spans** by end of run. node-9 (where jaeger is pinned) has 192 GB RAM; a few back-to-back runs would consume all of it.

Confirmed empirically: node-9 went `NotReady` after multiple N=100 runs, kubelet stopped reporting status, host stopped responding to SSH, required a CloudLab power-cycle to recover.

**Workaround applied**: `run_steady_with_spam.sh` now does `kubectl rollout restart deployment/jaeger-${VARIANT}-ctr` as Phase 0 of every run, giving jaeger a clean ~0 GB starting point.

**Better fix (future)**: switch jaeger to badger (disk-backed; node-9 has 832 GB free disk) or elasticsearch.

### 9. Burst size (fan-out N) is the dominant knob, not total span rate

At fixed wrk request rate (`SPAM_RPS=4091`), varying just the per-request span fanout N gives wildly different collector behavior:

| N (spans/request) | Cluster-wide span rate | OOMs | composepost drop% | Verdict |
|---|---:|---:|---:|---|
| 50 | ~22.5 k/s | 0 | 0.0% | too conservative |
| **75** | **~34 k/s** | **0** | **4.7%** | **sweet spot** |
| 100 | ~45 k/s | 1 (sometimes more) | 22-31% | unstable / OOMs |

At the same total RPS the bigger per-request fanout means each gRPC handler decodes a larger ResourceSpans in one shot — a 100-span batch ≈ 100 KB heap allocated in one burst. With multiple concurrent decodes, that stacks into MB-sized transient spikes that race past memory_limiter's polling window. At N=75, the per-batch decode footprint stays smaller and concurrent bursts stay under the GOMEMLIMIT threshold.

**N=75 at 70% / GOMEMLIMIT=180MiB** is the goldilocks regime for comparing memory_limiter (priority-blind shedding) against the priority pass-through processor: 4.7% drops at the SDK with perfectly equal CP/LP rates, and zero OOM crashloops. This is the load for the priority-processor comparison run.

### 10. Vanilla SDK vs SB SDK at the same N=75 load — different pressure distribution

At the same N=75 / 70% / 180MiB / 2k dsb_sn rps + 4091 spam rps load:

| Metric | vrev0 (vanilla SDK) | sbrev0 (SB SDK) |
|---|---|---|
| otelcol OOMs | 0 | 0 |
| composepost SDK drop | **0.0%** | 4.7% (CP=LP) |
| tracepressure cluster drop | **3.6%** | 0.7% |
| wrk2api achieved rps | 1999 | 1999 |

The drop *total* is similar in both runs but lands on *different services*:
- Vanilla: 0% at composepost, 3.6% at tracepressure
- SB: 4.7% at composepost, 0.7% at tracepressure

Why the shift: SB spans carry the `_br` breadcrumb attribute on every CP span (~50-150B per CP), making each composepost span heavier on the wire and at decode. composepost's high CP share (~87%) means the heavy node's otelcol decodes proportionally more bytes per second under SB than under vanilla, putting more memory pressure there. Tracepressure spans don't carry breadcrumbs (the spammer's children use `__bag.force_lp` which is stripped on the wire and isn't an SB priority marker), so their bytes-per-span are similar to vanilla — and SB's tracepressure-side load isn't materially heavier than vanilla's.

## Per-priority data (the comparison the priority processor needs to beat)

### Baseline run: `sbrev0_2k_spamN75_70pct` (the sweet spot)

Config: 2k rps dsb_sn + N=75 spam @ 4091 rps, memory_limiter limit=70% / spike=20% / check=100ms, GOMEMLIMIT=180MiB.

```
otelcol: 9 pods r=0  (zero OOMs)

composepost SDK (5-min steady):
  spans_received=5.84M  spans_sent=5.56M  spans_dropped=277k  (4.7%)
  cp_dropped / cp_received = 242k / 5.11M = 4.7%
  lp_dropped / lp_received =  35k /  730k = 4.7%
  → CP and LP refused at IDENTICAL rates (priority-blind)
  send_unavailable=1264

tracepressure cluster sum:
  recv=93.17M  drop=628k  (0.7%)
  cp_dropped / cp_received = 8k / 1.23M = 0.7%
  lp_dropped / lp_received = 620k / 91.94M = 0.7%

wrk2api achieved: 1999 rps (target 2000)
spam achieved: 4086 rps (target 4091)
```

This is the priority-blind shedding baseline. The priority processor's expected behavior under the same load: shift the drop ratio so CP loss is ~0% and LP absorbs the same total volume.

### Earlier high-pressure run: `sbrev0_2k_spamN100_gomemlim`

For reference (heavier load, 80% / 200MiB):

```
otelcol-side (cluster sum):
  recv_accepted = 6.77 M
  recv_refused  = 79.03 M   ← ~92% refused at receiver
  exp_failed    = 0
  queue_size    = 0          ← jaeger drained everything we sent

composepost SDK:
  spans_received=33.62M  spans_sent=27.12M  spans_dropped=6.50M  (19.3%)
  cp_dropped / cp_received = 5.69M / 29.4M = 19.3%
  lp_dropped / lp_received = 812k / 4.2M  = 19.3%
  send_unavailable = 25,365

tracepressure cluster sum:
  spans_received = 388.10M
  spans_dropped  = 156.21M  (40.3%)
  cp_dropped / cp_received = 1.54M / 5.76M  = 26.8%
  lp_dropped / lp_received = 154.66M / 382.34M = 40.5%
```

The composepost CP/LP drop equality (19.3% each) is the cleanest priority-blind signature. The priority processor's expected behavior under the same load: CP drop near 0%, LP drop higher to absorb the same total volume.

## Files

- `examples/dsb_sn/workflow/socialnetwork/TracePressureService.go` — synthetic spam service
- `runtime/plugins/otelcol/sb_processor.go` — SB SDK with `__bag.force_lp` escape hatch for forced LP classification
- `runtime/plugins/otelcol/pack.go` — `AttrForceLP` constant
- `utils/run_steady_with_spam.sh` — workload driver (warmup + ramp + steady + spam, with Phase 0 jaeger restart)
- `utils/spam_post.lua` — wrk POST helper
- `examples/dsb_sn/build_sbrev0/k8s/otelcol-sbrev0-ctr-daemonset.yaml` — has `GOMEMLIMIT=200MiB` and resource limits
- `examples/dsb_sn/build_sbrev0/docker/otelcol_sbrev0_ctr/config.yaml` — memory_limiter at 100ms / soft=50%
- Results dirs: `examples/dsb_sn/results_new_4/sbrev0_*` (per run)
