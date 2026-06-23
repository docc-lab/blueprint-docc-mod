# R5: bridge-data overhead on an overloaded tracing collector

**Reviewer ask (R5):** show the max throughput the tracing infrastructure can
achieve under high stress *with and without* spans carrying bridge data. Add load
in a loop to overload the collector without overloading app logic.

## Setup
- **Generator:** `tgenload` DaemonSet (one pod/worker node 1-9), each pod forks
  `N=24` telemetrygen processes (workers 4) at `--rate 20000` against the
  **node-local** collector (`internalTrafficPolicy: Local`). Image
  `10.10.1.1:30000/tgenload:latest` (entrypoint forks `run_br`/`run_d`/`run_plain`).
  No app — pure OTLP span load.
- **Collectors:** otelcol pb-esrev2 DaemonSet, 9 pods, **1-core cap each** (9 cores
  total), `file -> /dev/null` exporter (removes the jaeger/ES backend ceiling so the
  COLLECTOR is what saturates), traces pipeline `[batch] -> [file]`.
- **Payloads (exact wire bytes, proto `bytes`, not base64):**
  `_br = varint(depth,1B) + ckpt4(4B) + bloom(ceil(m/8))`, bloom cap = `cpd-1` @
  `DefaultBloomFPRate=0.0001` -> 8/10/13/15/17 B for cpd 2/3/4/5/6. `_d = varint(depth)`
  = **1 B**. Checkpoint fraction = `1/cpd` of instances emit `_br`, rest emit `_d`.
- **Metric:** sum over 9 pods of `otelcol_receiver_accepted_spans` delta / window.
  At saturation `accept/s` == max throughput; `otelcol_process_cpu_seconds` confirms
  the cap is the bottleneck.

