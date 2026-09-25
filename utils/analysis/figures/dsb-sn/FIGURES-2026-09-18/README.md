# FIGURES-2026-09-18 (Tomislav-RetCtx)

Same drawing conventions as FIGURES-2026-09-17: final on-page size, 3.33 in wide (one column
of a two-column CS conference paper), 8 pt axis labels, 7 pt ticks and legend, sans-serif,
SVG text as outlines. Nothing hand-edited.

## No-work END-TO-END, response path OFF, n=3
- nwe2e-norev-latency-vs-throughput.*  mean and p99 vs ACHIEVED throughput.
  bridges root: retctx-nwe2e-norev-20260918T154407Z  (REVERSE_TRUSS=off: every childless
  server span is a checkpoint; CPD still uniform 2..6; admission-control collectors
  1 CPU/256Mi, GOMEMLIMIT 230MiB; tuned Jaeger/ES store)
  baselines: None and Vanilla are taken from the reverse-ON root
  retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z via --extra. That is a valid splice, not
  an approximation: no-tracing carries no SDK at all, and vanilla was already deployed there
  with REVERSE_TRUSS=off / RT_LEAF_REJECT=0 (the runner forces reverse off for every
  non-bridge kind), with the same collector profile, ramp grid and store. Neither baseline
  has any response path to turn off, so their curves are identical in both campaigns.
  n=3: the campaign completed 2026-09-18 15:55 EDT, all 252 points, and every figure here
  was regenerated from the full analysis (no --partial, no --repetition). Bands are min..max
  over the 3 runs.
  capacity (req/s), mean over 3 runs: None 12,075 | Vanilla 9,390 | PB 8,225 | CGPB 8,074
                                      | SB 7,644
  same kinds with the response path ON (n=3): PB 6,629 | CGPB 6,557 | SB 6,103
    -> +24.1 % / +23.1 % / +25.3 %
  command:
    utils/plot_dsb_sn_latency_throughput.py --out <norev root> --figures <this dir> \
      --repetition 1 --extra <reverse-ON root>:nt --extra <reverse-ON root>:v \
      --name nwe2e-norev-latency-vs-throughput
  points come from utils/analyze_dsb_sn_nw.py --out <root> (full run, 252/252 verified).

## No-work END-TO-END, response path OFF, span drop rates, n=3
- drop-rates-norev.*               total / worst-collector checkpoint / non-checkpoint
- drop-rates-all-norev.*           the same three metrics pooled over all 8 collectors
- drop-rates-except-worst-norev.*  over the 7 collectors left after removing the worst
- drop-rates-worst-norev.*         over the worst collector alone
- response-time-norev.*            mean and p99 vs OFFERED rate (the offered-rate view; the
                                   achieved-throughput view is nwe2e-norev-latency-vs-throughput)
  Three panels stacked vertically at column width. One formula throughout:
  spans refused / spans handled, over whichever collectors the variant selects.
  bridges root: retctx-nwe2e-norev-20260918T154407Z, all 3 repetitions (error bars are
  sample SDs over the 3 runs).
  vanilla is BORROWED from retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z with --borrow,
  for the same reason as in the latency figure: vanilla was already deployed there with
  REVERSE_TRUSS=off, so its ramps are identical in both campaigns. --repetition is applied
  after the borrow, so vanilla is cut to repetition 1 as well.
  READ THE MIDDLE PANEL CAREFULLY: vanilla has no checkpoint/non-checkpoint split (it runs a
  memory_limiter, not the priority processor), so the black line on EVERY panel is vanilla's
  TOTAL drop rate, repeated as the reference. It is not a vanilla checkpoint-loss rate.
  lifetime checkpoint loss over the whole ramp, n=3 pooled: PB 1.67 % | CGPB 1.95 % | SB 0.99 %
    per repetition: PB 2.56/1.15/1.30 | CGPB 0.81/2.36/2.68 | SB 0.14/0.88/1.94
    The per-run spread is larger than the gap between bridges, so the bridges are NOT
    separable on total checkpoint loss. Report it as a range, not an ordering.
  ONSET is the reproducible quantity -- first offered rate with any checkpoint loss:
    PB 7,500 / 7,500 / 7,500   (achieved 7,425 / 7,431 / 7,420)
    CGPB 7,000 / 7,500 / 7,000 (achieved 6,786 / 7,430 / 6,785)
    SB 8,000 / 8,000 / 8,000   (achieved 7,573 / 7,614 / 7,528, i.e. 98-100 % of its peak)
  command:
    utils/plot_dsb_sn_e2e_drops.py --out <norev root> --figures <this dir> \
      --borrow <reverse-ON root>:v --repetition 1 --xtick 2 --suffix=-norev

