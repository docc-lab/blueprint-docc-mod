# ClickHouse backend with a gateway collector (2026-09-22, evening)

Tomislav-RetCtx. Jaeger v1 + Elasticsearch replaced by: agents (unchanged, admission profile) ->
node-9 GATEWAY collector (same otelcontribcol image, same priority processor / memory_limiter,
batch 20000 / 1 s, ClickHouse exporter) -> ClickHouse 25.8 (16 CPU, 64 GiB, node-9). A Jaeger-API
shim (`utils/retctx_jaeger_shim.py`, python:3.12-slim) keeps the pod name `jaeger-<variant>-ctr`
and serves /api/services, /api/traces and :14269/metrics from ClickHouse, so the runner, the
spread sampler, `eligible2.py` and `pb_jaeger_recon` are unchanged. Bridges agents forward the
per-batch `bridges-priority` gRPC header to the gateway (batch `metadata_keys` + `headers_setter`
extension), so the gateway's priority processor sees LP vs HP. `derive_dsb_sn_nw.py --backend
clickhouse [--gateway-cpu N]`. Attribute fidelity verified through the shim: `_br`/`_d` binary,
`_rc` string, 23-span traces, no dangling parents.

Why: at 4,000 req/s Jaeger's 10,000-span queue overflowed on Elasticsearch merge stalls, the
agents' 5 s retry backoff idled their exporter consumers, and both vanilla (20.6 percent refused)
and bridges lost data with the drain collapsing (79.8k clean -> 71.5k under pushback). A 200 ms
retry made it worse (25.5 percent). ClickHouse takes 158k spans/s at 4.4 of 16 cores with no
pushback.

## Uncapped gateway (4 CPU), 600 s points, vanilla only
root `retctx-nwe2e-sustainch-on-20260922T185738Z`

| offered | agents in = out spans/s | refused | gateway CPU | ClickHouse CPU |
|---:|---:|---:|---:|---:|
| 4000 | 91,962 | 0 | 2.01 | 2.14 |
| 5000 | 114,952 | 0 | 2.30 | 3.31 |
| 6000 | 136,559 | 0 | 2.68 | 3.72 |
| 7000 | 157,690 | 0 | 3.05 | 4.42 |

Nothing in the pipeline saturates below the application knee. Gateway cost ~0.64 core +
0.015 core per 1k spans/s; agent heaps 28-62 MiB vs soft 102. The gateway's CPU limit is
therefore the knob that makes it a fixed-rate sink.

## Capped gateway (2 CPU), 300 s points, both arms reverse ON
40/60 = admission6040 (agents refuse-all 102 MiB, gateway 819 MiB), root
`retctx-nwe2e-sustaingw-on-20260922T194711Z` (cgpb killed after 3500).
55/80 = admissionotel (OTel Helm defaults: limit 80 / spike 25 -> refuse-all 141 MiB, GC 205;
gateway 1126 / 1638), root `retctx-nwe2e-sustainotel-on-20260922T204224Z`.

| offered | v 40/60 refused | v 55/80 refused | cgpb 40/60 cp / LP / all | cgpb 55/80 cp / LP / all | gateway 55/80 (cgpb) |
|---:|---:|---:|---|---|---|
| 3000 | 0 | 0 | 0 / 22.6 / 15.1 | 0 / 0 / 0 | 1.67 cores, shed 0 |
| 3500 | 0 | 0 | 0 / 34.7 / 23.3 | 0 / 15.0 / 10.0 | 2.00, shed 10.9 pct of LP |
| 4000 | 0 | 0 | -- | 0 / 40.2 / 27.0 | 1.99, 19.1 |
| 4500 | 0 | 0 | -- | 0 / 54.0 / 36.2 | 2.00, 23.7 |
| 5000 | 20.98 (all spans) | 18.77 (all spans) | -- | 0 / 66.2 / 44.4 | 2.00, 28.1 |

- Vanilla: clean to 4500; at 5000 the gateway pins at 2.00 cores, its heap crosses its soft
  limit, its memory_limiter refuses (backpressure, retried by agents, nothing lost there), the
  agents' consumers stall in backoff, agent heaps cross soft, agents refuse the SDK (loss).
  Raising the limits to 55/80 delayed the same chain by about a minute.
- cgpb at 40/60: the composepost agent's derived LP-shed level (45.8 MiB) sat below its steady
  heap (47 MiB), so it shed 64 percent of its LP at 3000 with an idle pipeline. At 55/80 that
  floor is gone (3000 fully clean).
- cgpb at 55/80: ZERO HP refused on all eight agents and the gateway at every rate, zero
  refuse-all seconds; at 5000 the agents refused 0 of 11.3M HP while shedding 66 percent of LP,
  agent heap peaks 104-143 MiB vs soft 141. cgpb reaches the 2-core cap at ~80k spans/s vs
  ~90-100k for vanilla (heavier spans + priority classification), so it sheds LP from 3500
  where vanilla is still clean.
- Chain confirmed end to end: gateway CPU cap -> gateway sheds LP at its level -> rejected
  batches retried by agents -> agent heaps rise -> agents shed LP at their levels -> HP untouched.
  The 5-30 s retry backoff between agents and gateway under-feeds the gateway in bursts.

Scripts: scratchpad `sustainch.sh`, `sustaingw.sh`, `sustainotel.sh`, `sustain_status.py <tag>`,
`live_line.py`, `sweep_monitor2.sh`, `qfull_all.py`, `vanilla_timeline.py`.

## Bursty runs at mean 4500 (2026-09-22 21:55-23:41 UTC)
Roots `retctx-nwe2e-burst4500-on-20260922T215518Z` (v pb cgpb sb) and
`retctx-nwe2e-burst4500-off-20260922T225550Z` (pb cgpb sb). ClickHouse backend, gateway 2 CPU,
admissionotel 55/80, 600 s, `-D pareto` alpha 1.5 cap 2.0 epoch 10 s seed 1001: realised mean
4334 req/s, 60 epochs 3328..6239, 15 epochs (25 percent) above vanilla's 5000 boundary, 3 above
6000; p99 184-257 ms. Identical schedule for every case.

| case | arm | all spans refused (agents) | checkpoints refused | LP refused | gateway LP shed | eligible (spread sample, 100 traces) |
|---|---|---:|---:|---:|---:|---|
| v    | -   | 14.34 pct (8.6M) | n/a | n/a | memory_limiter refused 14.3M (backpressure) | 50 pct COMPLETE (span counts 12/15/20/23) |
| pb   | ON  | 37.44 | 0.03 | 55.8 | 27.5 | 100 (47 without undetermined interior gaps) |
| cgpb | ON  | 38.18 | 0.00 | 56.9 | 27.9 | 100 (74) |
| sb   | ON  | 44.57 | 0.00 | 66.5 | 29.2 | 100 (46) |
| pb   | OFF | 30.63 | 0.06 | 63.5 | 31.1 (+0.16 pct of HP at the gateway) | 100 |
| cgpb | OFF | 30.68 | 0.12 | 63.5 | 31.2 | 100 |
| sb   | OFF | 35.34 | 0.48 | 72.9 | 34.1 | 90 (84) |

Checkpoint-loss attribution (`scratchpad/cp_census.py`): pb ON 5,858 HP on node-1 only (heap
peak 151 vs soft 141, GC-peak crossings); pb OFF node-4 18,581 + gateway 49,078 (its heap peak
1119 vs 1126, 2 refuse-all seconds); cgpb OFF node-1 5,333 + node-2 32,435; sb OFF node-2 88,789
(12 refuse-all s) + node-4 58,905 + node-7 2,272. Response path ON preserves checkpoints better
than OFF for every bridge (0.03 / 0.00 / 0.00 vs 0.06 / 0.12 / 0.48 percent). Vanilla loses 14
percent of all spans indiscriminately and half its sampled traces are incomplete; the bridges
lose at most half a percent of checkpoints and every ON-arm sampled trace is
reconstruction-eligible. The eligibility numbers are from 100 traces spread over the 600 s
(10 per 60 s sub-window) and carry the undetermined-interior caveat noted above.

## Bursty 4500 RERUN with 5000-trace uniform samples (2026-09-23 02:06-04:02 UTC)
Roots `retctx-nwe2e-burst4500b-on-20260923T020655Z` (v pb cgpb sb) and
`retctx-nwe2e-burst4500b-off-20260923T031255Z` (pb cgpb sb). Identical settings to the first
bursty run; the only change is the trace sample: 5000 traces per point drawn uniformly across
ten 60 s sub-windows (`RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random`; the shim
orders by cityHash64(TraceId)) instead of Jaeger's 100 most-recent. The 100-trace figures in the
previous section are low-resolution and biased toward each sub-window's last fraction of a
second (checked on sb OFF: 90 -> 96 percent eligible, 30 -> 6 percent intact).

| case | arm | all spans refused | checkpoints refused (run 1 / run 2) | LP refused | eligible, 5000 traces (anchor lost / leaf ckpt lost / undetermined) |
|---|---|---:|---|---:|---|
| v    | -   | 14.25 pct | n/a (14.34 / 14.25 all spans) | n/a | 58.7 pct COMPLETE |
| pb   | ON  | 37.66 | 0.03 / 0.12 | 56.1 | 100 (0 / 0 / 37) |
| cgpb | ON  | 38.16 | 0.00 / 0.00 | 56.9 | 100 (0 / 0 / 36) |
| sb   | ON  | 44.25 | 0.00 / 0.28 | 65.8 | 99 (0 / 1 / 51) |
| pb   | OFF | 30.35 | 0.06 / 0.59 | 62.4 | 97 (2 / 3 / 1) |
| cgpb | OFF | 30.80 | 0.12 / 0.37 | 63.5 | 98 (1 / 2 / 1) |
| sb   | OFF | 35.07 | 0.48 / 0.70 | 72.0 | 97 (2 / 2 / 1) |

LP and all-span refusal fractions reproduce run to run within 0.4 pt (fixed burst seed). The
checkpoint fractions are the run-to-run variable: every one of them is a handful of refuse-all
seconds on one or two agents whose GC peak overshoots the 141 MiB line (run 2: pb ON node-1
2 s / 159 MiB peak; sb ON node-4 10 s / 164; pb OFF node-4 12 s + node-1 4 s + node-2 5 s; cgpb
OFF node-4 9 s; sb OFF node-1 19 s + node-2 8 s + gateway 1 s). The gateway refused HP in one
point per run (pb OFF run 1, sb OFF run 2), both under 0.2 percent. ON <= OFF on checkpoints for
every bridge in both runs. `cp_census.py` per point; eligibility via `eligible2.py` on the
settled 5000-trace samples.

Next: exact trace-level census from the SDK (refused_ids.go), images rebuilt from
`retctx-nw-census-20260923T040259Z`, campaign `burst4500c`.
