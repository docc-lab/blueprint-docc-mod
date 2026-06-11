# DSB-SN Vanilla Pipeline Ramp Results

Empirical characterization of the **vanilla** (no-bridge) DSB-SN pipeline under
load, with realistic resource constraints on the OTel collector. Establishes
the baseline behavior the bridge variants (PB/CG-PB/SB) are compared against.

Setup snapshot: **2026-06-02**, CloudLab `bridges-tb-02` testbed, target
identifier suffix `vrev0` (variant `v`, extra `rev0`).

## Hardware & Cluster

- **CloudLab profile**: docc-lab kubespray-based bridges testbed
- **10 nodes**: each Intel Xeon (40 CPU / 196 GiB RAM), 10 Gbps interconnect
- **Kubernetes v1.29.10** via kubespray (Calico CNI, kube-proxy, MetalLB)
- **CPU pinning**: every node clamped to 2.2 GHz via
  `utils/pin-cpu-22ghz.sh pin --all` (matches paper §5.1; intel_pstate or
  intel_cpufreq driver path, performance governor, no_turbo=1)
- **Local docker registry** at `10.10.1.1:30000` (NFS-backed PVC via
  nfs-subdir-external-provisioner)

## Software Configuration

### Blueprint runtime tuning

Set by `examples/dsb_sn/wiring/specs/docker.go` `init()`:

```go
const (
    DefaultGCIntervalSec = "0.1"   // override via env to "0.01" for paper-aligned runs
    DefaultGOGC          = "off"   // disable heap-driven GC; use only timer GC
)
```

For this run we exported `BLUEPRINT_GC_INTERVAL_SEC=0.01 BLUEPRINT_GOGC=off`
before wiring, so every Blueprint-built service container gets
`GC_INTERVAL_SEC=0.01` and `GOGC=off` in its env.

### Collector config (`opentelemetry-collector-contrib/config-vanilla.yaml`)

Aligned with `test-config-bridges.yaml` so vanilla vs bridges differs only in
the processor stage:

- `memory_limiter` first in pipeline (`limit_percentage: 80`,
  `spike_limit_percentage: 20`, `check_interval: 1s`)
- `batch` second (defaults — `send_batch_size: 8192`, `timeout: 200ms`)
- `otlp` exporter with `sending_queue.queue_size: 100000`,
  `block_on_overflow: false`, `retry_on_failure.max_elapsed_time: 0`

### Service-to-node pinning

`utils/pin_nodes.py` applies node-pinning yaml against `build_vrev0/k8s/`:

| Node   | Services (sum CPU req)                                        |
|--------|---------------------------------------------------------------|
| node-1 | composepost + hometimeline-cache + hometimeline-svc (19c)     |
| node-2 | social-cache + social-db + socialgraph (35c)                  |
| node-3 | uniqueid + usermention (7c)                                   |
| node-4 | usertimeline-cache + usertimeline-db + usertimeline-svc (21c) |
| node-5 | media + text + userid (16c)                                   |
| node-6 | post-cache + post-db + post-storage (10c)                     |
| node-7 | urlshorten-db + urlshorten-svc (7c)                           |
| node-8 | user-cache + user-db + user-svc (7c)                          |
| node-9 | jaeger (20c)                                                  |

`otelcol-vrev0-ctr` and `wrk2api-service-vrev0-ctr` run as DaemonSets across
all nodes (one pod per node).

### otelcol resource constraint

DaemonSet manifest edited directly (not in compose source — only for this
build, not persisted):

```yaml
resources:
  requests: { cpu: "1", memory: 1Gi }
  limits:   { cpu: "1", memory: 1Gi }
```

→ `memory_limiter` hard limit = 80% × 1Gi ≈ **819 MiB**; soft trigger at
**~615 MiB**. Real backpressure territory.

### Service routing

