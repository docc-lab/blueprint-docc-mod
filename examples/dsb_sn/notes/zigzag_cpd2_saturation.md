# Investigation: latency "zigzag" for PB/CGPB at cpd=2 in the saturation knee

**Opened:** 2026-06-16
**Status:** OPEN — hypothesis formed, decisive experiment (RC isolation) not yet run.

## Observation
In the **no-limiter** experiment set (collector = `[batch]` only, 4Gi/1000m, nothing
shed), the by-cpd mean/p99 response-time plots show PB and CGPB at **cpd=2**
oscillating non-monotonically ("zigzag") once latency starts climbing (~3800→5000
rps), while **vanilla is smooth/monotonic**. cpd=6 is clean for the bridges too.
Slopes/overall trajectory match vanilla; it's the step-to-step bounce that's wrong.

Mean latency (ms), no-limiter cpd=2 (single runs except CGPB n=2 shown separately):
```
 rps   vanilla   PB    CGPB-r1  CGPB-r2
3800    122      98     79       87
4000    358    1430   2250      333
4200    732     796    258      808
4400   1540    2960   3490     2010
4600   2430    2320   1550     2250
4800   3220    4450   5100     3710
5000   5760    5550   4760     4580
```

## Characterization findings (what the data already tells us)
1. **Not a systematic per-rps oscillation.** The two CGPB runs spike at *different*
   rps (r1 spikes @4000=2250 while r2 is calm @333; r2 higher @4200 while r1 drops to
   258). A load-tied mechanism would phase-align across runs → it does not → this is
   **run-to-run / per-step sampling variance**, not deterministic bridge behavior.
2. **Throughput is clean; only mean latency bounces.** Achieved-rps rises smoothly for
   all variants. Within a step, achieved and latency are **inversely coupled** (CGPB-r1
   @4000: achieved 3653↓ / latency 2250↑; r2 @4000: achieved 3949↑ / latency 333↓) —
   the signature of a **transient stall**: some 30s windows catch the app backed-up,
   others flowing.
3. **Vanilla's smoothness is partly luck (n=1 monotonic draw), but the bridges
   genuinely jitter more.** The saturation knee is near-vertical (0.3s→5s over ~600
   rps), so any extra jitter is amplified into large visible swings. Vanilla has no
   background SDK work; the bridges do.

## Leading hypothesis
The bridge SDK's periodic background activity inside the app pods — the 100ms flush
(`SBExportInterval`), the HP/LP dual-buffer export goroutines, and **GC from per-span
breadcrumb allocation** — injects transient CPU stalls. **cpd=2 is worst because it
allocates the most**: 50% of spans are checkpoints emitting the full `_br`
(varint depth ‖ ckpt4 ‖ bloom) vs 17% at cpd=6 (which is why cpd=6 is clean). In the
near-vertical knee, a stall coinciding with a 30s wrk step produces a latency spike at
a random rps → the zigzag. Vanilla allocates far less per span → less GC → smoother.

## Decisive experiment (planned, not yet run)
The **RC control processor** (`BRIDGE_KIND=rc`) is the scalpel: it has the *identical*
export machinery (dual HP/LP buffers, `bridges-priority` header, `_br` random bytes via
`RC_BR_BASE/SLOPE`) but **zero PCRB structure** (no bloom ops, no ckpt4 — just a 1/cpd
coin flip).
- Run **RC cpd=2 no-limiter**, with `RC_BR_BASE/SLOPE` set so `_br`≈ PCRB cpd2 size (~12B).
  - **If RC cpd=2 also zigzags** → cause is the export/buffer/GC machinery (fixable:
    flush-cadence tuning, GC tuning, buffer/byte pooling, sync.Pool for breadcrumbs).
  - **If RC cpd=2 is clean** → cause is PCRB-specific work (bloom alloc/ops).

## Secondary / corroborating
- **Quantify variance:** 5 rounds each of PB cpd=2 + CGPB cpd=2 → per-step mean±std/CI.
  If the averaged curve smooths toward vanilla and std is large only in the knee, that
  confirms it's variance amplified by the vertical knee (a *plotting/methodology* issue,
  not a bridge defect).
- **Resolve the knee:** finer rps steps (4000→4800 by 100) at **60s** steps (vs current
  30s) to stop aliasing the near-vertical region with a too-short, multi-second-latency
  sampling window.

## Methodology note
This file is the template for the practice: when an anomaly appears, log
Observation → Characterization (what the data rules in/out) → Hypothesis → the decisive
experiment that would falsify it, *before* running more sweeps.

