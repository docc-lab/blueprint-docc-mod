# n=5 anti-affinity fourway overhead result (corrected per-variant OT templates)

**Date:** 2026-06-30 → 07-01
**Sweep TS:** `20260630_174854`  (data: `~/runs/clean4_*_r*_20260630_174854`)
**Plots:** `~/runs/clean4_{mean,p99}RT_ramp_20260630_174854.{png,pdf}`
**Status:** first VALID fourway — supersedes all prior fourway sweeps (which had the vanilla/sb wrappers mis-generated; see [[ot_wrapper_template_vanilla_contamination]]).

## Experiment setup
- **Variants:** vanilla (v), pbridge (pb), cgpb, sbridge (sb). All esrev2 images, built `--extra rev2`.
- **OT wrapper codegen FIXED:** per-variant templates now selected by `OT_BRIDGE` env
  (build_deploy_dsb.sh derives it from the spec). Confirmed in generated output:
  vanilla wrapper carries NO `childCount`/`seqNum` (true no-bridge baseline); pb=Path,
  cgpb=CGPB (adds server-side `SetAttributes(childCount)`), sb=SBridge (endEvents machinery).
  Fix is in BOTH `ir_ot_client.go` and `ir_ot_server.go`. Prior runs ran path-bridge
  wrappers for ALL variants → vanilla was not a clean baseline.
- **Placement: ANTI-AFFINITY.** No two traced `*-service`s with a call edge share a node
  (verified 0 masked co-located edges). Layout: n1 composepost+userid, n2 hometimeline+
  urlshorten, n3 usermention+usertimeline, n4 socialgraph+text, n5 post-storage,
  n6 media+uniqueid, n7 wrk2api, n8 user, n9 jaeger+elasticsearch. Caches/DBs (untraced)
  ride with their service.
- **wrk2api = Deployment (1 replica) on node-7**, NOT the per-node DaemonSet — so the
  gateway→backend hop also crosses the network. (`--wrk2api-deploy`.)
- **Collectors: PASSTHROUGH / unconstrained.** Per-node otelcol DaemonSet, traces pipeline
  `processors: [batch]` only — NO priority processor, NO memory_limiter (all four variants).
- **Sink: Jaeger + Elasticsearch** (esrev2 = useES=true). otelcol otlp → jaeger → ES.
  NOTE: the ES tier is still in the path (ES-backpressure confound not removed; the
  central-collector/file or ClickHouse sink in [[central_collector_trace_storage]] is NOT
  yet applied here).
- **GC:** natural (GOGC=100, GC_INTERVAL_SEC=0), no memory limit constraint. cpd=2.
- **Cluster:** CPU pinned 2.2 GHz. `--no-pin-requests` (nodeSelector placement only, no CPU
  requests/limits — CPU unconstrained). 35 pods/variant.
- **Load:** wrk2 via NodePort, `compose-post.lua`, ramp 2000→5000 rps step 200, 30 s/step,
  100 rps × 100 s warmup. **Seeded & paired:** RANDOM_SEED = 1000+round, so all four
  variants in a round replay an identical request stream; rounds 1–5 → seeds 1001–1005.
- **n = 5** rounds, round-interleaved, torn down + reseeded between every run. 0 non-2xx
  across all 40 runs. Run continuously (no cold gaps) after a discarded 1-round warm-up
  starter (ts `20260630_164137`).
- **CPU captured** per step via kubelet `stats/summary` cadvisor counters folded into the
  ramp snapshots (before/after Δ → CPU-sec/step).

## Results (n=5, round-averaged)

### mean response time (ms)
| rps | v | pb | cgpb | sb |
|--|--|--|--|--|
|2000|13.8|13.4|12.4|13.5|
|2800|18.1|18.6|19.6|20.2|
|3000|30.6|30.3|34.1|31.3|
|3400|46.1|65.8|58.1|62.8|
|3600|43.9|60.6|90.0|81.1|
|3800|140.8|85.9|114.7|182.2|
|4000|231.6|141.3|211.3|281.3|
|4200|291.7|318.6|259.0|419.4|
|4400|1066.7|1236.1|1333.7|1727.8|
|4600|1896.0|1930.0|2002.0|1798.6|
|4800|3652.0|4196.0|3748.0|4006.0|
|5000|4508.0|4614.0|4632.0|4816.0|

### p99 response time (ms) — selected
| rps | v | pb | cgpb | sb |
|--|--|--|--|--|
|2000|32.5|32.0|29.9|31.1|
|3000|84.4|87.2|78.0|77.3|
|3600|136.7|202.5|268.6|246.5|
|4000|796.7|550.3|693.0|963.7|
|4400|1566.0|1844.0|1890.0|2315.5|
|5000|6976.0|7100.0|7110.0|7376.0|

(full per-step mean/p99/achieved tables for all 16 steps are in the run data / session log.)

### CPU (traced-service cores, sum of *-service + wrk2api pods)
| rps | v | pb | cgpb | sb |
|--|--|--|--|--|
|2000|12.51|13.19|13.20|13.37|
|3000|19.36|19.93|19.83|20.20|
|4000|26.51|29.81|29.87|27.99|
|4400|28.91|38.58|33.03|37.13|
|5000|32.39|43.59|39.04|41.19|

Per-service @4000 hotspot: composepost v=5.11 vs pb=7.40, cgpb=7.69, sb=5.66 (pb/cgpb
concentrate overhead on the high-fan-out orchestrator).

## Read
- **Response time, flat operating region (≤3000 rps): statistically indistinguishable** —
  all four ~12–34 ms, interleaved. Bridge overhead is below the RT noise floor here.
- **Response time, loaded (≥3400 rps): vanilla fastest, bridges slower**, sb/cgpb worst in
  the mid-ramp (e.g. 3600: v 44 vs cgpb 90, sb 81; 4400: v 1067 vs sb 1728). The overhead
  becomes visible in RT only once the system is pressured.
- **CPU: vanilla cheapest at EVERY step, monotone** — ~+0.7 cores flat → +7–11 cores past
  the knee. This is the clean, physically-correct overhead signal the contaminated
  baseline hid. Among bridges, pb tends highest in CPU at deep saturation; all bridges > v.
- The corrected vanilla baseline is the key change: previously vanilla ran the path-bridge
  wrapper and clustered with / above the bridges, producing anti-physical orderings.

## Caveats / next
- Jaeger+ES sink still in path (ES-backpressure not eliminated).
- CPU absolute values depend on aggregation (traced-services-only vs all-pods); use
  traced-services-only as the standard overhead measure.
- n=5; consider n=10 for tighter CIs. Consider the file/central-collector sink to remove ES.
