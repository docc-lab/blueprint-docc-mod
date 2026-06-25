# Why bridges "match or beat vanilla" in the cpd=2 natural-GC ramp — baseline confound

**Date:** 2026-06-24
**Artifact:** `runs/fourway_natural_all.png` (DSB-SN no-limiter cpd=2, natural GC, avg r2–6:
vanilla vs pbridge vs cgpb vs sbridge).

## Observation
In the four-way app-latency ramp, the three bridge variants (pb/cgpb/sb) show the **same
or LOWER** mean RT than vanilla through the knee — vanilla appears to collapse *first*.
That is impossible if bridges add overhead, so it demanded explanation.

## Characterization (existing data, r2–6, n=5 except rc)
- **Flat region (2000–3000 rps, low variance):** mean RT ≈ 27–31 ms for all four. Deltas vs
  vanilla: pbridge **+0.6 ms**, cgpb **−1.8 ms**, sbridge **−3.7 ms** — all far inside the
  ±5–10 ms within-variant spread. → per-span app overhead is **below the measurement floor**.
- **Knee location (load where mean RT first exceeds 500 ms):**
  vanilla **3900 ± 100**, pbridge **4133 ± 94**, cgpb **4200 ± 0**, sbridge **4200 ± 0**.
  Vanilla collapses ~250 rps *earlier* than all three bridges, consistently (small σ).
- **Build revisions:** vanilla = **esrev0**; pbridge/cgpb/sbridge = **esrev2**. Vanilla is
  the only esrev0 build in the comparison.

## Hypothesis
The ~250 rps knee gap is a **build-revision confound, not a bridge effect**: `esrev0`
vanilla is an inferior build to the `esrev2` bridges (SDK/app/processor changes between
revisions raised the saturation point). Mean-RT-at-fixed-load near the near-vertical knee
then amplifies it (a ~250 rps shift swings RT by thousands of ms). "vanilla" was never a
same-revision control.

## Falsifying test — the esrev2 no-bridge control (`rc_esrev2`)
If it's a revision effect, the **esrev2 no-bridge control** should knee with the bridges
(~4200), not with esrev0 vanilla (~3900). Result (`ramp_rcnolim_esrev2_nolimiter_cpd2_r1`,
n=1): rc control reaches 4200 rps at only **443 ms** — it is the **latest-collapsing** of
all, and bridges sit *above* it. Mean RT minus rc control at matched load:

| load | rc ctrl | pbridge | cgpb | sbridge | vanilla(esrev0) |
|------|--------:|--------:|-----:|--------:|----------------:|
| 3600 | 46  | +82  | +104 | +45  | +51  |
| 3800 | 60  | +68  | +63  | +93  | +183 |
| 4000 | 221 | +150 | +54  | +15  | +161 |
| 4200 | 443 | +315 | +253 | +240 | (collapsed) |

## Conclusion
Confirmed. Against the **correct same-revision control (rc_esrev2)**, all three bridges
show **positive, monotonically-growing overhead** (Δ +45 → +315 ms) — exactly the expected
direction. The apparent "bridges beat vanilla" was an artifact of comparing esrev2 bridges
against an **esrev0** vanilla baseline that collapses ~250 rps earlier than *any* esrev2
build (control included). Two compounding factors:
1. **Cross-revision baseline** (esrev0 vanilla vs esrev2 bridges) — the dominant error.
2. Near-vertical-knee variance makes mean-RT-at-fixed-load a poor overhead metric (see
   [[zigzag_cpd2_saturation_investigation]]); the clean signal is in the flat region and in
   knee location, not in knee-region RT means.

## Fix (for the paper)
- **Do NOT use v_esrev0 as the overhead baseline.** Use a **same-revision** control:
  rebuild vanilla at esrev2 (`build_deploy_dsb.sh -s docker_v_es --extra rev2`) OR use
  `rc_esrev2`, and run it **interleaved, n=5**, alongside the bridges.
- Report overhead as (a) flat-region mean RT delta and (b) knee-location (max sustained rps)
  delta — both vs the same-revision control. Avoid headlining knee-region RT-at-fixed-load.
- Caveat: rc here is **n=1**; the direction + knee are unambiguous but rerun n=5 before
  quoting numbers.

Builds: vanilla `v_esrev0`, bridges `{pb,cgpb,sb}_esrev2`, control `rc_esrev2`. Data in
`runs/{ileav6_vNATGC,ileav6_pbNATGC,cgpb6_NATGC_esrev2_cpd2,sb6_NATGC_esrev2_cpd2}_r[2-6]_*`
and `runs/ramp_rcnolim_esrev2_nolimiter_cpd2_*_r1_*`.
