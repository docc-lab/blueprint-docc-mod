# Trace validity across the ramp (n = 5)

`trace-validity-n5.{pdf,png,svg}` — what fraction of captured traces is still
*usable* at each ramp point, by each mode's own rule. `trace-validity-n5.csv`
has the per-configuration, per-rate numbers behind it.

## The two rules

* **vanilla** — usable iff every span survived. This is not a strict scoring
  choice: vanilla has no recovery path, so a lost span is simply gone. The
  consequence is span-count amplification — a 23-span trace survives whole with
  probability `(1-p)^23`, so 3% per-span loss already costs half the traces and
  20% costs all but 0.6% of them. That exponent is a property of the deployment,
  not of the metric.
* **bridges** — usable iff (a) no checkpoint was lost and (b) every severed
  fragment is reattached to its **true nearest surviving ancestor**.
  Reconstruction is the bridges repo's own `pb0` / `cgp0` / `sb3`
  (`recon.ReconstructPB0`, `ReconstructCGP0`, `ReconstructSB3WithDEE`) run on the
  measured payloads.

## Where the data comes from

The already-captured `settled-traces.json.gz` of the n = 5 matrix — 10 campaign
roots, 8 configurations, 28 rates, 100 traces per point, 97,594 traces. No new
campaign and no simulation: every span, payload and drop is measured.

Ground truth is the workload itself. ComposePost emits one deterministic 23-span
tree whose operation names are unique within a trace; that is re-derived per case
from the intact captures and rejected if any two disagree (they never do). So the
true parent of every node, dropped or not, is known. Dropped nodes keep their real
span IDs wherever a surviving child's `CHILD_OF` reference, a CGPB hash array, or
a returned truss names them.

Wire formats are decoded, not assumed: the ranged window payload
`uvarint(depth) || checkpoint(8) || byte(distance-1) || bloom[geometry(distance)]`
(plus a hash array for CGPB, length-prefixed for SB) in `_br`, a depth varint in
`_d`, and the packed reverse envelope in `_rc`. Every payload must consume exactly
its declared length. **Zero decode errors across all 97,594 traces.**

Bloom geometry is derived from each payload's own window distance using the same
capacity rule and estimator both sides use, and membership is tested with the
prehashed scheme the SDK emits with (`recon.Config.Prehashed`).

## Checkpoint loss is measured, not modelled

A surviving span self-labels (`_br` = checkpoint, `_d`/`_o` = not). A dropped span
is classified from the survivors around it: a payload names its window anchor,
which is a checkpoint by definition, and every node strictly between that anchor
and the carrier lies inside the window and so is a non-checkpoint by definition.
What neither rule reaches is reported as unclassified rather than modelled.

This agrees with the collectors' own `cp_dropped` counter point for point.

**Below 7000 offered req/s the census is complete — zero unclassified drops — and
no checkpoint was lost in any bridge configuration in any of the five rounds.**
All uncertainty is confined to rates past the throughput wall (~7.3–7.8 k
achieved): 1,598 traces (1.6%) with a proven lost checkpoint, 10,374 (10.6%) with
at least one unclassified drop.

## What is NOT covered

* **S-Bridge is scored on its CG core only.** Its payload's orthogonal tail is
  byte-compatible up to and including the hash array, but this deployment emits a
  *dense* ordinal per window span while the reconstructor's sparse-ordinal layer
  expects only non-first branch choices. Feeding one to the other would be
  substituting a schema, so SB runs with `SB3IgnoreOrdinals` — a lower bound for SB.
* **2,723 traces (2.8%) are excluded from the reconstruction score**, all at
  rates ≥ 8000: a returned truss named an origin that could not be bound to a
  unique position because too many candidates at that depth were also missing.
  They are left out of the denominator rather than scored on a guessed identity.
* **The 100 traces per point are not a uniform sample of the 30 s window.** The
  capture queries Jaeger with `limit=100` bounded to the measured window, and
  Jaeger returns the most recent matches, so every sample is a contiguous
  0.3–0.8 s slice from the *end* of the window — where queues are fullest. Absolute
  percentages are therefore pessimistic, consistently so across configurations.
* **Traces whose `wrk2api` root span was dropped are invisible to the query**,
  because Jaeger finds a trace by that service. In a bridge the root is a
  checkpoint (priority 1) and almost always survives; in vanilla it is an ordinary
  span and is dropped at the prevailing rate. The vanilla curve is therefore
  *optimistic* — its worst traces are missing from the sample — so the
  vanilla-to-bridges gap shown here is a lower bound.
* All five rounds used seed 1001: five repetitions of one workload, not five
  workloads.

Raw per-point records, including the two scoring ablations, are archived under
`/users/tomislav/bridges/.local/dsb-validity/`.

## Different saturation points, and why they do not change the result

Each configuration saturates at its own rate, so a common offered rate compares
systems at different points of their own envelopes:

| config   | peak completed req/s | shedding onset (offered) | peak collector CPU / pod |
|----------|---------------------:|-------------------------:|-------------------------:|
| nt       |        12,006 ± 33   |            —             |        0.01 / 1.00       |
| v        |         9,043 ± 96   |          6,800           |        0.43 / 1.00       |
| pb off   |         7,845 ± 55   |          6,700           |        0.44 / 1.00       |
| pb on    |         7,457 ± 80   |          3,700           |        0.45 / 1.00       |
| cgpb off |         7,853 ± 71   |          6,500           |        0.43 / 1.00       |
| cgpb on  |         7,445 ± 34   |          3,600           |        0.47 / 1.00       |
| sb off   |         7,306 ± 42   |          4,900           |        0.40 / 1.00       |
| sb on    |         7,037 ± 141  |          2,600           |        0.48 / 1.00       |