## VERDICT (2026-06-17) — RC isolation run
RC cpd2 no-limiter (identical export machinery: dual HP/LP buffers, bridges-priority
header, 50%-HP coin flip, 28B random `_br` per checkpoint — but NO PCRB structure)
ramps **smooth & monotonic**, no zigzag (3800→4400: 60→221→443→1060 ms; even below
vanilla). vs PB/CGPB cpd2 which swing 1.4–3.5 s in that band.

⇒ **The zigzag is NOT the export/buffer/GC machinery** (RC has all of it, clean).
⇒ **It is PCRB-specific**: the per-span bloom ops in OnStart — `bloom.NewFromBytes`
(decode inbound) → `Add(spanID)` → `Bytes()` (re-encode) on every span on the request
hot path. Static `_br` allocation alone (RC) does NOT cause it, so it's the bloom
*operations/encode-decode churn*, not breadcrumb size. Heaviest at cpd2 (50% checkpoints).
Next: confirm via pprof/alloc on a PB cpd2 app pod, and test a cheaper bloom path
(reuse buffer / skip re-encode when unchanged / coarser bloom).

(Caveat noted: prior cluster runs were invalid — fresh-node bring-up gaps: missing
otelcol image, missing wrk2, 0.9GHz CPUs, missing service pinning, and missing aiohttp
seed dep. This RC run is on the corrected cluster — see [[feedback_new_cluster_bringup]].)

## UPDATE (2026-06-17) — GC mode is the zigzag lever; gctrace + forced-vs-natural
gctrace from the composepost app pod (`GODEBUG=gctrace=1`) revealed the app pods run
**`GOGC=off` + `GC_INTERVAL_SEC=0.1`**: a forced `runtime.GC()` every 100ms (10 GC/s,
all `(forced)`), heap pinned at 5–6 MB. This is deliberate methodology — fixed GC
cadence identical across ALL variants (vanilla/rc/pb/cgpb) so per-span tracing cost is
isolated from GC-timing noise (vanilla esrev0 + rc both also set it → comparison is fair).

Per-step GC binning (PB cpd2, forced): GC-CPU/s climbs 197→1463 ms/s (→~1.5 cores at
5k); mark-assist/s 1.6→112; **max-STW pause zigzags in lockstep with mean latency**
(spike steps 4000/4400/5000 → 8.8/10.4/9.7ms; dip steps 4200/4600 → 6.3/6.5ms). So the
zigzag = the fixed 100ms forced-GC cadence being sampled by 30s wrk steps against the
near-vertical knee: a step randomly catches more/fewer expensive bursts → non-monotonic.

**Forced (GOGC=off,0.1) vs Natural (GOGC=100, interval=0)** — PB cpd2 no-limiter, both 0 non_2xx:
```
 rps   forced mean/p99     natural mean/p99    achieved f/n
3200   78 / 458 (spike)    45 / 104            3145 / 3148
3400   47 / 187            96 / 204            3347 / 3314
3800   77 / 188            167 / 434           3761 / 3759
4000   494 / 1410 (spike)  333 / 781           3949 / 3963
4200   168 / 696 (dip)     1250 / 2670         4067 / 3831
4400   1090 / 1570         3650 / 5130         4250 / 3613
5000   4090 / 6840         5930 / 8750         3824 / 3506
```
**Natural GC removes the zigzag** (latency climbs ~monotonically 45→96→149→167→333→1250
→3650→…) BUT: (a) ~5–8ms higher baseline at low rps, (b) heap grows to **107 MB live /
221 MB goal** under load (vs pinned 5–6 MB forced) → bigger collections + heavier
mark-assist, (c) **earlier saturation** — achieved-rps ceiling drops (~3500 vs forced
~4250) and knee latency is far worse (4200: 1.25s vs 168ms). Forced keeps the heap tiny
→ cheap predictable collections → higher throughput ceiling, at the cost of the sampling
zigzag.

⇒ Refined verdict: the zigzag is a **GC-cadence sampling artifact** (fixed 100ms forced
GC × near-vertical knee × 30s steps), riding on top of the real per-span PCRB bloom-alloc
cost (RC isolation). GC mode changes only how that cost *manifests*: forced = zigzag +
higher ceiling; natural = smooth + earlier saturation. For smooth publishable curves use
natural GC (or longer/finer steps to de-alias the forced cadence); for max throughput
keep forced. Either way the fix for the underlying cost is cheaper bloom ops
(sync.Pool / reuse buffer / skip re-encode).

Side finding: `GOGC=off` + `GC_INTERVAL_SEC=0` (no GC at all, no mem-limit) OOM-LOCKED a
node — *some* GC is load-bearing. See [[feedback_gogc_off_needs_forced_gc]].
Artifacts: `runs/ramp_pbgctrace_…071208/` (forced) + `runs/ramp_pbNATGC100_…101942/` (natural).