The `otelcol-vrev0-ctr` Service has `internalTrafficPolicy: Local` (set by
`d2k8s --daemon-services otelcol_vrev0_ctr`). Every app pod sends spans to
its **node-local** otelcol pod. The `wrk2api-service-vrev0-ctr` Service does
NOT have this policy (`--no-local-policy`) — wrk's connections distribute
across nodes via the default Cluster policy.

## Workload

- Seed: `init_social_graph.py` with `socfb-Reed98` (962 users, 37,624 follow
  edges)
- Warmup: **100 rps for 100 s** (c=1 / t=1) — cache-prime, NOT the ramp floor
- Ramp: **2000 → 4000 rps, step 200**, 60 s/step, 30 s break
- Workload: `compose-post.lua` against `POST /ComposePost` on the wrk2api NodePort
- wrk `-c` and `-t` derived: `C = ⌈rps² / 20000⌉`, `T = ⌈C/10⌉`

## Per-step latency

| step          | Mean      | p99        | rps achieved |
|---------------|----------:|-----------:|-------------:|
| Warmup 100    |   7.64 ms |    9.81 ms |        99.99 |
| 2000          |  26.56 ms |   77.12 ms |         1996 |
| 2200          |  35.55 ms |   79.23 ms |         2196 |
| 2400          |  38.43 ms |   77.44 ms |         2373 |
| 2600          |  38.80 ms |   79.23 ms |         2578 |
| 2800          |  54.88 ms |  100.42 ms |         2791 |
| 3000          |  50.85 ms |  119.81 ms |         2964 |
| **3200 (knee)** | **154.38 ms** | **744.45 ms** |       3124 |
| 3400          | 595.89 ms |     1.32 s |         3290 |
| 3600          |    1.44 s |     3.55 s |         3369 |
| 3800          |    2.37 s |     6.13 s |         3435 |
| 4000          |    5.44 s |    11.63 s |         3308 |

**Throughput ceiling: ~3.4k rps effective** (achievable load before
congestion collapse).

## Drop accounting

### otelcol-side (all 9 daemonset pods, cumulative across ramp)

`exporter_sent_spans` is filtered to `exporter="otlp"` (the canonical
downstream to Jaeger). The pipeline also has a `debug` exporter on the
same path that increments its own per-exporter counter — including both
would double every span. `dump_otelcol_drops.sh` filters via the optional
`scrape_metric ... 'exporter="otlp"'` arg as of the 2026-06-02 fix.

| pod (node)          | receiver_refused | exporter_sent (otlp) |
|---------------------|-----------------:|---------------------:|
| 55kd7 (node-6)      |                0 |            2,331,008 |
| 5d7xm (node-2)      |                0 |            2,445,622 |
| gqc77 (node-7)      |                0 |            2,330,574 |
| hqnwl (node-5)      |                0 |            8,124,240 |
| l9hmz (node-9)      |                0 |              429,178 |
| **r8pzw (node-1)**  |     **460,091**  |       **14,619,406** |
| rb6q6 (node-3)      |                0 |            4,231,620 |
| vprts (node-4)      |                0 |            2,328,334 |
| zc865 (node-8)      |                0 |            2,332,994 |
| **TOTAL**           |     **460,091**  |       **39,172,976** |

- `receiver_failed_spans`: 0 (no `exporter` dimension — accurate as-is)
- `exporter_send_failed_spans` (otlp only): 0

### SDK-side (composepost service only — central trace fanout hub)

```
spans_received   = 15,211,392
spans_flushed    = 15,211,392
spans_sent       = 14,852,679
spans_dropped    =    358,713
batches_sent     =     60,485
batches_dropped  =      1,141
send_deadline    =          0
send_unavailable =      1,141    ← every drop categorized as gRPC Unavailable
send_exhausted   =          0
send_canceled    =          0
send_other       =          0
buffer_depth     =          0    ← buffer drains in-tick, no SDK-side queueing
```

### wrk-side (application-level HTTP errors)

- **`Non-2xx or 3xx responses`: 0 across the entire ramp.** No HTTP 5xx
  surfaced to wrk; saturation manifested purely as latency degradation.

