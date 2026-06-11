# DSB-SN SB Pipeline Ramp Results — v3 (Pass-Through Priority)

Empirical characterization of the SB (structural bridge) DSB-SN pipeline
under load with the **pass-through priority processor** + **SDK-side
priority routing**. Companion to:

- `RAMP_RESULTS.md` — vanilla SDK + `memory_limiter` baseline (1 CPU / 1 GiB)
- `RAMP_RESULTS_SB_V2.md` — SB SDK + buffered priority v2 (1 CPU / 1 GiB)

This run is **at half the memory budget** (256 MiB) of the prior two.

Setup snapshot: **2026-06-08**, CloudLab `bridges-tb-02` testbed, target
identifier suffix `sbrev0` (variant `sb`, extra `rev0`).

## Architectural change vs v2

The v1–v5 priority processor maintained internal `hp/lp` slices and a
worker pool. Under sustained load at 256 MiB the heavy pod consistently
OOM-killed: hp_buf_depth grew to 125k–140k spans before workers could
drain it, allocations reached 200 MB, the cgroup killed the process.

**v3 removes the buffer entirely.** Architecture:

```
SB SDK partitions buffer by isCheckpoint → two batches per flush
    ├── CP batch  → UploadTraces ctx with "bridges-priority: hp"
    └── LP batch  → UploadTraces ctx with "bridges-priority: lp"
                  ↓
                  one gRPC connection per service, multiplexed
                  ↓
otelcol otlp receiver (include_metadata: true)
    ↓ propagates gRPC metadata into ctx
priority processor (pass-through)
    ├── reads ctx via client.FromContext(ctx).Metadata.Get("bridges-priority")
    ├── reads ms.Alloc and pressure state
    └── either passes batch straight through OR returns gRPC Unavailable
                  ↓
otlp exporter (sending_queue enabled, queue_size=1000) → jaeger
```

In-flight memory is bounded by `sending_queue` (queue_size × send_batch_size),
the same mechanism the vanilla / memory_limiter pipeline uses. The
priority processor owns zero in-flight memory.

| v2 priority processor | v3 priority processor |
|---|---|
| hp/lp Go slices, unbounded between checks | no slices |
| worker pool (10 goroutines), batch_size=256 | no workers |
| reads spans, classifies via `_br` attribute presence | reads `bridges-priority` gRPC metadata header |
| sending_queue.enabled: false on otlp exporter | sending_queue.enabled: true |
| 500 lines of Go | ~100 lines of Go |

## SDK change (sb_processor.go)

`flushBuffer` now partitions `snap` by `isCheckpoint` (already classified
at OnEnd time), builds separate `ResourceSpans` per priority class, and
calls `client.UploadTraces` separately for each — with
`metadata.AppendToOutgoingContext(ctx, "bridges-priority", "hp"|"lp")`.

Per-priority counters (`cpSent / lpSent / cpDropped / lpDropped`) are
attributed by `isHP` rather than re-inspecting span attributes at the
flush path.

## Collector config (build_sbrev0/docker/otelcol_sbrev0_ctr/config.yaml)

```yaml
receivers:
  otlp/highprio:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        include_metadata: true
      http:
        endpoint: 0.0.0.0:4318
        include_metadata: true

processors:
  priority:
    check_interval:  100ms
    low_percentage:  40    # alloc threshold to refuse LP
    mid_percentage:  60    # alloc threshold to refuse all
    high_percentage: 80    # ceiling (same behavior as mid for now)

exporters:
  otlp:
    endpoint: ${JAEGER_SBREV0_OTLP_DIAL_ADDR}
    sending_queue:
      enabled: true
      num_consumers: 10
      queue_size: 1000
    retry_on_failure:
      enabled: true
```

## Cluster shape

- 9 otelcol DaemonSet pods (one per worker node), **500m CPU / 256 MiB RAM**
- 13 backend service pods, pinned per `node-pinning-sbrev0.yaml`
- 1 jaeger all-in-one pod, dedicated node
- 2.2 GHz clamp via `utils/pin-cpu-22ghz.sh`
- Social graph seeded via `scripts/init_social_graph.py --graph socfb-Reed98`

## Workload

```
utils/ramp_with_snapshots.sh sbrev0 results_new_4/sbrev0_passthru 0
```

11 ramp steps (2000 → 4000 in 200-rps increments), 60s per step, **0s
break between steps** (BREAK_SECS=0). Preceded by 100s warmup at 100 rps
(`wrk -t 1 -c 10 -d 100s -R 100`). wrk thread/connection formula:
`C = ceil(rps² / 20000)`, `T = ceil(C/10)`.

## Results

### Per-step latency