## FINAL (2026-06-17) — 5×5 averaged sweep, natural GC: pbridge cpd2 ≡ vanilla
5 rounds vanilla + 5 rounds pb cpd2, all natural GC (GOGC=100, interval=0), no-limiter,
fresh teardown+seed per round (deploy-to-deploy independence), 2000→5000@30s, all 0 non_2xx.
```
 rps   vanilla mean±sd    pb cpd2 mean±sd
 2000      23±4 ms           20±2 ms
 3000      45±5 ms           41±2 ms
 3400      77±13 ms          64±13 ms
 3800     185±64 ms         122±15 ms      <- pb LOWER through knee-onset
 4000     820±511 ms        386±174 ms
 4200     1.32±0.26 s       0.77±0.25 s
 4400     2.98±0.22 s       2.89±0.27 s
 5000     5.94±0.28 s       5.55±0.17 s
```
Curves overlap within sd at every load; achieved-rps ceiling identical (~3850); pb if
anything marginally faster 3800–4200. **⇒ Under natural GC, pbridge cpd2 has NO
measurable latency penalty vs vanilla.** The big sd at 4000–4200 = metastable knee
(reason for 5 rounds). Plot: `runs/natgc_sweep_vanilla_vs_pb.png`. Sweep dirs:
`runs/sweep5_{v,pb}NATGC100_*`. Plot venv: `/tmp/plotvenv` (matplotlib; system has none).

**Closing the whole investigation:** forced 100ms GC (GOGC=off) → pb zigzags + looks
expensive; natural GC → pb ≡ vanilla. The PB "overhead"/zigzag was a GC-cadence sampling
artifact, NOT intrinsic tracing cost. The real per-span bloom-alloc cost (RC isolation)
is real but cheap enough to vanish into noise once GC isn't force-clocked at 100ms.

## DEFINITIVE (2026-06-18) — interleaved A/B, blocked-design confound removed
The 5×5 above was BLOCKED (all vanilla, then all pb) → suspicious pb-faster knee gap =
possible time-drift confound. Reran INTERLEAVED (v,pb,v,pb,… 6 rounds each), dropped r1
(cold-cluster), averaged r2-6 (n=5 each), natural GC, 0 non_2xx throughout:
```
 rps   vanilla mean±sd   pb cpd2 mean±sd
 2000      22±3 ms          23±2 ms     <- sub-knee: identical within ±3ms
 3000      50±6 ms          52±7 ms
 3200      73±27 ms         71±26 ms
 3400      92±46 ms        139±66 ms     <- knee: high variance, sign flips
 4000     566±419 ms       371±146 ms
 4200     1.34±0.17 s      0.88±0.18 s
 4400     2.90±0.21 s      2.97±0.28 s   <- flips back +68ms
 5000     6.07±0.50 s      5.72±0.32 s
```
Result: sub-knee (≤3200) vanilla ≡ pb within ±3ms (tight sd) — **zero measurable tracing
overhead**. Knee (≥3400) is metastable: pb trends marginally lower but sign flips and all
deltas are within ~1-2 sd → NOT a real pb advantage, just knee noise. Interleaving killed
the blocked-design "pb faster" artifact; the no-overhead conclusion survived a clean A/B.
Both saturate ~3850 achieved rps. Plot: `runs/interleaved_vanilla_vs_pb.png`. Dirs:
`runs/ileav6_{v,pb}NATGC_r1..6_*` (r1 = dropped warmup). FINAL ANSWER for the paper:
under natural GC, pbridge cpd=2 imposes no measurable app-latency cost vs vanilla.

## THREE-WAY (2026-06-18) — + CGPB cpd=2 natural GC (n=5, r2-6, r1 dropped)
Rebuilt cgpb_esrev2 (was cpd=6, off+0.1 → set cpd=2 via otelcol configdiscovery config_map,
GOGC=100+interval=0, pinned, d2k8s --build-only), ran 6 rounds, dropped r1. cpd is
collector-side in the otelcol config (`configdiscovery: config_map: cpd`) and the APP
fetches it at runtime — so cpd change needs only an otelcol rebuild, no app rebuild.
```
 rps    vanilla      pbridge      cgpb
 2000   22±3 ms      23±2 ms      19±1 ms     <- sub-knee: all 3 within a few ms
 3000   50±6 ms      52±7 ms      42±3 ms
 3400   92±46 ms     139±66 ms    117±54 ms   <- knee: all overlap within sd
 4200   1.34±0.17s   0.88±0.18s   0.87±0.25s
 5000   6.07±0.50s   5.72±0.32s   5.95±0.88s
```
All three trace ONE curve, same ~3850 achieved-rps ceiling. ⇒ under natural GC NEITHER
bridge (pbridge nor cgpb) at cpd=2 has measurable app-latency overhead vs vanilla — makes
sense: pb & cgpb share the app-side breadcrumb machinery and differ only in the
collector-side processor, which is off the request path in the no-limiter [batch] pipeline.
Plot: `runs/threeway_natural_vanilla_pb_cgpb.png`. Dirs: `runs/cgpb6_NATGC_esrev2_cpd2_*_r1..6_*`.

