# Ramp Analysis — SB+priority vs Vanilla, threshold and cpd tuning

5-round-averaged 2k→4k ramp comparisons across vanilla+memlim and
SB+priority at multiple `cpd` and threshold values, exploring how the
3-tier priority processor's tuning parameters (`cpd` for the SB SDK's
CP/LP classification; `ultrasoft/soft/hard` percentages for the
priority processor) trade CP-retention against total throughput and
latency.

## TL;DR

- **CP preservation is the durable win.** At every congested RPS step
  (3000-4000), SB+priority drops CP at a small fraction of vanilla's
  uniform drop rate, in exchange for shedding LP harder. The
  cluster-wide end-to-end span throughput is essentially the same
  between SB+priority and vanilla — what changes is the *composition*
  of what gets through.
- **Sparser checkpoints win.** `cpd=6` (the maximum meaningful for
  cpost's trace depth) dominates every smaller `cpd` value on CP
  retention. At 4000 rps, cpd=6 loses only **3.3 %** of CP, vs
  vanilla's 50.8 % uniform drop — **15× better CP preservation**.
  Sparser CPs give the priority mechanism more LP to shed, so it can
  protect CPs harder under congestion.
- **There's a `cpd`/latency tradeoff at the cliff.** Higher `cpd`
  means better CP retention but also more latency overhead at 3000-
  3400 rps. cpd=2 has the cleanest pre-cliff latency profile (98 ms
  mean at 3400 vs cpd=6's 341 ms). cpd=4 is the middle ground.
- **Threshold tuning is secondary to `cpd` tuning.** Of the three
  threshold configurations tested at cpd=2 (35/50/70, 50/60/70,
  45/60/70), **45/60/70 has the best pre-cliff latency** while
  35/50/70 has the best raw CP retention. Below-cliff threshold
  choice ≈ shifts the latency-vs-CP curve.
- **Vanilla 50/70 vs 60/70 is irrelevant** — both vanilla memlim
  configs land within sd of each other.

## Setup

Standard DSB-SN deployment, two variants:
- `vesrev0` (vanilla SDK + memlim)
- `sb_esrev0` (SB SDK + 3-tier priority processor)

Common config:
- otelcol: 256 MiB container, GOMEMLIMIT=180 MiB, `check_interval=100ms`
- ES: `ES_JAVA_OPTS=-Xms4g -Xmx4g`, no container memory limit,
  `ES_BULK_WORKERS=10`, `COLLECTOR_QUEUE_SIZE=10000`
- otlp exporter: `sending_queue.queue_size=1000`, `block_on_overflow=false`,
  `num_consumers=10`

Workload pattern (`utils/ramp_with_snapshots.sh`):
1. Warmup at 100rps × 100s (cache priming)
2. 11-step ramp from 2000 to 4000 rps in steps of 200, each step
   running wrk for 60s with no pause between steps (BREAK_SECS=0)
3. Pre/post snapshots of every otelcol Prometheus counter and every
   service's SDK metrics for per-step delta computation

Each configuration was run 5 rounds. Each round starts with a fresh
teardown + apply + seed (37,624 follow relationships) for a clean ES
state. Within a single round, ES accumulates from one rps step to the
next, so absolute drop numbers reflect "drop% given that ES indexed
spans from all prior steps in this round."

## Configurations tested

### Vanilla + memlim

| label | spike_limit% | limit% | effective soft / hard |
|---|---:|---:|---:|
| vanilla 50/70 | 20 | 70 | soft trip at 50%, hard at 70% |
| vanilla 60/70 | 10 | 70 | soft trip at 60%, hard at 70% |

### SB + 3-tier priority

| label | cpd | ultrasoft / soft / hard | ultrasoft width | soft width |
|---|---:|---|---:|---:|
| SB cpd=2 35/50/70 | 2 | 35 / 50 / 70 | 15 pp | 20 pp |
| SB cpd=2 50/60/70 | 2 | 50 / 60 / 70 | 10 pp | 10 pp |
| SB cpd=2 45/60/70 | 2 | 45 / 60 / 70 | 15 pp | 10 pp |
| SB cpd=4 35/50/70 | 4 | 35 / 50 / 70 | 15 pp | 20 pp |
| SB cpd=5 35/50/70 | 5 | 35 / 50 / 70 | 15 pp | 20 pp |
| SB cpd=6 35/50/70 | 6 | 35 / 50 / 70 | 15 pp | 20 pp |

Ultrasoft: LP-only refusal. Soft: blanket refusal (memlim soft).
Hard: force GC + blanket refusal (memlim hard).

The 3-tier model is documented in `../PRIORITY_PROCESSOR_3TIER.md`.

## The `cpd` shift

`cpd=2` vs `cpd=3` produces dramatically different CP:LP ratios at the
heavy-fanout services. The convention here: a span at depth `d` is CP
iff `(d mod cpd) == 0`.

| cpd | cpost CP:LP | cluster CP:LP | LP-shedding headroom |
|---:|---:|---:|---|
| 3 | 7:1 | ~2.3:1 | minimal — CP dominates |
| 2 | 1:7 | ~1.1:1 | ample — LP slightly dominates |

The `cpd=3` runs in this analysis used the configurations from
earlier in the project (see `../PRIORITY_PROCESSOR_3TIER.md`).
The `cpd=2` work here is what unlocks the priority mechanism's
real value.

## Summary results (5-round avg)

### Mean app latency

| rps | V 50/70 | V 60/70 | SB cpd=2 35/50/70 | SB cpd=2 45/60/70 | SB cpd=4 35/50/70 | SB cpd=5 35/50/70 | SB cpd=6 35/50/70 |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 2000 | 14 ms | 13 ms | 16 ms | 14 ms | 13 ms | 14 ms | 14 ms |
| 2200 | 19 ms | 19 ms | 17 ms | 17 ms | 21 ms | 18 ms | 22 ms |
| 2400 | 24 ms | 21 ms | 18 ms | 19 ms | 22 ms | 23 ms | 24 ms |
| 2600 | 25 ms | 24 ms | 24 ms | 21 ms | 21 ms | 23 ms | 26 ms |
| 2800 | 30 ms | 29 ms | 34 ms | 28 ms | 34 ms | 37 ms | 36 ms |
| 3000 | 50 ms | 48 ms | 52 ms | 48 ms | 56 ms | 52 ms | 64 ms |
| 3200 | 75 ms | 59 ms | 65 ms | 73 ms | 76 ms | 83 ms | 101 ms |
| **3400** | 219 ms | 147 ms | **98 ms** | **78 ms** | 194 ms | 278 ms | 341 ms |
| **3600** | 450 ms | 539 ms | 730 ms | **182 ms** | 802 ms | 563 ms | 1.45 s |
| 3800 | 732 ms | 1.12 s | 813 ms | 1.97 s | 1.84 s | 1.83 s | 1.74 s |
| 4000 | 3.18 s | 3.26 s | 3.34 s | 3.60 s | 3.46 s | 3.34 s | 3.36 s |

### CP and LP drop rates (all SB rows at 35/50/70)

| rps | V 50/70 tot | SB cpd=2 CP / LP | SB cpd=4 CP / LP | SB cpd=5 CP / LP | SB cpd=6 CP / LP |
|---:|---:|---:|---:|---:|---:|
| 2800 | 0.0 % | 0.1 / 5.1 % | 0.0 / 1.6 % | 0.0 / 5.5 % | 0.0 / 4.5 % |
| **3000** | 15.4 % | **2.0** / 46.9 % | **2.0** / 53.4 % | 3.7 / 61.6 % | **0.4** / 47.3 % |
| 3200 | 39.6 % | 6.9 / 83.4 % | 4.9 / 75.7 % | 8.0 / 78.7 % | **3.7** / 64.3 % |
| 3400 | 30.7 % | 5.0 / 71.6 % | 2.8 / 57.8 % | 4.8 / 61.6 % | **2.3** / 55.7 % |
| 3600 | 43.0 % | 6.5 / 76.4 % | 9.4 / 83.8 % | 9.5 / 81.8 % | **4.3** / 68.6 % |
| 3800 | 42.2 % | 7.3 / 76.7 % | 4.3 / 75.0 % | 10.7 / 83.5 % | **4.3** / 70.5 % |
| 4000 | 46.2 % | 13.2 / 91.3 % | 10.2 / 87.1 % | 11.5 / 83.5 % | **3.3** / 71.9 % |

### Total drop rates (all SB rows at 35/50/70)

| rps | V 50/70 | SB cpd=2 | SB cpd=4 | SB cpd=5 | SB cpd=6 |
|---:|---:|---:|---:|---:|---:|
| 2800 | 0.0 % | 2.5 % | 0.8 % | 2.6 % | 2.8 % |
| 3000 | 15.4 % | 23.4 % | 28.9 % | 31.3 % | 29.3 % |
| 3200 | 39.6 % | 43.4 % | 41.9 % | 41.7 % | 41.1 % |
| 3400 | 30.7 % | 36.7 % | 31.6 % | 31.9 % | 35.3 % |
| 3600 | 43.0 % | 39.8 % | 48.4 % | 43.9 % | 44.0 % |
| 3800 | 42.2 % | 40.4 % | 41.3 % | 45.4 % | 45.2 % |
| 4000 | 46.2 % | 50.4 % | 50.5 % | 45.8 % | 45.7 % |

### Span throughput to ES (otelcol → jaeger, sps)

| rps | V 50/70 | V 60/70 | SB 35/50/70 | SB 45/60/70 |
|---:|---:|---:|---:|---:|
| 2000 | 45,928 | 45,901 | 45,924 | 45,919 |
| 2400 | 54,576 | 54,580 | 54,583 | 54,595 |
| 2800 | 62,700 | 62,749 | 60,554 | 62,316 |
| 3000 | 44,297 | 43,556 | 43,875 | 41,885 |
| 3200 | 43,844 | 40,604 | 38,984 | 36,779 |
| 3400 | 40,415 | 43,602 | 44,876 | 44,388 |
| 3600 | 44,341 | 42,733 | 46,583 | 44,516 |
| 3800 | 41,565 | 41,735 | 43,559 | 40,624 |
| 4000 | 38,927 | 40,084 | 39,961 | 38,976 |

End-to-end throughput converges around **40-46 k sps**, set by ES's
indexing ceiling. All four configurations bump into roughly the same
hard cap; the choice of which traces to ship is what differentiates
them.

## Insights

### 1. Priority's value is composition, not total throughput

At 3000 rps:
- Vanilla 50/70 ships 44.3 k sps, drops 15.4 % uniformly (CP loss = 15.4 %)
- SB 45/60/70 ships 41.9 k sps (~5 % lower), drops 0.6 % CP and 38.7 % LP

At 3600 rps:
- Vanilla 50/70 ships 44.3 k sps, drops 43.0 % uniformly
- SB 45/60/70 ships 44.5 k sps, drops 7.5 % CP and 66.2 % LP

The trade is consistent: comparable total bytes-out, but SB+priority
**preserves the spans that carry the bridge-state checkpoints** at
the cost of dropping LP harder. Whether that's a good trade depends
on what you're using the trace data for — for the SB/Bridges design
goal of preserving CP-marked checkpoints, this is exactly right.

### 2. Sparser checkpoints win for CP preservation

CP:LP cluster-wide ratio shifts with `cpd`. The earlier `cpd=3` runs
(documented in `../PRIORITY_PROCESSOR_3TIER.md`) showed CP drops of
25-37 % at 3000-3400 rps because the LP-shedding mechanism had
checkpoints making up most of the span volume — only 1/8 was LP, so
the mechanism had little to sacrifice.

Each subsequent `cpd` value tested unlocks more LP volume:

| cpd | cpost CP:LP | cluster CP:LP | direction |
|---:|---:|---:|---|
| 3 | 7 : 1 | ~2.3 : 1 | CP-heavy (priority mechanism fuel-starved) |
| 2 | 1 : 7 | ~1.10 : 1 | balanced |
| 4 | ~1 : 11 | ~0.91 : 1 | slightly LP-heavy |
| 5 | (similar to cpd=2) | ~1.10 : 1 | balanced (noisy, doesn't track linearly) |
| 6 | (sparser still) | ~0.62 : 1 | LP-heavy (1.6× LP per CP) |

CP drop% across all `cpd` values (5-round avg, 35/50/70 thresholds):

| rps | cpd=2 | cpd=4 | cpd=5 | **cpd=6** |
|---:|---:|---:|---:|---:|
| 3000 | 2.0 % | 2.0 % | 3.7 % | **0.4 %** |
| 3200 | 6.9 % | 4.9 % | 8.0 % | **3.7 %** |
| 3400 | 5.0 % | 2.8 % | 4.8 % | **2.3 %** |
| 3600 | 6.5 % | 9.4 % | 9.5 % | **4.3 %** |
| 3800 | 7.3 % | 4.3 % | 10.7 % | **4.3 %** |
| 4000 | 13.2 % | 10.2 % | 11.5 % | **3.3 %** |

**cpd=6 dominates at every congested RPS step.** At 4000 rps, vanilla
loses 50.8 % of CP uniformly; cpd=6 loses only 3.3 % — **15× better
CP preservation**.

The non-monotonicity at `cpd=5` (worse than both cpd=4 and cpd=6) is
puzzling — likely a workload-specific artifact of the depth modulo
classification interacting with cpost's call graph shape. Even so,
the overall trend "sparser → better" is robust.

For real workloads, tune `cpd` such that checkpoints are *sparse*
(CP a small fraction of total span volume). The classical Bridges
intuition "checkpoints are rare but valuable" also gives the priority
mechanism the most leverage.

### 2b. Sparser cpd costs pre-cliff latency

CP retention isn't free. Higher `cpd` causes more LP volume to push
through the otelcol, which means the SDK has more work per span on
the bridge-state path, and the otelcol's priority processor is busier
making per-batch decisions on a larger LP stream. Pre-cliff (≤3400
rps), this translates to small but real latency overhead.

Mean latency at the steps approaching the cliff (5-round avg):

| rps | V 50/70 | cpd=2 | cpd=4 | cpd=5 | cpd=6 |
|---:|---:|---:|---:|---:|---:|
| 3000 | 50 ms | 52 ms | 56 ms | 52 ms | 64 ms |
| 3200 | 75 ms | 65 ms | 76 ms | 83 ms | 101 ms |
| 3400 | 219 ms | **98 ms** | 194 ms | 278 ms | 341 ms |

Below 3000 the configs are indistinguishable. At 3200-3400 the cost
of CP-preservation shows up: **cpd=2 has the best pre-cliff latency**
(98 ms at 3400 vs vanilla 219 ms, cpd=6's 341 ms). **cpd=4 splits
the difference** between cpd=2's latency advantage and cpd=6's CP
advantage — ~3 % CP at 3400 with 194 ms mean.

Operating point recommendation:
- **If CP preservation is paramount (research / audit traces)**: cpd=6.
- **If latency matters more than CP coverage (production SLOs)**: cpd=2.
- **If you want a balanced profile**: cpd=4.

### 3. Threshold tuning is secondary but real

Comparing 35/50/70, 50/60/70, 45/60/70 (all `cpd=2`):

| metric | 35/50/70 | 50/60/70 | 45/60/70 |
|---|---|---|---|
| best CP retention (3000-3400) | ✓ (2.0/6.9/5.0 %) | (4.8/10.0/10.9 %) | (0.6/11.3/7.4 %) |
| 3400-3600 mean latency | (98/730 ms) | (160/999 ms) | **78/182 ms** ✓ |
| 2800 pristine | (5 % LP shed early) | ✓ (0 %) | ✓ (0.4 % LP) |
| degradation at 4000 | best (13.2 %) | worst (19.7 %) | mid (16.5 %) |

- **45/60/70 best for the operational range** (3400-3600), where the
  combination of "later onset" + "wide ultrasoft band" minimizes both
  latency and CP drops at the cliff.
- **35/50/70 best for pure CP-retention**, especially under sustained
  pressure (3800-4000).
- **50/60/70 best at the cleanest sub-cliff load** (2800), but loses
  the latency benefit at 3400+.

### 4. Vanilla 60/70 vs 50/70 — basically no difference

Increasing memlim's soft threshold from 50 to 60 % doesn't meaningfully
change vanilla's behavior — both have rough total drops of ~15-50 %
across the cliff. Vanilla's improvement was within sd of zero.

### 5. App-level saturation hits at ~3800-4000 regardless

All four configurations cross from "manageable" (sub-second p99) to
"saturated" (multi-second p50) between 3800 and 4000 rps. The app
itself is the bottleneck at that load — the trace-pipeline shedding
choice can't change that.

## Plots

### Headline plots (cpd sweep 2/4/5/6 vs vanilla 50/70, all at 35/50/70 SB thresholds)

| file | what |
|---|---|
| `plots/ramp_cpd_sweep_panels.png` | 2×2: app rps, span throughput, mean (log), p99 (log) |
| `plots/ramp_cpd_sweep_drops_cp.png` | **CP drop% across cpd values vs vanilla total** — the headline result |
| `plots/ramp_cpd_sweep_drops_lp.png` | LP drop% across cpd values vs vanilla total |
| `plots/ramp_cpd_sweep_drops_total.png` | Total drop% across all configs |
| `plots/ramp_cpd_sweep_latency_precliff.png` | linear-scale mean + p99 for ≤3400 rps (the cpd/latency tradeoff) |

### Threshold tuning at cpd=2 (vs vanilla 60/70)

| file | what |
|---|---|
| `plots/ramp_4configs.png` | 2×2: vanilla 50/70 + vanilla 60/70 + SB cpd=2 35/50/70 + SB cpd=2 45/60/70 |
| `plots/ramp_4configs_drops.png` | drop% lines for the 4 configs above |
| `plots/ramp_v6070_vs_sb456070.png` | 2×2 head-to-head: vanilla 60/70 vs SB cpd=2 45/60/70 |
| `plots/ramp_v6070_vs_sb456070_drops.png` | drops, same head-to-head |

### Legacy / cpd=3 era

| file | what |
|---|---|
| `plots/ramp_summary_4panel.png` | earlier 4-panel using SB cpd=3 + vanilla 50/70 |
| `plots/ramp_cp_lp_drops_5rounds.png` | CP/LP drops with the cpd=3 SB config |
| `plots/ramp_comparison_5rounds.png` | full-range mean+p99 (log) for cpd=3 |
| `plots/ramp_comparison_5rounds_zoom.png` | linear-scale zoom ≤3400 rps for cpd=3 |
| `plots/ramp_latency_cpcap_v5.png` | latency 3-panel for cpd=3 vs vanilla |
| `plots/ramp_throughput_cpcap_v5.png` | throughput 2-panel for cpd=3 vs vanilla |

## Data

5-round aggregates saved as TSV in `data/`. The columns are:
`step_rps, ach, ach_sd, mean_ms, mean_ms_sd, p99_ms, p99_ms_sd,
exp_sps, exp_sps_sd, cp_pct, cp_pct_sd, lp_pct, lp_pct_sd, tot_pct, tot_pct_sd`.

Files:
- `agg_vanilla_5070.tsv` — vanilla 50/70
- `agg_sb_cpd2.tsv` — SB cpd=2 35/50/70
- `agg_sb_cpd4.tsv` — SB cpd=4 35/50/70
- `agg_sb_cpd5.tsv` — SB cpd=5 35/50/70
- `agg_sb_cpd6.tsv` — SB cpd=6 35/50/70

Legacy aggregates (cpd=3, partial schemas — kept for reference):
- `vanilla_avg.tsv` — vanilla 50/70
- `sb_avg.tsv` — SB cpd=3 35/50/70
- `sb_cpcap_5rounds.tsv` — SB cpd=3 35/50/70 with cp/lp capture
- `ramp_throughput_5rounds.tsv` — throughput numbers for cpd=3 sweep

Per-round run directories are under `../runs/` with the naming pattern:
- vanilla: `ramp_vanilla_memlim[_6070]_2to4k_r{1..5}_<date>`
- SB cpd=2 35/50/70: `ramp_sb_3tier_cpd2_2to4k_r{1..5}_<date>`
- SB cpd=2 50/60/70: `ramp_sb_3tier_cpd2_506070_2to4k_r{1..5}_<date>`
- SB cpd=2 45/60/70: `ramp_sb_3tier_cpd2_456070_2to4k_r{1..5}_<date>`
- SB cpd=4 35/50/70: `ramp_sb_3tier_cpd4_355070_2to4k_r{1..5}_<date>`
- SB cpd=5 35/50/70: `ramp_sb_3tier_cpd5_355070_2to4k_r{1..5}_<date>`
- SB cpd=6 35/50/70: `ramp_sb_3tier_cpd6_355070_2to4k_r{1..5}_<date>`

Each run dir contains:
- `snapshots.tsv` — pre/post counter snapshots per step
- `summary.tsv` — wrk per-step achieved rps / mean / p99 / non-2xx
- `ramp.log` — raw wrk output

## Reproducing

Scripts in this directory drive the sweeps:
- `run_5_sb_cpd2.sh` — 5 SB ramps with cpd=2, thresholds 35/50/70
- `run_5_sb_cpd2_506070.sh` — same with thresholds 50/60/70
- `run_5_sb_cpd2_456070.sh` — same with thresholds 45/60/70
- `run_5_vanilla_6070.sh` — 5 vanilla ramps with memlim spike=10/limit=70

Each script: rebuilds the otelcol image with the new config, then
loops 5×{teardown → apply → seed → ramp_with_snapshots.sh}.

The underlying ramp driver is `../../../utils/ramp_with_snapshots.sh`.
The teardown+apply+seed wrapper is `../../../utils/teardown_seed_ramp.sh`.

## Caveats

1. **ES accumulation within a sweep**: ES indexes ~30-40 M docs over
   each round's 11 steps. Drop numbers at higher RPS reflect both
   genuine pressure AND ES degradation from prior steps. A "fresh-ES"
   2k or 3k baseline would show lower absolute drops than the sweep-
   context numbers reported here. Cross-variant comparisons within
   the same sweep methodology remain valid.
2. **Per-step counter wraparound**: a few rounds saw individual
   otelcol pod restarts between snapshots, producing negative per-step
   deltas. The aggregation excludes those values when computing throughput
   averages. Doesn't affect SDK-side cp/lp/tot numbers (computed at SDK
   level, which doesn't restart on the otelcol pod cycle).
3. **Cluster-specific saturation**: the ~3800-4000 cliff is specific
   to this hardware (9 worker nodes, 4 CPU/node). Different cluster
   sizes will shift the cliff but the relative comparisons between
   configurations should hold.
