# FIGURES-2026-09-17 (Tomislav-RetCtx)

Every figure here is drawn at final on-page size: 3.33 in wide (one column of a two-column CS
conference paper), 8 pt axis labels, 7 pt ticks and legend. SVG text is outlines, so no viewer
can substitute a font. Nothing is hand-edited; the scripts below regenerate all of it.

## No-work END-TO-END, admission-control collectors, n=3
- nwe2e-latency-vs-throughput.*       mean and p99 vs ACHIEVED throughput. Each curve ends at
                                      that variant's capacity; band = min..max over 3 runs;
                                      post-saturation branch drawn faint.
- nwe2e-latency-vs-throughput-runs.*  same, with the 3 individual runs inside the band
  root: retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
  capacity (req/s): None 12,075 | Vanilla 9,390 | PB 6,629 | CGPB 6,557 | SB 6,103
  script: utils/plot_dsb_sn_latency_throughput.py

## Real-work END-TO-END, n=3, offered 2000..5000 step 200
  roots: retctx-e2e-cpd2-6-inverse-20260915T041603Z (Vanilla, PB, CGPB)
       + retctx-e2e-sb2-cpd2-6-inverse-20260915T183039Z (rebuilt SB)
  The primary root still holds the SUPERSEDED S-Bridge, so it is dropped with
  `--exclude sb` and the rebuilt one added with `--extra`. Never plot that root alone.
- response-time-e2e.*              mean and p99 vs OFFERED rate
  The latency-vs-achieved-throughput view is deliberately NOT used for the real-work
  regime: all four capacities sit within 3 % (Vanilla 3,445 | CGPB 3,345 | SB 3,337 |
  PB 3,336), so every curve's wall lands on the same vertical and the achieved-throughput
  axis only crowds the points near saturation and folds back on itself. That view is kept
  for the no-work regime, where capacities span a factor of two.
- drop-rates-e2e.*                 total / worst-collector checkpoint / non-checkpoint drops
- drop-rates-all-e2e.*             the same three metrics over all 8 collectors
- drop-rates-except-worst-e2e.*    over the 7 collectors left after removing the worst
- drop-rates-worst-e2e.*           over the worst collector alone
  Drop panels stack vertically below 4.5 in width. One formula throughout:
  spans refused / spans handled, over whichever collectors the variant selects.
  script: utils/plot_dsb_sn_e2e_drops.py

## Collector-only span throughput
- collector-throughput.*  offered vs exported spans/s, 1 CPU collector, 4 variants, n=3
  root: /users/tomislav/deployments/collector-load/spanload-dense-ramps-20260914T185629Z
  panels: no semconv (100k steps to 1.6M) and 10 semconv (10k steps to 200k)
  plateau (k spans/s): no semconv Vanilla 982 / PB 543 / CGPB 540 / SB 534
                       10 semconv Vanilla 109 / PB 102 / CGPB 102 / SB 101
  script: utils/plot_spanload_throughput.py

Dropped from this set: the old `end-to-end.*` three-panel figure (mean, p99, completed vs
offered). The latency-vs-throughput figures show the same data with capacity readable off
the x-axis.
