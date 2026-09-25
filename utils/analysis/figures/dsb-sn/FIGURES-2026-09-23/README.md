# Bursty-load figures from the SDK refused-trace census (2026-09-23)

Campaign `burst4500c`: ON root `retctx-nwe2e-burst4500c-on-20260923T042542Z` (v pb cgpb sb),
OFF root `retctx-nwe2e-burst4500c-off-20260923T053149Z` (pb cgpb sb). Images from
`retctx-nw-census-20260923T040259Z` (SDK plugin `runtime/plugins/otelcol/refused_ids.go`: every
service records the trace IDs of spans the agent refused, by class). ClickHouse backend, node-9
gateway collector capped at 2 CPU, agents admissionotel (OTel Helm default 55/80), mean 4500 req/s,
Pareto bursts alpha 1.5 cap 2.0 epoch 10 s seed 1001 (60 epochs 3328..6239 req/s, realised mean
4334, 15 epochs above vanilla's 5000 fixed-rate boundary), 600 s per case, identical schedule for
every case. Loss downstream of the agents is zero (send_failed / enqueue_failed 0 at agents and
gateway), so the union of the agents' refused-trace records is the exact set of lost traces.

## census-outcome.{pdf,png,svg}
Exact per-trace outcome (~2.6M traces per case; `scratchpad/census_outcome.py`, data
`census_outcome.json`): intact = no span lost; LP lost only = reconstructable; checkpoint lost =
reconstruction-ineligible (vanilla: any span lost = broken). Number at the right of each bar is
the broken share.

| case | arm | intact | LP lost only | broken |
|---|---|---:|---:|---:|
| v    | -   | 59.07 | -- | 40.93 |
| pb   | ON  | 4.11 | 95.89 | 0.000 |
| pb   | OFF | 12.38 | 86.70 | 0.918 |
| cgpb | ON  | 3.33 | 96.67 | 0.000 |
| cgpb | OFF | 12.19 | 87.81 | 0.000 |
| sb   | ON  | 1.43 | 97.49 | 1.078 |
| sb   | OFF | 5.63 | 87.57 | 6.802 |

Where the broken traces come from (per agent, `refused_union.py`): vanilla node-1 (composepost)
33.5 percent of all traces, node-4 9.9, node-2 4.4; sb ON: all 28,013 on node-7 (wrk2api, the root
span, always a checkpoint) -- traces a store-side sample can never see because their root is
gone; pb OFF: all 23,849 on node-4 (socialgraph + text); sb OFF: 176,805 broken traces from 0.72 percent of HP spans, because in the OFF arm the refused HP spans are leaf checkpoints, one per trace per leaf service, so each one breaks a distinct trace (per-agent split in census_sb_off.out / refused_union.py). Cross-check against the 5000-trace uniform
samples: v 59.07 vs 60.1 complete; pb ON any-loss 95.89 vs 95.74; cgpb ON 96.67 vs 96.92; pb OFF
HP-loss 0.918 vs 0.78; sb ON HP-loss 1.078 vs 0.02 (root-loss blind spot).

## burst-mechanism.{pdf,png,svg}
One 600 s point (v and cgpb ON, same burst schedule). Top: offered rate per 10 s epoch (exact,
wrk2 trace). Middle: vanilla spans refused per epoch as a share of arriving spans (memory_limiter
refuse/resume transitions x arrivals, anchored to per-agent refused totals; method validated on the
fixed-rate runs to <0.5 percent). Bottom: cgpb ON, same share split into LP (light) and checkpoints
(dark), from the priority processor's per-second counters (exact); 0 of 19.7M checkpoints refused.
Vanilla starts losing at ~85 s, once the first two bursts have filled the gateway's buffer, and
keeps losing through the point (mean 13.3 percent of arrivals, epoch max 35); cgpb sheds a mean 37
percent of arrivals as LP (epoch max 61) and no checkpoints. Script `plot_burst_mechanism.py`
(`--v`, `--bridge`, `--bridge-label`).

## lp-loss-excluding-worst-agent.{pdf,png,svg}
Non-checkpoint (LP) spans refused by the bridges' agents over the bursty point, from each agent's
priority-processor counters (exact). Per configuration: bar = aggregate over the seven agents other
than the worst; dots = each of those seven; open red marker = the excluded worst agent, which is the
composepost agent (node-1) in all six configurations. Right-hand column: aggregate excluding the
worst (all eight agents in parentheses). Vanilla has no priority classes, so its row (gray, untagged;
the caption says so) is ALL spans refused, from each agent's receiver counters: 3.5 % excluding its worst
agent (composepost, 32.9 %), 13.7 % over all eight; others node-2 4, node-4 10, rest 0.

| config | all 8 agents | excl. worst | worst (composepost) | others (nodes 2..8) |
|---|---:|---:|---:|---|
| PB on    | 56.5 | 39.8 | 87.3 | 51 34 60 10 22 44 9 |
| PB off   | 64.3 | 55.1 | 71.1 | 54 0 62 0 0 41 0 |
| CGPB on  | 57.4 | 40.2 | 89.0 | 51 40 62 3 26 42 0 |
| CGPB off | 64.2 | 52.9 | 72.5 | 58 0 54 0 0 42 0 |
| SB on    | 65.4 | 50.5 | 93.0 | 61 41 72 22 31 65 15 |
| SB off   | 72.6 | 63.9 | 79.0 | 67 0 67 0 0 51 0 |

In the OFF arm the leaf-only agents (nodes 3, 5, 6, 8) carry no LP at all (leaves are always
checkpoints without the response path), so their 0 percent is "nothing to shed", not "nothing
lost" -- which is exactly why OFF loses checkpoints there instead. Script `plot_lp_excl_worst.py`.

## nw-ramp-n5 (.pdf/.png/.svg + .summary.json)
No-work ramp, n=5, RESPONSE-PATH-ON rounds only (`retctx-nwe2e-m5-on-r{1..5}-*`): None, Vanilla,
PB, CGPB, SB in one figure, bridges labelled plainly (the paper's default configuration; on-vs-off is reserved for the ablation
figure `FIGURES-2026-09-20/optsdk-bridges-n5`). Mean and p99 response time vs achieved throughput,
band = min-max across the five rounds, 500..14000 offered step 500, 30 s per point, admission
collectors (50/70), Jaeger + Elasticsearch store. Capacities (median achieved at the wall): None
11,961; Vanilla 8,973; PB 7,404; CGPB 7,373; SB 6,943 req/s. Script
`scratchpad/regen_matrix_plots_on.py` (wraps utils/plot_dsb_sn_latency_throughput.py).
`nw-ramp-n5-errorbars` is the same figure with the min-max spread across the five rounds drawn as
error bars (caps) at each point instead of the shaded band (`plot_dsb_sn_latency_throughput.py
--spread errorbars`, new option; default remains the band).

## Layout for the 2x2 bursty-load figure (2026-09-23, revised)
Each panel is drawn at its final cell size, 2.2 in wide, with 8 pt text everywhere (style
`scratchpad/burst_fig_style.py`, fonts embedded as Type 42):
- top-left `census-outcome` 2.2 x 1.4 in; top-right `lp-loss-excluding-worst-agent` 2.2 x 1.4 in
  (compressed from 1.7 in, 2026-09-23).
  Identical axes box and row order, so the rows line up across the two cells (Vanilla's LP-panel
  row is its overall refusal, untagged; the caption says so). Right-hand column: broken share (left panel), LP
  refused excluding the worst agent with all agents in parentheses, in percent (right panel). The worst agent is composepost's in all six configurations.
- bottom-left `burst-mechanism` (CGPB rev) or `burst-mechanism-sb` (SB, the worst case: 222,806 of
  31.0M checkpoints refused, 8 epochs; drawn as red caps on its LP bars) 2.2 x 1.5 in (compressed from 1.85 in; legend now one row above the panels, like the top row; offered-rate ticks 4k/6k), two rows: offered rate per 10 s epoch (dashed
  line = vanilla's 5,000 req/s fixed-rate boundary); refused share per epoch with vanilla (gray,
  all spans) drawn over CGPB on (light blue, LP; its checkpoints refused: 0 of 19.7M, stacked in
  red if nonzero). Bottom-right is the table.
- Caption material that no longer fits on the panels: "broken" = a checkpoint lost (vanilla: any
  span lost); dashed line meaning; "7 agents" = the aggregate over the agents other than the worst (bar), "each" = those agents individually (dots), "worst" = the excluded agent, composepost in every configuration (open circle). All three panels' legends are a single row directly above the axes; the top row's bar padding was trimmed so the legend sits on the first bar (2026-09-23 fixes).
Arm names (user 2026-09-23): response path on = "<bridge> rev", forward-only = the plain bridge
name; one map (`LABEL` in burst_fig_style.py). Both mechanism variants use a fixed 0-100 % refusal
axis so they are directly comparable.