## FOUR-WAY (2026-06-18) — + SBridge cpd=2 natural GC (n=5, r2-6, r1 dropped)
Rebuilt sb_esrev2 (was cpd=4, off+0.1, traces pipeline [priority,batch]). To match the
pb/cgpb/vanilla no-limiter protocol, STRIPPED the priority processor → traces=[batch]
(user-approved: isolates app-side bridge cost; the otelcol daemonset shares node CPU with
app pods, so leaving [priority] would confound app-overhead with collector-processor CPU).
Set cpd=2 (configdiscovery), GOGC=100+interval=0, pinned, built, ran 6 rounds, dropped r1.
```
 rps    vanilla      pbridge      cgpb         sbridge
 2000   22±3 ms      23±2 ms      19±1 ms      23±3 ms    <- sub-knee: all 4 within a few ms
 3000   50±6 ms      52±7 ms      42±3 ms      36±5 ms
 3400   92±46 ms     139±66 ms    117±54 ms    61±9 ms    <- knee: all overlap within sd
 4200   1.34±0.17s   0.88±0.18s   0.87±0.25s   0.68±0.17s
 5000   6.07±0.50s   5.72±0.32s   5.95±0.88s   5.48±0.36s
```
All FOUR trace one curve, same ~3850 achieved-rps ceiling, overlap within sd. (SB runs a
hair low at 3000-3400 — 36/46/61 vs vanilla 50/73/92 — but bounces high again at 4000=652ms;
noise-band, not a real edge, and SB was its own block not interleaved w/ vanilla.)
⇒ **DEFINITIVE: under natural GC, NONE of the three bridges (pbridge, cgpb, sbridge) at
cpd=2 imposes measurable app-latency overhead vs vanilla.** The forced-100ms-GC zigzag was
the only "cost," and it was a GC-cadence artifact. Plot: `runs/fourway_natural_all.png`.
Dirs: `runs/sb6_NATGC_esrev2_cpd2_*_r1..6_*`. Analysis: `/tmp/fourway_analysis.py` (venv `/tmp/plotvenv`).

## FOUR-WAY @ cpd=6 (2026-06-18) — interleaved, natural GC, n=5 (r2-6, r1 dropped)
Repeated the four-way at cpd=6 (the LOW-allocation end: ~17% checkpoints vs 50% at cpd=2).
otelcol cpd 2→6 (rebuild otelcol images only — cpd is in otelcol configdiscovery, app
fetches at runtime; see [[dsb_sn_variant_build_pipeline]] GOTCHA 4), GOGC=100+interval=0,
sb pipeline=[batch]. Interleaved v→pb→cgpb→sb ×6. (Hit a chain bug mid-setup: PREV_DIR
missing `build_` prefix → delete no-op → `gone` hung 81min; fixed to `build_$1`.)
```
 rps    vanilla      pbridge      cgpb         sbridge
 2000   23±2 ms      23±2 ms      21±3 ms      21±4 ms    <- sub-knee: all 4 identical
 3000   53±9 ms      44±5 ms      45±5 ms      43±6 ms
 3400   78±18 ms     76±16 ms     76±23 ms     91±65 ms   <- knee: overlap within (large) sd
 4200   1.42±0.28s   0.81±0.15s   0.80±0.10s   0.92±0.39s
 5000   5.76±0.22s   5.57±0.18s   5.63±0.33s   5.83±0.39s
```
Same result as cpd=2: all four trace one curve, overlap within sd, ~3850 ceiling. As
expected cpd=6 (less per-span breadcrumb work) is, if anything, even more clearly free.
⇒ **COMPLETE: under natural GC, all 3 bridges at BOTH cpd=2 and cpd=6 have no measurable
app-latency overhead vs vanilla.** Plot: `runs/fourway_cpd6_natural_all.png`. Dirs:
`runs/cpd6il_{v,pb,cgpb,sb}NATGC_r1..6_*`. Analysis: `/tmp/fourway_cpd6_analysis.py`.
