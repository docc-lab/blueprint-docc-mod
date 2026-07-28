# nt zero-work ceiling sits *below* 10%-sampled tracing — investigation

**Date:** 2026-07-22 (updated 2026-07-24 with re-run result)
**Status:** RESOLVED-ish — H1 & H2 both rejected; residual ~4% attributed to
uncontrolled deploy-level variance (not tracing, not build, not time-of-day).
**Regime:** no-work (zero-work DSB-SN, `docker_*_es_nw`), passthrough collectors
@ 1 core / 4 GiB, n=5 ramp 500→14000 by 500.

## Observation

In the no-work n=5 campaign, several 10%-sampled traced variants show a *higher*
achieved-throughput ceiling than no-tracing (nt), which is physically impossible
if the only difference is tracing (10% sampling still creates a span + makes the
sampling decision on 100% of requests → it does strictly ≥ the work of nt; it
cannot raise the ceiling).

Knee (peak of round-averaged achieved curve, mean ± 95% CI, n=5):

| variant           | knee (rps)   |
|-------------------|--------------|
| no-tracing        | 12037 ± 107  |
| vanilla s10       | **12589 ± 73**   |
| SB cpd2 s10       | 12247 ± 171  |
| PB cpd6 s10       | 12004 ± 259  |
| CGPB cpd6 s10     | 11554 ± 271  |

nt lands *in the middle* of the 10% band, and vanilla-s10 clears it by ~550 rps.

## Characterization

1. **Below ~11.5k offered, nt and every 10% variant are pixel-identical** — they
   ride the offered==achieved diagonal together (e.g. at offered 11000 all are
   10729–10735). The tracing cost at 10% is genuinely invisible below saturation.

2. **The entire spread lives in the last 3–4 offered steps (≥12k)** — the
   saturation tail, where achieved decouples from offered:

   | offered | nt          | vanilla s10 | SB cpd2 s10 |
   |---------|-------------|-------------|-------------|
   | 12500   | 11959 ± 105 | 12219 ± 11  | 12163 ± 79  |
   | 13000   | 11967 ± 54  | 12431 ± 158 | 12247 ± 171 |
   | 13500   | 11974 ± 80  | 12589 ± 73  | 12245 ± 98  |

3. **The CI bands do NOT overlap at the ceiling** (e.g. offered 13500: nt
   [11894–12054] vs vanilla-s10 [12516–12662]). So it is **systematic and
   reproducible across all 5 rounds of each config**, NOT within-round noise.
   (This corrected an earlier wrong claim of "it's just measurement noise" — the
   error bars the analysis added disprove that.)

### Metric note (separate, already fixed)

The knee was originally `mean over rounds of (max over steps)` — averaging a
per-round **max** is an upward-biased extreme-value statistic; n=5 averaging
tightens the estimate around a biased target rather than removing the bias.
Fixed to **average across rounds at each step first, then take the peak of the
averaged curve** (`agg_nw_n5.py::knee`). This shaved ~70–140 rps off every knee
but did NOT close the nt-vs-s10 gap → the gap is not the max artifact.

## Hypotheses

- **H1 (cross-build confound):** nt is a separate build (`docker_nt_es_nw`) from
  the traced `_es_nw` builds; if compiled from a different base revision it could
  hit its own lower ceiling for non-tracing reasons (cf. the documented
  esrev0-vs-esrev2 baseline confound). **REJECTED** by the build check below.
- **H2 (temporal/environmental drift):** nt ran *first* in the campaign (Jul 21
  morning), ~14–18 h before the late 10% tiers. Was LEADING. **REJECTED** by the
  re-run below — nt-now (fresh deploy, ~18 h later) reproduces ~12.0k exactly.
- **H3 (uncontrolled deploy-level variance):** each config is deployed ONCE and
  run n rounds back-to-back, so within-config CIs share one pod placement / node
  assignment / warm state and are tight by construction — they do **not** capture
  deploy-to-deploy variance (which nodes host the gateway, neighbour noise, etc.).
  The ~4% nt-vs-s10 gap is most consistent with this: not caused by the variable
  under test (tracing can't speed things up), not the build, not time-of-day →
  an uncontrolled harness variable. **LEADING (by elimination).**

## Build check (rules out H1)

Compared `build_nt_es_nw` vs `build_v_es_nw`:
- Same Go 1.26.5, same base images (`distroless/base-debian12`), matching
  `go.mod` deps, same 15 services, same wiring/source tree.
- **Only** structural difference: the vanilla build carries the `ot/` wrappers
  (`*_OT{Client,Server}WrapperInterface.go`, `Wrk2APIService_OTServerWrapper…`)
  — the intended tracing-on-vs-off delta, and the entire line-count gap (nt 26.3k
  vs v 28.3k .go lines). Remaining diffs are cosmetic gofmt whitespace in
  `bloom.go` + a test file, off the app hot path.
- nt is therefore *leaner* than the traced builds → a build difference would make
  nt **faster**, not slower. H1 rejected.

## Falsifying experiment (in flight)

Re-run nt-nw NOW (post-campaign), near-ceiling ramp (offered 10k–14k), n=3
(`nt_recheck.sh` → `ramp_nt_recheck_r*`):
- If nt-now ≈ 12.5k → temporal drift confirmed (H2); nt's original ~12.0k was a
  "slow cluster moment."
- If nt-now ≈ 12.0k again → something reproducible about the nt config; reopen.

**RESULT (2026-07-24):** nt-now = **12067 ± 167 (n=3)**, statistically identical
to nt-original **12037 ± 107 (n=5)**. It did NOT rise toward the ~12.5k of the
late 10% variants. → **H2 (temporal drift) REJECTED.** nt reproducibly caps at
~12.0k across two independent deploys ~18 h apart.

With H1 (build) and H2 (time) both eliminated, and tracing physically unable to
*raise* the ceiling, the residual ~4% is an **uncontrolled deploy-level variable**
(H3) — pod placement on the static 9-node layout, gateway↔co-located-otelcol-
DaemonSet CPU contention, wrk2 client-side, etc. — that within-deploy n=5 cannot
see. No mechanism was found by which no-tracing is genuinely slower than sampled
tracing; none should exist.

To *characterize* H3 would require deploy-level replication (redeploy each config
k times, look for ceiling swaps) — more cluster time than a ~4% effect in the
sampling-recovered regime warrants. Not pursued.

## Takeaway for the paper (regardless of H2 outcome)

- The **100% story is unaffected** — bridges ~8.1–8.7k < vanilla 9.6k < nt 12.0k,
  gaps of thousands of rps, tracing-dominated; ~500 rps cross-time drift is noise
  at that scale.
- **Do not use nt as the absolute ceiling for the 10% comparison.** State the
  10% result as "at 10% sampling all traced variants recover to within a few % of
  the untraced ceiling," treating the residual ordering as cross-time drift, not
  signal. Any strict "sampling recovers throughput" claim should ideally come from
  interleaved (same-time) baseline+treatment runs, not configs hours apart.
