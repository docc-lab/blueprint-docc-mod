# Bursty span-loss figures, SN no-work, current stack (2026-09-25)

Campaign `snburst` (Tomislav-RetCtx). Stack = the SN no-work n=5 round: admissionotel500m agents, ClickHouse gateway 2 CPU,
queue3 collector, GOMAXPROCS auto, backlog margin, gateway LP resource_exhausted, agent compression none, GOGC=off
GOMEMLIMIT=1GiB, FINAL SDK images, bridges without reverse_passthrough (priority receiver, SDK HP retry, priority queues at
agents and gateway, depth_cubic). Refused-trace census on (`RETCTX_REFUSED_CENSUS=on`, `RETCTX_REFUSED_RECORDS=on`, runner
`RETCTX_REFUSED_BIN=on`); 5000 uniformly sampled traces per point.

- Boundary: vanilla sustained 300 s points `retctx-snburst-sustain-v-20260925T032116Z`: clean at 6500 / 7000 / 7500,
  first loss at 8000 (10.9 % of spans) -> mean 7500.
- Bursty: 600 s, wrk2 fork `-D pareto` alpha 1.5 cap 2.0 epoch 10 s seed 1001; realised 60 epochs 5547..10399 req/s,
  mean 7395, 21 epochs above 8000. Identical schedule for every case. Roots: vanilla
  `retctx-snburst-burst-v-20260925T035938Z`, rev (response path) `retctx-snburst-burst-rev-20260925T041441Z`,
  forward-only `retctx-snburst-burst-fwd-*`. (`retctx-snburst-burst-v-20260925T034431Z-superseded-norecords` lacks
  per-trace records.)

## census-outcome
Per configuration, share of traces (user 2026-09-25): RED = broken = lost any span (vanilla) / lost a checkpoint (bridges),
exact from the census union across the 13 services; BLUE = bridges' traces that lost only LP spans, from the 5000
uniformly sampled stored traces (share missing >= 1 of their 23 spans, minus red); GRAY = intact (no span lost). The blue
share cannot come from the census on this stack: the SDK census sees only spans refused back to the SDK, while most LP loss
is evicted inside the agent/gateway priority queues. HP is never evicted and HP send failures are 0, so red is exact.
Sample vs census on vanilla: 28.0 vs 28.17 % (cap 2.0), 25.3 vs 26.82 % (cap 2.25; the store-side sample cannot see a
vanilla trace whose root span was dropped). Right column = broken share. `cap2.25/` holds the same figures for the
cap-2.25 campaign (`retctx-snburst3-*`, realised 5309..11003 req/s, whole pod logs) plus `burst-mechanism-sb`.

## lp-loss-excluding-worst-agent
LP spans lost per agent node = SDK lp_dropped (services on that node) + agent priority-queue lp_send_failed +
lp_evicted_spans, over agent lp_enqueued + SDK lp_dropped. Bar = aggregate over the 7 agents other than the worst, dots =
each, open red marker = the worst; right column "excl. worst (all 8)". Vanilla row = ALL spans lost per node (SDK
spans_dropped / spans_received). Gateway LP evictions are not attributable to an agent (PB 4,361; SB rev 26,114; others 0).

## burst-mechanism (not reproduced)
The per-epoch timeline of FIGURES-2026-09-23 needs per-second counters over the whole point. On this stack the vanilla
agents log none (no memory_limiter transitions), and the bridge agents' 2000-line log tail covers only the last ~210 s.

Script: `scratchpad/snburst_figs.py --v ROOT --rev ROOT --fwd ROOT --out-dir DIR` (numbers in `snburst-summary.json`).