`trace-validity-normalized-n5.*` re-plots the left panel against load as a
fraction of each configuration's **own** peak. The conclusion survives it:

| load / own peak | 0.6 | 0.8 | 1.0 | 1.2 | 1.5 |
|-----------------|----:|----:|----:|----:|----:|
| vanilla         | 100 |  10 |   0 |   0 |   0 |
| pb off          | 100 | 100 |  84 |  79 |  94 |
| cgpb off        | 100 | 100 |  85 |  76 |  91 |
| sb off          | 100 | 100 |  92 |  83 |  91 |
| pb on           |  79 |  67 |  63 |  50 |  34 |
| cgpb on         |  71 |  59 |  61 |  53 |  43 |
| sb on           |  65 |  58 |  48 |  44 |  43 |

Vanilla is the only configuration that reaches zero usable traces, and it does so
*before* its own knee. Above offered 7,500 its per-span drop is 31-66%, putting
`(1-p)^23` below 2e-4 in every round, so the measured 0 of 500 is the expected
count and not a sampling accident.

Between offered 6,500 and 7,500 the per-point outcome is all-or-nothing rather
than a partial decline: shedding is correlated in time and each capture is a
~0.02 s slice, so a round lands either in a shed phase or a recovery phase
(6,500 round 3 predicted 41.6% intact and measured 0%; 7,000 round 4 predicted
1.2% and measured 100%). The min-max band across the five rounds already spans
that range in the figure; the vanilla LINE through that band is an average of
coin flips and should not be read as a trajectory. A vanilla point is effectively
one observation replicated 100x -- at offered 8,000 the five rounds' span-count
histograms are {12:100}, {5:100}, {20:100}, {7:100}, {23:100}. Bridge captures
span 0.1-0.4 s and do show a real spread.

**Collector CPU is not the binding constraint anywhere in these runs.** Per-pod
collector CPU peaks at 0.40–0.48 of its 1-core limit for every configuration at
every rate. Admission is a *memory* policy: the priority processor runs
`soft_percentage: 50 / hard_percentage: 70` against GOMEMLIMIT 230 MiB with
`force_gc` on a 100 ms check interval (vanilla instead runs `memory_limiter` at
70/20). The large ON/OFF difference in shedding onset (3,600–3,700 vs 6,500–6,700;
SB 2,600 vs 4,900) is `_rc` bytes consuming that memory budget sooner at the same
span rate — the span rate into the collector is identical across configurations at
a given offered rate.

---

# Reconstruction-eligible traces (supersedes the accuracy figure)

`reconstruction-eligible-n5.*`, data in `scratchpad/eligible.json`.

This counts eligibility, not accuracy, and needs **no ground truth**:

* **vanilla** — kept iff every span survived. It has no recovery path, so a lost
  span is simply gone.
* **bridges** — kept iff every window anchor named by a surviving payload is
  still present. A checkpoint restored by a returned truss (`_rc`) counts as
  present, which is exactly what the response path buys.

Both are read straight off the wire. No reconstruction is run, no truth tree is
built, no span identity has to be inferred. That removes the whole class of
problems the accuracy figure had: the truth of a dropped span's identity is only
partially observable (20-43% of dropped positions have no recoverable id), and
scoring against a minted id biases against the reconstructor.

| offered | vanilla | PB off | PB on | CGPB off | CGPB on | SB off | SB on |
|--------:|--------:|-------:|------:|---------:|--------:|-------:|------:|
|  ≤6000  |   100   |  100   |  100  |   100    |   100   |  100   |  100  |
|  6500   |    60   |  100   |  100  |   100    |   100   |  100   |  100  |
|  7000   |    20   |  100   |  100  |   100    |   100   |  100   |  100  |
|  7500   |     0   |  71.8  | 100.0 |   82.4   |  100.0  |  84.4  | 100.0 |
|  8500   |     0   | 100.0  |  94.6 |   89.6   |   88.8  |  74.8  |  93.6 |
| 10000   |     0   |  67.4  | 100.0 |   59.8   |   96.2  |  77.2  |  92.0 |
| 12000   |     0   | 100.0  |  93.4 |  100.0   |  100.0  |  90.4  | 100.0 |
| 14000   |     0   | 100.0  | 100.0 |   86.2   |  100.0  | 100.0  | 100.0 |

Vanilla is the only configuration that reaches zero. Past the knee the response
path is generally ahead of the same bridge without it — most visibly at 7500
(71.8/82.4/84.4 off versus 100.0 for all three on) and 10000 (67.4/59.8/77.2
versus 100.0/96.2/92.0) — because a returned truss keeps the anchor alive when
the checkpoint's own record is shed.

Caveats carried over unchanged: the 100 traces per point are Jaeger's most-recent
100 within the window, so each is a 0.3-0.8 s slice (vanilla's are ~0.02 s and
effectively n=1 per point); and a trace whose wrk2api root span was dropped is
invisible to the query, which flatters vanilla, so the gap is a lower bound.

The earlier `trace-validity-n5.*` figure scored reconstruction ACCURACY. Its
bridge numbers went through three different criteria and a harness defect and
should not be used; the vanilla curve in it is unaffected (it never depended on
reconstruction).