| target rps | achieved | mean | p99 | non-2xx |
|---|---|---|---|---|
| 2000 | 1996 | 4.18 s ⚠️ | 15.94 s | 200 |
| 2200 | 2196 | 20.6 ms | 47.8 ms | 0 |
| 2400 | 2373 | 25.2 ms | 62.6 ms | 0 |
| 2600 | 2579 | 21.6 ms | 47.7 ms | 0 |
| 2800 | 2794 | 33.7 ms | 78.0 ms | 0 |
| 3000 | 2964 | 50.7 ms | 120 ms | 0 |
| 3200 | 3161 | 47.7 ms | 135 ms | 0 |
| 3400 | 3329 | 145 ms | 586 ms | 0 |
| 3600 | 2980 | 5.27 s | 11.0 s | 0 |
| 3800 | 2766 | 9.11 s | 16.5 s | 0 |
| 4000 | 2609 | 11.8 s | 20.6 s | 0 |

⚠️ Step 2000 was an outlier: the script's warmup line had `-t 2 -c 1`
which wrk refuses ("connections must be ≥ threads"). The warmup
silently no-op'd and the ramp opened cold at 2000 rps. Fixed to
`-t 1 -c 10`. Steps 2200+ are the real measurements.

**dsb_sn application saturation: ~3300 rps achieved.** Beyond step
3400 the app cannot push more spans; refusal counts plateau.

### Collector — per-pod end-of-run state

`9qx7t` is on node-1 with composepost (the trace fanout hub).

| pod | node | state | alloc | cp_admitted | lp_admitted | cp_refused | lp_refused | gc_count |
|---|---|---|---|---|---|---|---|---|
| **9qx7t** | **node-1** | none | 73 MB | **11.9 M** | **3.8 M** | **776 k (6.1%)** | **1.78 M (32.0%)** | **427** |
| 5flnf | node-4 | none | 66 MB | 1.99 M | 200 k | 0 | 0 | 21 |
| 9jcct | node-5 | none | 52 MB | 2.07 M | 5.57 M | 0 | 0 | 62 |
| ctd6x | node-3 | none | 73 MB | 3.78 M | 203 k | 0 | 0 | 36 |
| jtdcd | node-2 | none | 92 MB | 2.07 M | 241 k | 0 | 0 | 21 |
| k2w7c | node-9 | none | 55 MB | 203 k | 203 k | 0 | 0 | 7 |
| kbg4v | node-8 | none | 53 MB | 1.99 M | 204 k | 0 | 0 | 21 |
| kzg9z | node-6 | none | 94 MB | 1.99 M | 203 k | 0 | 0 | 20 |
| s5nt7 | node-7 | none | 73 MB | 1.99 M | 203 k | 0 | 0 | 21 |

**Pod restarts: 0.** **OOMKills: 0.**

### Collector — aggregates

| Metric | Value |
|---|---|
| Total CP admitted | 27.97 M |
| Total LP admitted | 10.83 M |
| Total CP refused | 776 k (2.7% of all CP) |
| Total LP refused | 1.78 M (14.1% of all LP) |
| LP refused : CP refused ratio | **5.3×** |
| Total GC invocations (process-level GCs across pods) | 636 |

### SDK — per-service end-of-run counters

| service | spans_received | spans_dropped (%) | cp_received | cp_dropped (%) | lp_received | lp_dropped (%) | send_unavailable |
|---|---|---|---|---|---|---|---|
| **composepost** | 14.32 M | **10.2%** | 12.53 M | **7.0%** | 1.79 M | **32.1%** | 4 397 |
| **hometimeline** | 3.58 M | **31.9%** | 0 | – | 3.58 M | **31.9%** | 3 768 |
| **wrk2api** | 408 k | **18.7%** | 204 k | **6.0%** | 204 k | **31.4%** | 2 165 |
| media | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |
| post-storage | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |
| socialgraph | 1.90 M | 0% | 1.87 M | 0% | 38 k | 0% | 0 |
| text | 5.37 M | 0% | 0 | – | 5.37 M | 0% | 0 |
| uniqueid | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |
| urlshorten | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |
| userid | 75 k | 0% | 75 k | 0% | 0 | – | 0 |
| usermention | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |
| user | 1.79 M | 0% | 1.79 M | 0% | 962 | 0% | 0 |
| usertimeline | 1.79 M | 0% | 1.79 M | 0% | 0 | – | 0 |

### SDK — aggregates

| Metric | Value |
|---|---|
| Total spans emitted by SDKs | 39.20 M |
| Total spans handed to collector | 35.50 M |
| Total spans dropped at SDK | 2.67 M (~6.8%) |
| Total send_unavailable events | 10 330 |
| Total send_deadline events | 250 |
| Total send_exhausted / canceled / other | 0 |