## CRITICAL: saturate, or you measure the wrong thing
At `N=6` the generators only offered ~750k/s -> collectors idled at **8.0/9 cores**
-> `accept/s` was generation-limited, NOT collector-limited (useless for "max
throughput"). Ramp `N`: 6->16->24 took throughput 750k->1.11M->**1.29M** and pinned
cpu at **9.0-9.25/9**. Only then is `accept/s` the real ceiling. Always check the cpu
column is at the cap before trusting a throughput number.

## Result (n=9 pooled = batch1 x5 + batch2 x4, mean +/- pstdev)
Two independent batches, run on different days. batch2 round-1 dropped (cold-start:
freshly-applied ds hadn't ramped, plain came in at 989k/cpu 7.78 -- unsaturated).

| config | max throughput (spans/s) | CV | vs plain | batch1 vs batch2 |
|---|---|---|---|---|
| plain (no bridge) | 1,168,177 +/- 6,254  | 0.5% | --     | -0.10% |
| cpd=6 (_br 17B)   | 1,088,185 +/- 11,764 | 1.1% | -6.8%  | +1.08% |
| cpd=3 (_br 10B)   | 1,056,073 +/- 12,565 | 1.2% | -9.6%  | +1.34% |
| cpd=2 (_br 8B)    | 1,052,610 +/- 15,201 | 1.4% | -9.9%  | +0.63% |
| all-span (worst)  | 1,047,289 +/- 15,539 | 1.5% | -10.3% | +0.23% |

Monotonic in checkpoint density; CV <=1.5%; the two batches agree to <=1.34% on
every config (plain to 0.10%) -- averages are stable. Figure:
`~/runs/r5_max_throughput_n9.png` (pooled `~/runs/r5_pooled_n9.tsv`). Per-span CPU at saturation:
plain 7.55 us -> allspan 8.50 us (better batching at high tput than the N=6
generation-limited run, where plain was ~10.9 us — so report CPU/span only at a
stated throughput, it is load-dependent).

**Takeaway for the paper:** on an overloaded collector, bridge data costs ~6% of
peak throughput at realistic cpd=6, ~10% in the every-span worst case.

## Follow-on: payload SIZE vs KV COUNT (what actually drives collector cost)
Two extensions answered "is it the bytes or the attributes?":

1. **CGPB-sized payloads** (sim `payload_B/ckpt`: cpd6=29B, cpd3=23B, cpd2=20B vs PB's
   17/10/8) cost only ~1pp more throughput per ~12 added bytes. So bigger `_br` ~= free.
2. **KV-count isolation** (MODE=multikv, every span carries K attributes). Two ways,
   SAME answer => the cost is attribute COUNT, not bytes. Throughput vs 1KV reference:

   | K | wire-fixed ~84B (n=5) | value-fixed 24B (n=2-3) |
   |---|---|---|
   | 1 | 1,062,638 (ref)       | 1,066,462 (ref) |
   | 2 | 960,189  (-9.6%)      | 981,412  (-8.0%) |
   | 3 | 853,456  (-19.7%)     | 863,928  (-19.0%) |
   | 4 | 790,685  (-25.6%)     | 793,802  (-25.6%) |
   | 6 | 716,616  (-32.6%)     | 722,248  (-32.3%) |

   In the wire-fixed control every config is ~84B on the wire (value = 84/K - 10), so
   the ~33% 1->6KV drop has ZERO byte confound. The value-fixed sweep (24B value, wire
   grows 34->84B) gives the SAME -32% => the ~50 extra wire bytes contributed ~1pp.
   Plain baseline ~1.12M; 0->1KV alone costs ~6%. Figure `~/runs/kv_count_throughput.png`
   (the two curves overlap). harnesses `/tmp/kv_measure.sh` (value-fixed),
   `/tmp/kv_measure_total84.sh` (wire-fixed); data `~/runs/kvcount_*`, `~/runs/kvtotal84_*`.

**Mechanism (reasoned, NOT yet profiled):** per added attribute, dominant cost is
per-submessage DECODE + heap ALLOCATION during OTLP unmarshal — each KeyValue, each
nested AnyValue, each key string is a separate allocated object (6x4B => ~18 small
objects vs ~3 for 1x24B), so malloc/GC scales with attribute count not bytes. pdata
`pcommon.Map` construction (more entries) and O(N) per-attribute iteration in the
pipeline ride along but are secondary. SAME root cause as the app-side GC finding
(per-KV alloc, not payload size) -> one unifying story across both ends of the pipe.
Design implication: pack everything into ONE `_br`/`_d` (PB/CGPB already do);
spreading the same bytes across many KVs would cost ~30% throughput.

Baseline telemetrygen span ~140-150B wire (computed from proto; ~74B of it is the 2
default attrs network.peer.address + peer.service). No wire-size metric is exported by
this otelcol build (0.139.0-dev) -> would need a pcap to measure exactly.

**DEFERRED / return-to-if-needed:** confirm the mechanism with a `pprof` CPU profile of
a collector under 1-KV vs 6-KV load (expect proto-unmarshal alloc to dominate). User
judged the reasoning sound enough to not block on it for now (2026-06-21).

## MASTER sweep: all bridge types x cpd 2-8 (the headline figure)
Plain + {pb, cgpb, sb} x cpd 2..8, n=2 rounds, GATED on 9/9 generators Running per
config (the gate matters: ungated, one node's gen mid-restart underfed its collector
and dropped plain ~1.15M; gated, plain hits its true ceiling ~1.23M). `_br` =
ceil(sim payload_B/ckpt) per type/cpd; non-ckpt key `_d`(1B) pb/cgpb, `_o`(2B) sb.
Saturation confirmed by `kubectl top` = 994-1003m per collector pod (at the 1-core cap).

| type | non-ckpt | mean spans/s | CV | vs plain |
|---|---|---|---|---|
| plain | -- | 1,234,040 | -- | -- |
| pb   | _d 1B | 1,048,648 | 0.6% | -15.0% |
| cgpb | _d 1B | 1,056,890 | 1.5% | -14.4% |
| sb   | _o 2B | 1,049,985 | 1.4% | -14.9% |

DEAD FLAT across cpd 2-8 (per-cpd means all 1.04-1.08M) AND across type, despite _br
ranging 10B(pb cpd2) -> 41B(sb cpd8). => the cost is exactly ONE bridge attribute per
span (~15%); type, cpd, and payload size are all third-order. Figure
`~/runs/master_throughput.png`; data `~/runs/master_20260621_064030/results.tsv`;
harness `/tmp/master_measure.sh` (note the 9/9 readiness gate in measure()).

NEXT (queued): carrier comparison at fixed ~20B payload, every span -- event-name
(span.AddEvent) and key-as-data (big key, empty value) vs the 1-attribute -15% baseline
(reuse master) -- to see if a cheaper carrier buys back part of the 15%. Needs a
patched telemetrygen (SDK AddEvent / custom attr); build+push image only AFTER any
running master sweep (it pulls :latest per config).

## Carrier comparison (NULL result): carrier choice doesn't matter
Same 20B payload on every span, different OTLP carrier, saturated gated harness, n=1:
| carrier | spans/s | vs plain |
|---|---|---|
| plain | 1,218,870 | -- |
| 1-attribute (from master) | ~1,050,000 | -15.0% |
| event-name (span.AddEvent) | 1,018,042 | -16.5% |
| key-as-data (key=20B, value="") | 1,041,873 | -14.5% |

All within ~2pp (single-run noise) => NO carrier within the span's structured children
beats a plain attribute. The proto-allocation reasoning (event/keydata ~half) was wrong:
each carrier brings its own fixed overhead that cancels the saving (event adds an Event
msg + mandatory timestamp + SpanEvent pdata slot; keydata still allocs KeyValue +
empty AnyValue + the 20B key string). REFINED takeaway: the ~15% is the cost of adding
ANY ONE structured child element to a span (decode + heap-alloc + pdata-slice growth +
downstream iteration); field layout and payload bytes are second-order.

The only proto-cheap lever (append to an EXISTING field: span name / trace_state) is
RULED OUT on backend grounds, not worth testing: span/operation NAME is a primary
low-cardinality index dimension (Jaeger/ES keyword index + operations list; Tempo/Parquet
name-column dictionary encoding; spanmetrics emits operation as a METRIC LABEL ->
unbounded series / Prometheus OOM; tail-sampling & service-maps assume categorical name).
Embedding per-span bytes makes name unique-per-span and breaks all of it. SAME trap as
key-as-data (high-cardinality KEYS): both "win" at the collector only by hiding data in a
field the backend assumes is low-cardinality -> they move the cost downstream where it's
worse + invisible. trace_state is the least-bad non-attr option (opaque propagation
field, not usually indexed) but 512B W3C limit + rides every header + many backends drop it.

CONCLUSION: no free collector-side carrier. The ~15% is the honest near-irreducible cost
of carrying ONE piece of per-span structured metadata through an overloaded collector;
the ATTRIBUTE is the semantically correct home (designed for arbitrary per-span metadata,
indexed as tags). Only legitimate lever = element COUNT (one _br/_d), which the bridges
already do. telemetrygen patched with `--event-name` (worker.go AddEvent); key-as-data
needs no patch (attr `<blob>=""`). data `~/runs/carrier_20260621_*`, harness
`/tmp/carrier_measure.sh`.

## Artifacts
- harness: `/tmp/cpd_measure.sh` (sweep, sets ds env per config, scrapes),
  `/tmp/tgenimg/entrypoint.sh` (`MODE`/`CKPT_FRAC`/`BRB`/`DB`/`N`/`KVN`/`KVB` env);
  `/tmp/kv_measure.sh` (value-fixed KV sweep), `/tmp/kv_measure_total84.sh` (wire-fixed control).
- data + figure: `~/runs/r5_cpd_measure_20260619_151530/{results.tsv,r5_max_throughput.png}`.

## Gotchas banked
- tgenload entrypoint `wait` ignores SIGTERM -> old pods hang `Terminating` through
  the grace period on every rollout; force-delete (`--grace-period=0`) or one node
  generates nothing and depresses the number. The sweep script does this.
- A LEFTOVER tgenload DaemonSet from a prior run will flood the same collectors and
  silently confound everything (saw cumulative counter at 2.8B, ~780k/s background
  under a zero-spam step). Verify only ONE generator is live before measuring.
- `pgrep -f cpd_measure.sh` self-matches the checking shell -> kill -> exit 144. Kill
  by explicit PID.
</content>
