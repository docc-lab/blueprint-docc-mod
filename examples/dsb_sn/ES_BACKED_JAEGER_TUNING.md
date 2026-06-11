# ES-Backed Jaeger Tuning — Findings

Investigation of trace-pipeline drop behavior with ES-backed jaeger as the
realistic downstream storage path. Pursued via the `docker_v_es` Blueprint
variant (vanilla SDK + Elasticsearch storage), running at 1024 b padding-off
unless noted.

## TL;DR

Jaeger's default `ES_BULK_WORKERS=1` is a serialization choke that masks any
upstream throughput signal. With it set to 10 (production-typical), drops at
realistic workloads collapse from 90%+ to 0–2.3%. Most earlier "memlim
shedding" measurements were artifacts of this default, not of the workload
or SDK design.

## Setup

- `docker_v_es` Blueprint variant (vanilla SDK + ES-backed jaeger added via
  the new `jaeger.CollectorWithElasticsearch` plugin function)
- `OTLP_RETRY=off`, `OTLP_DEADLINE_MS=1000` (env-var tunables baked into
  Blueprint dockergen)
- `SPAN_PADDING_BYTES=0` (no artificial wire-byte inflation)
- otelcol DaemonSet, 9 pods (one per node), 256 MiB cgroup, 500m CPU
- Jaeger pinned to node-9 with `nodeSelector`
- ES pinned to node-9, single-node mode, no container memory limit,
  `ES_JAVA_OPTS=-Xms4g -Xmx4g`

### Jaeger env vars (production-tuned vs jaeger defaults)

| env var | jaeger default | this setup | rationale |
|---|---|---|---|
| `ES_BULK_WORKERS` | **1** | **10** | parallel bulk-write goroutines — the dominant lever |
| `ES_BULK_SIZE` | 5 MB | 10 MB | larger bulks reduce per-batch overhead |
| `ES_BULK_ACTIONS` | 1,000 | 5,000 | more docs per bulk request |
| `ES_BULK_FLUSH_INTERVAL` | 200 ms | 200 ms | (kept default) |
| `COLLECTOR_QUEUE_SIZE` | 2,000 | 10,000 | bigger buffer for ES write hiccups |
| `COLLECTOR_NUM_WORKERS` | 50 | 100 | OTLP→ES translation workers |
| `COLLECTOR_OTLP_GRPC_MAX_MESSAGE_SIZE` | 4 MiB | 32 MiB | accommodates larger OTLP messages |

## Four-way load sweep

Vanilla SDK, ES-backed jaeger, no padding, retry off, 1 s deadline.

### App layer

| target rps | achieved | p50 lat | max lat | HTTP errors |
|---|---|---|---|---|
| 500 | 499.95 | 7.39 ms | 9.55 ms | 0 |
| 1000 | 999.81 | 11.28 ms | 21.63 ms | 0 |
| 2000 | 1998.22 | 43.18 ms | 73.86 ms | 0 |
| 2500 | 2493.88 | 130.51 ms | 744.45 ms | 0 |
| 3000 | 2957.18 | 187.48 ms | 1.29 s | 0 |

### SDK-side drops

| target rps | composepost | hometimeline | text/media/wrk2api | uniqueid/usermention |
|---|---|---|---|---|
| 500 | 0 % | 0 % | 0 % | 0 % |
| 1000 | 0 % | 0 % | 0 % | 0 % |
| 2000 | **2.3 %** | **2.3 %** | 0 % | 0 % |
| 2500 | **39.7 %** | **39.6 %** | **11.9 %** | 0 % |
| 3000 | **60.4 %** | **60.6 %** | **23.5 %** | 0.8 % |

### otelcol-side drops

| target rps | receiver_refused (memlim) | exporter_enqueue_failed | exporter_send_failed |
|---|---|---|---|
| 500 | 0 | 0 | 0 |
| 1000 | 0 | 0 | 0 |
| 2000 | 141,152 | 0 | 0 |
| 2500 | **3,413,422** | 0 | 0 |
| 3000 | **6,124,145** | 0 | 0 |

### Mid-test resource utilization (steady phase)

| | jaeger CPU peak / steady | ES CPU peak / steady | jaeger mem | ES mem growth |
|---|---|---|---|---|
| 500 | 5.32 / ~2 cores | 10.94 / ~2 cores | plateau ~190 MiB | 5.3 → 10.3 GiB |
| 1000 | 5.22 / ~3 cores | 10.81 / ~5 cores | plateau ~250 MiB | 7.7 → 14.8 GiB |
| 2000 | 5.44 / ~4 cores | 12.71 / ~10 cores | grew to 1.04 GiB | 12.2 → 20 GiB |
| 2500 | 5.06 / ~4 cores | 12.60 / ~11 cores | grew to 1.19 GiB | 23 → 35 GiB |
| 3000 | 5.23 / ~4 cores | 13.14 / ~11 cores | grew to 1.29 GiB | 22 → 28 GiB |

