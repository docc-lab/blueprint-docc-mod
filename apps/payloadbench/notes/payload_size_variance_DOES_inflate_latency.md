# Highly-variable payload sizes DO inflate latency — and by more than M/G/1 predicts

**Date:** 2026-07-30
**Hypothesis (advisor):** highly-variable payload sizes inflate response-time variance.
**Verdict:** SUPPORTED, quantified, and the effect exceeds classical queueing prediction.
**App:** `apps/payloadbench` nt_es (untraced), edge node-2 / internal node-3, 8-core
limits + GOMAXPROCS=8, natural GC, single replica, wrk2 `-t16 -c128`.
**Data:** `results/variance_20260730_080805/` (+ `analysis.txt`), n=3 rounds.
**Tooling:** `variance_experiment.sh`, `analyze_variance.py`, bimodal sampler in
`workload/payload.lua`.

## Why earlier attempts found nothing (and it wasn't a null result)

The hypothesis is a queueing claim. For M/G/1,
`E[W] = rho/(1-rho) * E[S](1 + CV_S^2)/2` — waiting depends on the VARIANCE of service
time, amplified as rho→1. The causal chain is

> payload size variance → **service time variance** → response time variance

The first campaigns (fixed 1024 B vs U[800,1200] vs Exp(1000), n=5, 2k→62k) manipulated
link 1 and measured link 3, but **never moved link 2**: at 1 KB, ±20% size spread yields
CV_size = 0.115, and payload-dependent work is a minority of per-request cost, so
CV_S stayed ≈ 0.05 against a response-time CV of ~0.68 dominated by scheduling/GC jitter.
Those nulls were a **power failure, not evidence against the hypothesis.**

## What made it testable: heavy tails at a REALISTIC mean

The fix is not bigger mean payloads (1 MB is not RPC traffic — it is blob transfer that
belongs in object storage). It is **high CV at a small mean**, which is also the realistic
shape: many small calls, rare large aggregate response.

| arm | distribution | mean | CV_size |
|---|---|---|---|
| fixed | constant 4250 B | 4250 B | 0 |
| exp | exponential, mean 4250 B | 4250 B | 1.0 |
| **bimodal** | **95% × 1 KiB + 5% × 64 KiB** | **4250 B** | **3.31** |

4.25 KB mean is ordinary RPC traffic. The 5% tail costs ~3× the service time of the
small requests, so CV_S actually moves.

### Calibration: CV_S measured, not assumed (rho ~ 0.1, queueing-free)

| arm | CV_size | E[S] | CV_S |
|---|---|---|---|
| fixed | 0 | 0.656 ms | 0.267 |
| exp | 1.0 | 0.646 ms | 0.317 |
| bimodal | 3.31 | 0.681 ms | 0.432 |

Means matched within 5% ⇒ the arms differ only in variance. CV_S rises monotonically, so
**link 2 moved this time.** Note the transmission is heavily attenuated: CV_size 3.31 →
CV_S 0.432 (payload-induced component ≈ 0.34 after removing the 0.267 baseline jitter in
quadrature), i.e. only ~10% of size variance reaches service time on a real stack.

## Result (n=3, ± = between-round sd)

| rho | fixed mean | exp mean | bimodal mean | bimodal penalty | ΔCV vs fixed | E[W] ratio | vs P-K |
|---|---|---|---|---|---|---|---|
| 0.50 | 0.752±0.007 | 0.764±0.007 | 0.843±0.006 | **+12.1%** | +62% | 1.69× | 1.53× |
| 0.70 | 0.909±0.001 | 0.948±0.004 | 1.125±0.002 | **+23.8%** | +69% | 1.76× | 1.59× |
| 0.85 | 1.039±0.010 | 1.126±0.020 | 1.458±0.004 | **+40.3%** | +62% | 2.03× | 1.83× |
| 0.95 | 1.205±0.025 | 1.375±0.039 | 1.886±0.025 | **+56.5%** | +13% | **2.20×** | 1.98× |

1. **The penalty grows with utilisation** (+12 → +24 → +40 → +57%) — the ρ/(1−ρ)
   signature that makes this a genuine queueing effect, not a fixed per-request cost gap.
2. **Response-time CV is 62–69% higher** through rho=0.85 at matched mean and matched rho.
3. **Dose-response**: exp lies between fixed and bimodal at EVERY rho, so the effect
   tracks the amount of variance, not just its presence.
4. **Error bars are 10–100× smaller than the effect** (±0.002–0.039 ms).
5. **Capacity also drops**: bimodal ceiling 25 781 rps vs fixed 28 298 (−8.9%) at
   identical mean payload — variance costs throughput as well as latency.

## Two findings beyond the textbook result

**(a) The effect is 1.5–2× LARGER than M/G/1 predicts**, and the excess grows with load
(1.53× at rho=0.5 → 1.98× at rho=0.95). P-K from the measured CV_S predicts only a 1.11×
inflation in E[W]; we measure 1.69–2.20×. Leading explanation: **head-of-line blocking on
the shared HTTP/2 connection** — a 64 KiB message occupies the single per-connection
transport writer while small requests queue behind it (see
`payload_size_variance_no_effect.md` on the one-`loopyWriter`-per-connection structure).
M/G/c has no term for this. UNTESTED prediction: raising `GRPC_CLIENT_CONNS` should
*reduce* the excess over P-K by giving small requests alternative write paths.

**(b) CV ratio is the WRONG metric to headline.** ΔCV collapses to +13% at rho=0.95 while
the mean penalty peaks at +56.5%, because at high rho queueing generates variance for both
arms (fixed's own CV jumps 0.283 → 0.450). Payload variance becomes a smaller *share* of
total variance while its absolute contribution keeps growing. **Report absolute SD and
mean/tail latency penalty**; CV ratio understates the effect exactly at the loads that
matter.

## Methodology that mattered

- **Match on rho, not rps.** Each arm is offered a fraction of ITS OWN measured capacity.
  At matched rps, 24 000 would be rho=0.85 for fixed but rho=0.93 for bimodal, and the
  latency gap would be mostly "driven closer to its own limit". This exact confound
  earlier faked a −35% "improvement" from a 2.4% mean-size difference.
- **Match mean payload.** Capacity depends strongly on size (1 KB → 56k rps; 4.25 KB →
  28.3k rps), so unmatched means are not comparable at any fixed rps.
- **Measure CV_S; do not assume it.** Without the rho≈0.1 calibration you cannot
  distinguish "no effect" from "no manipulation" — which is precisely what went wrong for
  the first four campaigns.
- **`-c 128`, not `-c 1024`** — 1024 sits ~14% below peak throughput with 9× inflated
  latency from self-inflicted client queueing.
- Report **knee = peak of the round-averaged curve**, never mean-of-per-round-maxima.

## Caveats / next

- Capacity per arm was measured once (n=1) and drifts ~5–7% between deploys, so
  "rho=0.95" is really 0.95±0.05. Fine for the trend; tighten with repeated capacity
  measurement before publishing exact coefficients.
- Effect sizes are specific to this stack's fixed overhead (~71 µs + ~25 ns/byte). A
  leaner service would show payload variance mattering at *smaller* sizes; report the
  crossover so the result generalises.
- Matched-rps ramps (4k→32k, 3 arms, n=3) in progress → knee comparison and the
  *operational* penalty, which compounds capacity loss with queueing amplification.