## Where in the pipeline things are being lost

Following the OTel pipeline from span emission backward:

1. **App SDK (vanilla_processor.go)**: receives every span via OnEnd, buffers
   internally, flushes every 100 ms in BatchSize=512 chunks. **No internal
   queue overflow**: `buffer_depth` is 0 at every tick because the flush
   tick drains the buffer entirely. Spans only "drop" here when the
   downstream OTLP UploadTraces returns a permanent error.

2. **App-side OTLP exporter → otelcol gRPC OTLP receiver**: the SDK's
   `client.UploadTraces` returns `gRPC Unavailable` when the receiver's
   `memory_limiter` says "I'm above the spike limit, refusing batches".
   The vanilla processor counts this as `send_unavailable` and counts the
   spans in the rejected batch as `spans_dropped`. **All 358,713 composepost
   drops came through this path** (matches `send_unavailable=1141` × ~314
   spans/batch).

3. **otelcol `memory_limiter` processor**: when RSS > spike trigger
   (~615 MiB on a 1Gi limit), increments `receiver_refused_spans` and
   returns ResourceExhausted/Unavailable to the gRPC server, which the
   receiver translates to a refusal returned to the SDK. **All 460,091
   refused spans on node-1's otelcol pod (`r8pzw`)** are this case.

4. **otelcol `batch` processor**: no drops (it buffers up to 8192 spans for
   200 ms then flushes whatever it has).

5. **otelcol `otlp` exporter → Jaeger**: `sending_queue` is 100,000 deep
   with `block_on_overflow: false`. If the queue filled, we'd see
   `exporter_enqueue_failed_spans` and ultimately `exporter_send_failed_spans`.
   **Zero across the ramp** — Jaeger absorbed everything the collectors
   could push.

### The bottleneck: node-1's otelcol pod

**All 460,091 receiver_refused spans landed on one pod** (`r8pzw`, the
otelcol daemonset member on node-1). The other 8 collectors are idle by
comparison: `<5M sent` each, vs. `29.2M sent` on `r8pzw`. Caused by the
combination of:

- **Service routing**: `internalTrafficPolicy: Local` on
  `otelcol-vrev0-ctr` forces every app pod's spans to the node-local
  otelcol pod.
- **App-to-node assignment**: `composepost` (the central trace fanout —
  every ComposePost call spawns ~10 downstream gRPC spans plus the inbound
  HTTP span and the immediate user/social/timeline writes) lives on node-1.
  Whatever rate composepost can serve, the node-1 otelcol must absorb the
  full span fanout at that rate. With a 1 CPU / 1 GiB cap on the
  collector, it gets crushed first while the other 8 stay healthy.

### Cascading effect on the application

The SDK's OTLP gRPC client blocks while UploadTraces is in flight. When the
node-1 collector returns Unavailable, the SDK retries (briefly — until the
1s `context.WithTimeout` in `vanilla_processor.sendData` fires) and only
then drops. That ~1 s of blocked retry per failed batch slows down the
goroutines doing trace span finalize, which ripples into the request hot
path on composepost specifically. Result: **composepost p99 latency
explodes at the same rps where node-1's otelcol starts refusing**, dragging
the whole request fanout with it. The other services on other nodes are
fine — their otelcols are idle.

## Comparison: unpinned vs. pinned (same 1cpu/1Gi otelcol constraint)

| Metric                          | Unpinned          | Pinned             | Δ          |
|---------------------------------|------------------:|-------------------:|-----------:|
| Knee step                       | rps=3000 (1.58s)  | rps=3200 (154 ms)  | +200 rps   |
| Mean @ rps=3000                 | 1.58 s            | 50.85 ms           | −97%       |
| p99 @ rps=3000                  | 3.91 s            | 119.81 ms          | −97%       |
| Mean @ rps=4000                 | 11.10 s           | 5.44 s             | −51%       |
| p99 @ rps=4000                  | 20.55 s           | 11.63 s            | −43%       |
| Actual throughput @ rps=4000    | 2611              | 3308               | +27%       |
| Total otelcol receiver_refused  | 766,401           | 460,091            | −40%       |
| Composepost SDK spans_dropped   | 545,765           | 358,713            | −34%       |

