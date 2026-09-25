# FIGURES-2026-09-16 (Tomislav-RetCtx)

Latest figure set. SB everywhere is the rebuilt CG-core + structural-truss implementation (Lehmer on);
the old SB is not in any figure here.

No-work (paper section 5.4 regime), 100 % sampling, n=3, CPD random 2..6, inverse_depth, leaf rejection:
- nw-100pct-n3.{pdf,svg,png} + nw-100pct-n3.provenance.json
  mean, p99, completed throughput vs offered rate; error bars = sample SD over 3 repetitions
  root: /users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z (RESULTS-n3.md)
  peaks (req/s, mean +- SD): nt 12060+-88, v 9805+-57, PB 7831+-96, CGPB 7870+-92, SB 7375+-72

Real-work end-to-end, n=3, 2000..5000 step 200:
- end-to-end.*    mean, p99, completed throughput (v, PB, CGPB, SB)
- response-time.* mean and p99 ramps
- drop-rates.*    total drop vs vanilla, worst-case checkpoint drop vs vanilla, non-checkpoint drop vs vanilla
  roots: /users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z (v, PB, CGPB; RESULTS-n3.md)
         /users/tomislav/deployments/dsb-sn/retctx-e2e-sb2-cpd2-6-inverse-20260915T183039Z (SB; RESULTS-sb2.md)

Copied from FIGURES-2026-09-15 on 2026-09-16; file contents unchanged.

No-work END-TO-END (admission-control collectors), n=3, 100 % sampling:
- nwe2e-response-time.*  mean and p99 ramps (no-tracing, vanilla, PB, CGPB, SB)
- nwe2e-drop-rates.*     v/PB/CGPB/SB only (no-tracing has no SDK, so no drop rate exists): total drop vs vanilla, worst-case checkpoint drop vs vanilla, non-checkpoint drop vs vanilla
- nwe2e-end-to-end.*     mean, p99, completed throughput (all five kinds)
- nwe2e-drop-rates-all.*          same three metrics over all 8 collectors
- nwe2e-drop-rates-except-worst.* same three metrics over the 7 collectors left after dropping the worst
- nwe2e-drop-rates-worst.*        same three metrics over the worst collector alone
  All four drop figures use ONE formula: spans refused / spans handled, summed over the collectors the
  variant selects. They differ only in which collectors are counted. There is no unweighted average.
- nwe2e-figures.provenance.json
  root: /users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z (RESULTS-nwe2e.md)
  peaks (req/s, mean +- SD): nt 12075+-78, v 9390+-95, PB 6629+-111, CGPB 6557+-89, SB 6103+-41
  checkpoint spans refused across all 420 points: 0
  DIFFERENT REGIME from nw-100pct-n3 above (passthrough collectors, nothing sheds on purpose). Do not overlay.