### Priority discrimination

The bridges design's claim is that under memory pressure, CP (checkpoint)
spans survive better than LP (non-checkpoint) spans. Per-service drop
ratios:

| service | CP drop% | LP drop% | LP / CP ratio |
|---|---|---|---|
| composepost | 7.0% | 32.1% | **4.6×** |
| wrk2api | 6.0% | 31.4% | **5.2×** |
| hometimeline | n/a (no CP) | 31.9% | – |
| collector heavy pod (9qx7t) | 6.1% | 32.0% | **5.2×** |

**Pattern holds across all measurement points: LP gets shed ~5× more
often than CP under sustained pressure.** This is the bridges design's
target behavior — CP spans (the structurally interesting ones for
tracing) are preserved at the expense of fill-in LP spans.

## Comparison vs. prior runs

| | RAMP_RESULTS (v + ml) | RAMP_RESULTS_SB_V2 (sb + priority v2) | this run (sb + priority v3) |
|---|---|---|---|
| Date | 2026-06-02 | 2026-06-04 | 2026-06-08 |
| SDK | vanilla | SB | SB |
| Collector | memory_limiter | buffered priority processor | **pass-through priority** |
| otelcol resources | 1 CPU / 1 GiB | 1 CPU / 1 GiB | **500m / 256 MiB** |
| OOMKills | 0 | 0 | **0** |
| Throughput ceiling | 3.4k rps | 3.5k rps | 3.3k rps |
| Knee step | 3200 (154 ms) | 3400 (102 ms) | 3400 (145 ms) |
| Collector heavy pod CP drop% | – | 3.9% | **6.1%** |
| Collector heavy pod LP drop% | – | 3.9% | **32.0%** |
| LP / CP refusal ratio at collector | – | 1.0× (uniform) | **5.2×** |
| SDK heavy service drop% | 2.4% | 34.6% | 10.2% |

Key observations:

1. **Survival at ¼ the memory.** v2 with buffered priority needed 1 GiB
   to avoid OOM. v3 pass-through holds at 256 MiB because in-flight
   memory is bounded by sending_queue (the same mechanism vanilla's
   memory_limiter pipeline relies on).

2. **Priority discrimination is real.** v2 had identical CP/LP drop
   rates because all drops happened in the hard-pressure "refuse all"
   zone — the soft-pressure LP eviction never fired (drain capacity
   exceeded incoming). v3 actually shows the expected differential:
   under Low pressure the processor refuses LP only, under Mid
   refuses everything. LP gets shed first, CP gets preserved.

3. **Throughput unchanged.** The app saturates around 3.3–3.5k rps
   regardless of collector pipeline. The collector is not the
   throughput bottleneck in either configuration.

## Reproducing this run

```bash
# 1) Build and push otelcontribcol with v3 priority processor
cd opentelemetry-collector-contrib
./build-and-push.sh 10.10.1.1:30000   # uses sudo for docker daemon

# 2) Build and push the SB service images (each carries an embedded
#    copy of runtime/plugins/otelcol/sb_processor.go)
cd examples/dsb_sn/build_sbrev0/docker
for svc in $(ls -d *_service_sbrev0_ctr otelcol_sbrev0_ctr); do
  img=$(echo "$svc" | tr '_' '-')
  sudo docker build --platform linux/amd64 -t "10.10.1.1:30000/$img:latest" "./$svc"
  sudo docker push  "10.10.1.1:30000/$img:latest"
done

# 3) Deploy
kubectl apply -f ../../build_sbrev0/k8s/
kubectl patch service wrk2api-service-sbrev0-ctr -p '{"spec":{"type":"NodePort"}}'

# 4) Seed
cd ../../scripts
python3 init_social_graph.py --graph socfb-Reed98 --ip 10.10.1.1 --port "$NP"

# 5) Ramp
bash ../../utils/ramp_with_snapshots.sh sbrev0 ../../results_new_4/sbrev0_passthru 0
```

## Files / artifacts

- `opentelemetry-collector-contrib/processor/priorityprocessor/priority.go` — pass-through v3
- `opentelemetry-collector-contrib/processor/priorityprocessor/config.go` — Config struct (no NumConsumers/BatchSize)
- `opentelemetry-collector-contrib/test-config-bridges.yaml` — pipeline config
- `runtime/plugins/otelcol/sb_processor.go` — SDK with CP/LP partition + metadata header
- `examples/dsb_sn/results_new_4/sbrev0_passthru/` — summary.tsv, snapshots.tsv, ramp.log
