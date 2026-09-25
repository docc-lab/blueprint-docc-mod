# Stationary 300 s runs at ~5,500 req/s: bursty vs fixed (2026-09-22)

Collectors: admission6040 (1 CPU, 256Mi, GOMEMLIMIT 230MiB; priority soft 40 / hard 60 = 102/154 MiB of the cgroup; vanilla memory_limiter 60/20).
Bursty: `-D pareto` alpha 1.5 cap 4 epoch 1 s, seed 1001, nominal 6000 -> realised ~5,560; epochs 3,502..13,849.
Fixed: historical wrk2 generator at 5,570 -> realised 5,455.
Roots: retctx-nwe2e-burst6k-{on,off}-*, retctx-nwe2e-fixed5570-{on,off}-*.

## Checkpoint loss (SDK cp_dropped == collector hp_refused; send_deadline 0 everywhere)

| case      | bursty cp% | fixed cp% | bursty worst collector | fixed worst collector |
|-----------|-----------:|----------:|-----------------------:|----------------------:|
| v (refused, all spans) | 21.1 | 23.3 | -- | -- |
| pb ON     | 2.49 | 3.66 | 4.92 composepost | 7.60 composepost |
| cgpb ON   | 1.71 | 1.45 | 4.82 composepost | 4.24 composepost |
| sb ON     | 5.16 | (not run) | 9.49 composepost | -- |
| pb OFF    | 3.47 | 5.23 | 7.85 media/uniqueid (HP-only leaf) | 17.75 hometimeline/urlshorten |
| cgpb OFF  | 3.32 | 4.88 | 6.16 composepost | 10.76 post-storage (HP-only leaf) |
| sb OFF    | 2.41 | (not run) | 6.60 socialgraph/text | -- |

Fixed-rate reproduces (or exceeds) the bursty losses in every pair -> the loss is a property of
~5 minutes at this mean, not of burstiness. In every case loss is zero for the first 2-4 minutes,
then refuse-all on whichever collector fills first (queue build-up, not spikes). Bursts cost
latency only: p99 ~1.1-1.5 s vs ~55-65 ms fixed. The 30 s ramp underreports loss at every rate
>= ~5,500 for vanilla and bridges alike.

## Reconstruction eligibility from actual traces (true checkpoint set: window roots + 8 leaves)

Spread samples (10 traces per 30 s sub-window); range = [also no unclassifiable interior loss, eligible].

| case             | eligible | complete | note |
|------------------|---------:|---------:|------|
| v fixed          | -- | 33% | complete for t<120 s, 0% after |
| pb ON fixed      | 70-71% | 0% | |
| cgpb ON fixed    | 85-100% | 0% | 0 leaf trusses lost across 800 leaf positions |
| pb OFF fixed     | 68% | 12% | |
| cgpb OFF fixed   | 70% | 10% | undetermined 0% |
| pb OFF bursty    | 70% | 20% | |
| cgpb OFF bursty  | 78% | 33% | |
| sb OFF bursty    | 67% | 11% | |
| ON bursty (end-of-window slice only) | pb 52 / cgpb 63 / sb 12 | 0% | slice at t=243-262 s, not a run average |

Earlier figures that counted only named window-root anchors (92/92/78/83/98/94/70/100) OVERSTATE
eligibility: they cannot see a lost leaf checkpoint (OFF) or a lost promoted-carrier truss (ON).
Script: scratchpad eligible2.py. Deployment left running after the runs: cgpb, REVERSE_TRUSS=off.

## Figure

`checkpoint-loss-timeline.{pdf,png,svg}` -- cumulative fleet loss of TRACE-CRITICAL spans over each
300 s point, bursty (left) vs fixed (right), broken y axis (0-6 percent bridges, 18-25 percent vanilla).
- Bridges (PB/CGPB/SB, paper palette; response path on = solid, off = dashed; one marker shape per
  bridge every 50 s because CGPB vs SB is only dE 6.7 under protanopia): checkpoints refused,
  `hp_refused / (hp_admitted + hp_refused)`, from the collectors' own per-second
  `priority_processor_metrics` lines, fleet-summed over the 8 collectors.
- Vanilla (black): ALL spans refused, since any lost span breaks a vanilla trace. The stock
  memory_limiter has no per-second counter and the SDK logs are `--tail=2000` truncated for the busy
  services, so the series is built from three measured inputs (`scratchpad/vanilla_timeline.py`):
  every refuse/resume transition in each collector's log (state machine incl. the silent end after a
  forced GC, `memorylimiter.go:171-213` v0.139.0), the offered rate per second (wrk2 burst trace for
  the bursty point, constant for the fixed point), and each collector's refused total over the point
  (prometheus after-before). A refusing memory_limiter refuses everything, so
  refused_c(t) = total_c x R(t)1[refusing_c(t)] / sum_t R(t)1[refusing_c(t)].
  Checks against measured data: on the fixed point each collector's refusing-time share equals its
  refused share within 0.1 pt (e.g. node-1 39.8 vs 39.7, node-4 23.5 vs 23.5); on the four collectors
  whose SDK per-second logs are complete (nodes 3/5/6/8) onsets agree to the second and the cumulative
  deviation stays under 0.5 percent of the node's spans. Fleet onset: 137 s bursty, 105 s fixed;
  final 21.1 / 23.3 percent. Sub-second arrival lag (request latency + SDK batch) is ignored.
- Data: scratchpad `ckpt_timeline.json` (keys `env|kind|arm`, vanilla under `env|v|-`).

## Store-side pushback (added after the fact, 2026-09-22)

Jaeger v1 (`--collector.queue-size` 10,000 SPANS = ~0.1 s of traffic) returned
`Unavailable: sending queue is full` to the collectors' OTLP exporter in EVERY stationary point;
the exporter retries after a 5 s initial backoff (`retry_on_failure.initial_interval`, max 30 s),
so nothing is lost downstream (send_failed 0, Jaeger spans_dropped 0) but each rejection idles one of
the 10 sending-queue consumers for >= 5 s, the collector's queue (1000 batches x <= 8192 spans)
absorbs the backlog into heap, and the memory limiter / priority processor then refuses at the
receiver. Rejections per 300 s point (first rejection, s): v 1255 (131) bursty, 1476 (99) fixed;
pb ON 348 (254) / 621 (213); cgpb ON 235 (271) / 533 (198); sb ON 608 (220); pb OFF 948 (170) /
1295 (118); cgpb OFF 931 (179) / 1456 (102); sb OFF 852 (183). In both vanilla points the first
collector refusal follows the first Jaeger rejection by ~6 s (one backoff). Elasticsearch write pool
26/26 active with a queue at most snapshots, 0 rejected; Jaeger 7-7.7 of 12 cores, ES 14-16 of 26.
So at ~5,500 req/s the collectors' refusals are DOWNSTREAM-caused: the store absorbs ~77-98k spans/s
but not without recurring stalls, and the 5 s backoff converts each stall into collector heap growth.
An earlier note in this thread ("store is not the bottleneck at 5,500") was wrong.