## Zoomed response-time views (pre-knee region)
Both use --rt-xcut, which DROPS response-time points above the cut rather than only clipping
the view as --xmax does. With --xmax the segment heading to the next, off-scale point is left
as a stub climbing the right edge and the last point sits on the spine with half its error bar
cut off; --rt-xcut ends the curve at the cut and gives the axis a small margin.

- response-time-e2e-zoom.*   REAL-WORK app, mean and p99 vs offered rate, cut at 3.4k, n=3.
  roots: retctx-e2e-cpd2-6-inverse-20260915T041603Z (Vanilla, PB, CGPB)
       + retctx-e2e-sb2-cpd2-6-inverse-20260915T183039Z (rebuilt SB, --exclude sb + --extra)
  The published response-time-e2e.* runs to 4k, where the post-knee explosion to ~5,000 ms
  flattens everything below it into the axis. Cut at 3.4k with 100 ms / 250 ms ceilings the
  ordering is legible: at offered 3.2k, Vanilla 38 ms mean / 83 ms p99, PB 54/144,
  CGPB 49/110, SB 75/240.
  command:
    utils/plot_dsb_sn_e2e_drops.py --out <primary root> --figures <this dir> \
      --exclude sb --extra SB=<sb2 root>:sb --rt-xcut 3.4 --rt-ylim 100,250 \
      --rt-height 1.35 --xtick 0.5 --suffix=-e2e-zoom

- response-time-norev.*       NO-WORK, response path OFF, full ramp to 14k, n=3
- response-time-norev-zoom.*  the same, cut at 7.5k with 250 ms / 400 ms ceilings
  bridges root: retctx-nwe2e-norev-20260918T154407Z (all 3 repetitions)
  No tracing and Vanilla borrowed from retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z.
  At offered 7.5k (the last pre-knee point): None 14 ms mean, Vanilla 39, PB 85, CGPB 86,
  SB 204. Past 8k the bridges run away (SB 992 ms, PB 176, CGPB 181), which is why the full
  figure needs the zoom next to it.
  command: as the drop-rate block above, plus --rt-xcut 7.5 --rt-ylim 250,400
           --rt-height 1.35 --xtick 1 --suffix=-norev-zoom
  (--repetition is no longer passed anywhere: the campaign is complete at n=3)
  (the -zoom run also emits drop-rate panels identical to the unzoomed ones; those duplicates
   are deleted, only response-time-*-zoom.* is kept)

## No-work END-TO-END, response path ON (collector-constrained), n=3
The reverse-ON counterpart of the -norev set above, from the one no-work campaign that has
three repetitions: retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z. Same collector constraint
as every figure here (admission control, 1 CPU / 256Mi, GOMEMLIMIT 230MiB) and the same tuned
store. All five kinds are in this root, so nothing is borrowed and the error bars are real
sample SDs over 3 runs.
- response-time-nwe2e-n3.*       mean and p99 vs offered rate, full ramp to 14k
- response-time-nwe2e-n3-zoom.*  the same, cut at 6.5k with 250 ms / 450 ms ceilings
  At offered 6.0k (the last point before the wall): None 10 ms mean / 26 ms p99,
  Vanilla 19/53, PB 83/162, CGPB 86/167, SB 220/427. By 7.0k the bridges are at 783 / 1,442 /
  2,425 ms mean, which is why the full ramp needs the zoom beside it.
- drop-rates-nwe2e-n3.*, -all-, -except-worst-, -worst-  the drop panels for the same root
  THE RESULT TO READ: the checkpoint panel is flat zero for PB, CGPB and SB across the entire
  ramp, at every collector set including the worst single collector. With the response path
  on, the priority processor never has to refuse a checkpoint -- it sheds only expendable
  spans (55-60 % of them past 3k). Compare drop-rates-all-norev.*, where turning the response
  path off pushes pooled checkpoint loss to 15.6 % PB / 7.4 % CGPB / 1.8 % SB.
  (As everywhere in this script, the black line on all three panels is Vanilla's TOTAL drop
   rate, repeated as the reference; Vanilla runs a memory_limiter and has no class split.)
  command:
    utils/plot_dsb_sn_e2e_drops.py --out <n3 root> --figures <this dir> --xtick 2 \
      --suffix=-nwe2e-n3
    utils/plot_dsb_sn_e2e_drops.py --out <n3 root> --figures <this dir> --xtick 1 \
      --rt-xcut 6.5 --rt-ylim 250,450 --rt-height 1.35 --suffix=-nwe2e-n3-zoom