### Cliff between 2 k and 3 k rps

Between 2 k and 3 k rps the pipeline transitions sharply from "almost-clean"
to "saturated":

| metric | 2 k rps | 3 k rps |
|---|---|---|
| composepost drop % | 2.3 % | **60.4 %** |
| otelcol memlim total | 141 k | **6.12 M** |
| App p50 latency | 43 ms | **187 ms** |
| App max latency | 74 ms | **1.29 s** |

App-layer latency degradation at 3 k rps (p50 187 ms, max 1.29 s) suggests
the application *itself* is starting to feel pressure too — probably
internal gRPC retries within the call graph kicking in. The trace pipeline
ceiling under this tuning is somewhere in the 2 k–2.5 k rps band; above
that, both the trace pipeline AND the application start saturating.

## Before / after on the bulk-worker fix

Same workload (2 k rps, 5 min steady, no padding):

| metric | `ES_BULK_WORKERS=1` (default) | `ES_BULK_WORKERS=10` |
|---|---|---|
| composepost SDK drop rate | 95.7 % | **2.3 %** |
| hometimeline SDK drop rate | 89.0 % | **2.3 %** |
| otelcol memlim total | 10.2 M | 141 k |
| otelcol enqueue_failed total | 47 k | **0** |
| jaeger CPU peak | 1.43 cores | 5.44 cores |
| ES CPU peak | 2.16 cores | 12.71 cores |
| ES memory | pegged at 3 GiB | grew freely to 20 GiB |

## Findings

1. **`ES_BULK_WORKERS=1` is a misconfiguration trap.** A single bulk-write
   goroutine serializes all ES indexing, leaving jaeger pegged at <1 core
   and ES at ~1 core regardless of available hardware. Production
   deployments commonly use 8–32 workers.

2. **Span padding is a red herring at these regimes.** Pre-fix, drops were
   90 %+ regardless of padding (1024 B or 0 B). Post-fix, the pipeline
   carries un-padded spans at 2 k rps without breaking a sweat. The
   bottleneck was always upstream of any byte-rate concern.

3. **The pipeline scales cleanly through 1 k rps** with zero drops anywhere
   — SDK side, memlim, or exporter queue. At 2 k rps a small (~2.3 %)
   residual appears on the two node-1 services as transient memlim trips.

4. **App layer is fully insulated** in all three sweeps. Trace-pipeline
   backpressure never propagates to the request path.

5. **Earlier "memlim drop rate" measurements need re-interpretation.** With
   a sanely-tuned downstream, the trace pipeline doesn't gratuitously trip
   memlim. The 30 %+ drop rates we measured under "production-realistic
   conditions" were partly artifacts of the bulk-worker default plus the
   retry-storm dynamic, not intrinsic to the workload.

## Implications for the priority-shedding experiment

The substrate for measuring priority-aware shedding (SB SDK + new priority
processor) needs to actually saturate the tuned pipeline. The current
ceiling is somewhere above 2 k rps. To get a meaningful priority-vs-no-
priority comparison, options are:

- Push to **3 k–5 k rps** to find the new ceiling
- **Reintroduce padding** at 2 k rps to drive the 2.3 % residual into a
  more meaningful shedding regime
- **Constrain ES resources** to simulate undersized production deployments
- Test with **`ES_BULK_WORKERS` back at 1** to keep the historical
  bottleneck and isolate just the SDK-layer comparison

## Run directories

- 500 rps: `runs/nopad_vesrev0_500rps_bulkw10_164908/`
- 1 k rps: `runs/nopad_vesrev0_1krps_bulkw10_165815/`
- 2 k rps: `runs/nopad_vesrev0_2krps_bulkw10_170735/`
- 2.5 k rps: `runs/nopad_vesrev0_2500rps_bulkw10_181356/`
- 3 k rps: `runs/nopad_vesrev0_3krps_bulkw10_171839/`
- 2 k rps original (bulk_workers=1, padded): `runs/padded_vesrev0_2krps_5m_134354/`
- 2 k rps original (bulk_workers=1, nopad):  `runs/nopad_vesrev0_2krps_5m_141343/`
