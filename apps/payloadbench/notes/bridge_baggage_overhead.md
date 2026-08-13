# Tracing-bridge baggage on the call path costs nothing measurable

**Date:** 2026-08-12 · **Result:** null across all three bridges, at every RPC size tested.
**Data:** `results/bridges_20260812_172905` (pb), `results/bridges_20260812_213521` (cgpb, sb).

## Question

Each tracing bridge adds metadata to every RPC. Does that cost response time,
tail latency, latency *variance*, or capacity?

The variance question is the one that matters: this project previously showed
that payload-size **variance** inflates latency variance independently of the
mean (`payload_size_variance_DOES_inflate_latency.md`). s-bridge has a genuinely
heavy-tailed size distribution, so it is the natural test of whether that result
applies to bridge baggage.

## Design

Baggage goes on the **request only** (the call path); the response keeps the
unmodified base size. Base sizes are the SOSP'23 Fig 6 points already
characterised in `results/baselines_*`.

| bridge | distribution (cpd 6) | mean | CV_size |
|---|---|---|---|
| `none` | — (control) | 0 B | — |
| `pb`   | constant (simulated traces) | 24 B | 0 |
| `cgpb` | two-point: 24 B @ 89.30%, 33 B @ 10.70% (Uber traces) | 24.96 B | 0.11 |
| `sb`   | 27-bucket histogram (Uber traces) | 32.95 B | **2.86** |

`cgpb`/`sb` are implemented as real samplers in `workload/payload.lua`, **not**
collapsed to their means — collapsing `sb` would discard the very property under
test. `<P>_BASE` adds the application's base payload on top, so each distribution
stays stated in its own units. Validated at n=400k draws: `cgpb` mean 24.964 vs
24.963 published; `sb` p99.9 617 B vs 619 B published.

Both campaigns: 2 rounds, 2k→saturation in 2k steps, 25 s/step, `-t16 -c128`,
8-core limits, GOMAXPROCS=8, natural GC, single replica, 90 s drain between arms.

## Results (pre-knee, paired within round, n=2)

| cell | Δmean | Δsd | Δp99 |
|---|---|---|---|
| p10/pb   | +0.15% | — | — |
| p10/cgpb | +0.34% | −0.12% | +0.79% |
| p10/sb   | +0.51% | +3.44% | +1.64% |
| p50/pb   | +0.29% | — | — |
| p50/cgpb | −0.29% | −1.16% | −0.41% |
| p50/sb   | +0.08% | +0.13% | +0.30% |
| p90/pb   | −3.91% | — | — |
| p90/cgpb | +0.54% | −0.08% | +0.22% |
| p90/sb   | −1.80% | −12.50% | −4.45% |

Every pre-knee error bar spans zero. Capacity moved within ±2% with incoherent
signs (p10 negative, p90 positive) — no capacity effect.

## Why the null is the expected answer

Per-byte cost on this rig is **~24 ns/B**, from the p10-vs-p90 baseline gap at
6k rps (0.499 ms @ 392 B vs 1.024 ms @ 22000 B → 0.525 ms / 21608 B).

So 24–33 B predicts **~0.6–0.8 µs/request** ≈ 0.1–0.2% of a 0.5 ms response —
an order of magnitude below the ±1–3% step-to-step noise floor. The honest output
is an **upper bound**, not a point estimate:

> Bridge baggage at cpd 6 costs **< ~1.5% of response time and < ~2% of capacity**
> at every RPC size tested.

## Why sb's heavy tail does NOT inflate variance (boundary of the earlier result)

`sb` has CV_size 2.86 and a p99.99 near 3 KB, yet is indistinguishable from the
near-constant `cgpb` and from `pb`. This does not contradict the earlier bimodal
finding — it locates its boundary.

The bimodal arm moved latency because its tail requests cost **~3× the service
time** of the small ones. `sb`'s p99.99 tail adds ~3 KB, which at 24 ns/B is
**~72 µs on a ~400 µs request** (~1.2×), and it lands on **1 request in 10,000**.
Too small and too rare to move a distribution whose sd is already ~0.2 ms.

The causal chain needs all three links. Size variance only matters when it
becomes **service-time** variance:

    size variance -> SERVICE TIME variance -> response time variance

`sb` moves the first link a lot and the second link almost not at all.

## Two cells NOT to report as findings

* **p10/sb Δsd = +3.44%** — the largest credible number, but it **flipped with arm
  ordering** (r1 −0.09%, r2 +1.11% on the mean), so it is position, not payload.
* **p90/sb Δsd = −12.50%** (and p90/pb −3.91%) — p90 saturates at ~13k, leaving
  only 4–5 pre-knee steps at 2k granularity. That arm cannot resolve anything, and
  heavy-tailed bytes *reducing* variance is not physical.

## Methodology notes (both cost real time to learn)

**Arm order must be counterbalanced.** Rounds 1–2 originally ran control-first
every time, confounding arm position with time: within-round drift landed entirely
on the bridge arm and produced the bridge reading **7.9% FASTER on 10/11 steps** —
impossible for added bytes. `ramp_bridges.sh` now alternates order by round parity
(odd = control-first, even = bridge-first) so drift cancels in the pooled estimate.
The order-flip check is itself the diagnostic: a real effect keeps its sign.

**A physically impossible sign is a bug signal, not noise.** Every artifact in this
campaign announced itself as the bridge arm being *faster*. Treat that as a design
fault to find, not a number to average away.

## Reproduce

```bash
./deploy.sh                                    # includes NodePort 11011 (stage 5a)
./ramp_bridges.sh                              # default: none cgpb sb, 2 rounds
BRIDGES="none pb" ./ramp_bridges.sh            # the pb campaign
python3 analyze_bridges.py results/bridges_<ts>
```

Other checkpoint distances are a config change: edit the named dist in
`workload/payload.lua` and add it to `bridge_req_env` / `bridge_mean`.