Pinning lifts the knee by ~200 rps AND reduces drops by ~35-40% at the
same load, even though the otelcol constraint is identical. The effect is
indirect: pinning gives each service a stable, predictable CPU envelope
which keeps per-service tail latency tight, which keeps per-second span
generation rate from spiking irregularly, which gives the (still
constrained) collector enough time between bursts to drain its
sending_queue and not OOM.

## Reproducing this run

```bash
# 0. fresh kubespray cluster + nfs/registry + cpu pin already done
bash /local/repository/setup-kubespray.sh        # if needed
bash /local/repository/setup-kubernetes-extra.sh # nfs provisioner + registry
bash utils/pin-cpu-22ghz.sh pin --all

# 1. registry deployment
kubectl create namespace registry
kubectl apply -f utils/registry-pvc.yaml -f utils/registry-deployment.yaml -f utils/registry-service.yaml

# 2. build & push otelcontribcol (must precede d2k8s — base image for the otelcol service)
cd /users/tomislav/opentelemetry-collector-contrib
sg docker -c './build-and-push.sh 10.10.1.1:30000'

# 3. wire with paper-aligned GC tuning
cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn/wiring
rm -rf ../build_vrev0
BLUEPRINT_GC_INTERVAL_SEC=0.01 BLUEPRINT_GOGC=off go run main.go -w docker_v -extra rev0 -o ../build_vrev0

# 4. d2k8s build + push all app images + k8s manifests
cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn
cp build_vrev0/.local.env build_vrev0/docker/.env
sg docker -c 'set -a; . build_vrev0/docker/.env; set +a; python3 /users/tomislav/blueprint-docc-mod/d2k8s/d2k8s.py --registry 10.10.1.1:30000 --daemon-services otelcol_vrev0_ctr,wrk2api_service_vrev0_ctr --no-local-policy wrk2api_service_vrev0_ctr build_vrev0/docker/docker-compose.yml build_vrev0/k8s'

# 5. apply node pinning
python3 /users/tomislav/blueprint-docc-mod/utils/pin_nodes.py node-pinning-vrev0.yaml build_vrev0/k8s

# 6. otelcol resource limits (manual edit of build_vrev0/k8s/otelcol-vrev0-ctr-daemonset.yaml)
# add resources: { requests: { cpu: "1", memory: 1Gi }, limits: { cpu: "1", memory: 1Gi } }

# 7. fire the ramp
PYTHON3=/users/tomislav/.venvs/claude/bin/python3 bash /users/tomislav/blueprint-docc-mod/utils/run_dsb_sn_ramp.sh \
  --target vrev0 --start 2000 --end 4000 --step 200

# 8. observability (in parallel)
utils/dump_otelcol_drops.sh --watch 1                # otelcol drops, every 1s
kubectl logs -f deploy/composepost-service-vrev0-ctr | grep vanilla_processor_metrics   # SDK drops, every 1s
```

## Files / artifacts referenced

- `runtime/plugins/otelcol/vanilla_processor.go` — SDK counters
- `utils/dump_otelcol_drops.sh` — collector drop scraper (port-forwards
  each pod's `:8888/metrics`)
- `utils/run_dsb_sn_ramp.sh` — generic ramp runner (start/end/step args)
- `utils/pin-cpu-22ghz.sh` — CPU freq pinning, intel_pstate and
  intel_cpufreq paths
- `utils/pin_nodes.py` — node-affinity + resource injection from yaml
- `examples/dsb_sn/node-pinning-vrev0.yaml` — per-service node + cpu req
- `examples/dsb_sn/build_vrev0/k8s/otelcol-vrev0-ctr-daemonset.yaml` —
  manually-added resource limits
- `opentelemetry-collector-contrib/config-vanilla.yaml` — collector config
