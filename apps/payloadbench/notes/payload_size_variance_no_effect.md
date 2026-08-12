# Payload-size *spread* does not measurably affect response-time variance at 1 KB scale — and the saturation ceiling is a BYTE-rate ceiling

**Date:** 2026-07-29 → 07-30
**App:** `apps/payloadbench` (nt_es = untraced), edge on node-2 / internal on node-3,
8-core CPU limits + `GOMAXPROCS=8`, wrk2 from node-0 to the edge NodePort on node-2.
**Protocol:** 4 campaigns × 5 rounds × (2000→62000 rps, step 2000, 30 s/step, 10 s break,
60 s warmup @500 rps per round). ~160M requests total, **zero non-2xx in any campaign**.
**Data:** `apps/payloadbench/results/ramp_nt_es_{1k1k,u800_1200,f1000,exp1000}_n5_*`
(full per-step wrk2 HdrHistograms + `aggregate.txt`).

## Question

Does a wide spread of inter-service payload sizes make response time *more variable*
than a fixed payload size, and if so by how much?

## Design (and the confound that forced a 3rd campaign)

| campaign | size distribution | mean | CV_size |
|---|---|---|---|
| `1k1k`      | constant 1024 B      | 1024 | 0 |
| `u800_1200` | U[800, 1200]         | 1000 | 0.115 |
| `f1000`     | constant 1000 B      | 1000 | 0 |
| `exp1000`   | Exp(mean 1000), uncapped | 1000 | **1.00** |

The first comparison attempted was `u800_1200` vs `1k1k` — **invalid**, see the confound
below. `f1000` was added as the matched-mean control, and `exp1000` raises CV_size by ~9×
over uniform so the comparison becomes a **dose-response** rather than a single contrast:
if spread causes response-time variance, CV(response) must rise across 0 → 0.115 → 1.00.

## Result 1 (headline): NO effect, with a tight bound

Paired-by-step test on CV = sd/mean of response time (per-round CV, averaged over 5
rounds, then paired across the 22 stable steps 2k–44k; base CV_fix = 0.679):

| contrast | ΔCV | 95% CI | verdict |
|---|---|---|---|
| uniform (CV_size 0.115) vs fixed | **+1.33%** | [−1.37%, +4.03%] | not significant (t=1.03) |
| exponential (CV_size 1.00) vs fixed | **+1.55%** | [−0.89%, +3.99%] | not significant (t=1.32) |
| exponential vs uniform | +0.21% | — | not significant (t=0.15) |

**The dose-response is flat.** A ~9× increase in payload-size CV (0.115 → 1.00) produces
no additional response-time variance (+0.21%, n.s.). That absence of dose-response is the
strong evidence: if spread were causal, exp would show ~9× uniform's effect.

Both point estimates are positive and the sign counts lean positive (uniform higher at
14/22 steps, exp at 16/22), so a **very small real effect of order +1–2% cannot be
excluded** — but it is bounded above by ~+4% (95%).

Knee region (46k–56k, 6 steps) gives larger point estimates (+3.9% uniform, +5.7% exp)
but they are **not significant and not internally consistent** (exp is higher at only 2 of
6 steps while its band average is higher — i.e. driven by one or two outlier steps).
Treat as noise; between-round sd is 3–12 ms there.

### Why this is the expected outcome

Response-time CV sits at ~0.68 for *every* configuration, i.e. variance is dominated by
scheduling/queueing jitter, not payload. For size spread to matter, service time must
track bytes — but at 1 KB the per-request cost is mostly fixed (syscalls, goroutine
scheduling, HTTP/2 framing, protobuf setup), so only a small fraction of service time
inherits the size distribution. M/G/1 predicts mean wait ∝ (1+CV_s²)/2, and CV_s² barely
moves when only a small share of S scales with size.

**Scope of the negative result:** it applies to ~1 KB payloads. At larger sizes the
per-byte term dominates (see Result 2), so spread could matter there — untested.

## Result 2: the ceiling is bytes/sec, not requests/sec

Achieved throughput at 62k offered, and the same number in bytes:

| campaign | mean size | achieved rps | byte rate |
|---|---|---|---|
| `1k1k`      | 1024 B | 58 733 | 60.14 MB/s |
| `u800_1200` | 1000 B | 60 292 | 60.29 MB/s |
| `f1000`     | 1000 B | 60 815 | 60.81 MB/s |
| `exp1000`   | 1000 B | 60 522 | 60.52 MB/s |

Request-rate ceilings differ by up to 3.5%, but **byte rates agree within 1.1%** — and the
1024 B run's 2.34% smaller request ceiling is exactly its 2.34% larger payload. So the
bottleneck is a per-byte cost, ~60.1–60.8 MB/s per direction.

**It is not the network:** NICs on node-2/node-3 are 10 GbE and 60.5 MB/s = 481 Mbps ≈ 4.8%
of line rate. It is per-byte CPU work in the edge service, which is also the pod that
saturates first (6.4 of 8 cores at 50k rps while internal sits at ~2–3 of 8).

### CONFOUND THIS CREATES (the reason `f1000` exists)

Comparing size distributions with **different means** against a byte-rate-limited
bottleneck is invalid near saturation. U[800,1200] (mean 1000) vs fixed 1024 differs by
2.34% in bytes → 2.34% lower utilisation at every matched *request* rate → amplified by
ρ/(1−ρ) into an apparent **−35% stddev and −20…35% mean latency** for uniform. That
artifact vanished entirely once compared against `f1000` at matched mean (throughputs then
agree within 0.5% at every step). Its tell was that **CV was flat while absolute sd moved**
— a scale change, not a shape change.

> **Rule for future payload work (incl. bridges, whose baggage adds bytes/request):
> the comparable x-axis is MB/s, not rps.** Two workloads at the same request rate but
> different mean sizes sit at different utilisations, and near saturation that difference
> is amplified ~10–15×.

## Method notes worth keeping

- **Averaging stddevs across rounds is wrong.** Two different quantities are reported:
  (a) *between-round* sd of each per-round statistic = the error bar for comparisons;
  (b) *pooled* request-level sd via the law of total variance,
  `var = Σ n_r(sd_r² + (μ_r − μ_w)²) / Σ n_r`, which keeps the between-round mean shift a
  plain average of variances would drop. See `aggregate_rounds.py`.
- **Within-window request-level sd is not an error bar** — requests inside a 30 s window
  are autocorrelated (GC cadence, bursts); it describes distribution *shape*, not
  uncertainty of the mean. Error bars require independent rounds.
- **10 s steps are too short**: wrk2 spends ~10 s calibrating, so recorded percentiles come
  from what's left. Early 10 s probes reported a ~4% offered/achieved shortfall that fell to
  ~1.3% at 30 s — a pure calibration artifact. 30 s/step is the minimum used here.
- **Missing distribution parameters fail silently in `payload.lua`** (`numenv()` falls back
  to 0 → zero-byte payloads for a whole campaign). `run_ramp.sh` now has a pre-flight guard
  that hard-errors on e.g. `--req-dist exp` without `--req-mean`.
- **Zero non-2xx is a correctness proof, not just a health check**: `EdgeService.Call`
  verifies `len(resp) == resSize` per request and 500s on mismatch, so 0 errors across
  ~160M requests means every independently sampled size round-tripped byte-exact.
- **Interrupted campaigns**: `run_ramp.sh --out <dir> --round-start N` resumes in place.
  A partially-complete round must be **discarded**, never aggregated (it would weight
  low-RPS steps differently from high ones).

## CORRECTIONS (2026-07-30, after the campaigns)

Two problems found afterwards. **Both results above should be treated as provisional.**

### 1. All four campaigns ran with FORCED GC every 100 ms (confound for the variance metric)

The wiring spec bakes in `BLUEPRINT_GOGC=off` + `BLUEPRINT_GC_INTERVAL_SEC=0.1`, and
`inject_perf_env.py` was run **without** `--gc`, so every campaign had `GOGC=off,
GC_INTERVAL_SEC=0.1` on both services — a forced GC stall 10× per second. That is a
periodic latency stall which plausibly **dominates the response-time distribution and
swamps any payload-size effect**, and it neatly explains why CV was pinned at ~0.68
regardless of size distribution.

First natural-GC measurements (`GOGC=100, GC_INTERVAL_SEC=0`, fixed 1000 B, n=1) vs the
forced-GC n=5 values at the same rates:

| offered | CV natural (n=1) | CV forced (n=5) |
|---|---|---|
| 10 000 | 0.54 | 0.70 |
| 30 000 | 0.62 | 0.68 |
| 50 000 | 0.44 | 0.62 |

CV is **lower under natural GC at all three rates**, i.e. forced GC was inflating the
noise floor the size-spread effect had to clear. **The three-way size-distribution
comparison should be re-run under natural GC** before the null result is trusted.
(Throughput capacity is essentially unaffected: 59 665 natural vs 59 293 forced.)

### 2. The "byte-rate ceiling" interpretation is NOT established

The measurement stands (byte rates agree within 1.1% while request ceilings differ 3.5%),
but the *mechanism* attributed to it — per-byte CPU in the edge service — is contradicted
by direct CPU sampling: in controlled capacity probes the edge used only **3.4–5.4 of its
8 cores**, i.e. it was never CPU-saturated. Confirming this, **raising both services to 16
cores made throughput WORSE, reproducibly** (see table below), which cannot happen if CPU
were the binding resource.

Capacity (offer 90k, achieved == capacity, fixed 1000 B, 20 s):

| cores | client | achieved |
|---|---|---|
| 8  | 16thr/1024conn | **59 293** |
| 8  | 24thr/2048conn | 56 370 |
| 16 | 16thr/1024conn | 57 076 |
| 16 | 24thr/2048conn | 54 933 |

(Also note 2048 connections costs ~3k rps at both core counts — client concurrency must be
held fixed when comparing.)

Ruled out as the cap:
- **Network**: `enp94s0f0` (the iface holding the k8s InternalIP, *not* the default-route
  `eno1` an earlier check wrongly read) is 10 GbE; iperf3 measures 9.40 Gbit/s bulk and
  **1.70 Gbit/s with 1 KB messages**, vs our 0.48 Gbit/s per direction. Not binding, though
  the margin vs the small-message path (~3.5×) is far smaller than a "5% of line rate"
  framing suggests. Calico also runs a **VXLAN overlay**, so encap/decap is in the path.
- **gRPC stream limits**: grpc-go v1.82.1 defaults `maxConcurrentStreams` to
  `math.MaxUint32` (unlimited), so this is not it.

Leading untested hypotheses for the ~59–61k / ~60 MB/s cap:
1. **Single HTTP/2 connection edge→internal.** The generated client does one `grpc.Dial`
   per service, so all traffic rides one ClientConn whose transport writer goroutine
   serializes framing work. This fits every observation: not CPU-bound, no benefit from
   more cores, and throughput falling as bytes/request rises. Testable by applying
   `clientpool.Create(spec, internal_service, N)` — the runtime builds N independent
   clients (N `grpc.Dial`s ⇒ N TCP connections); pick N large (≥256) since the pool also
   caps concurrency at N.
2. **Retry amplification at saturation.** `retries.AddRetries(..., 3)` with the generated
   client's 1 s timeout: once queueing latency exceeds 1 s, requests time out and are
   retried up to 3×, tripling offered work while still reporting 2xx. Would produce a soft,
   self-limiting ceiling.

## Result 3: client-side connection pooling HURTS — do not add it to RPC paths

Tested because the single shared gRPC ClientConn was suspected of serializing framing
work (hypothesis 1 above). `clientpool.Create(spec, internal_service, N)` builds N clients,
each in its own derived namespace running the full client chain — so each performs its own
`grpc.Dial` ⇒ **N real TCP connections** (verified per-config by counting established
sockets to the internal ClusterIP from inside the edge pod's netns via `nsenter`, not
inferred from codegen).

Capacity (offer 90k, achieved == capacity, fixed 1000 B, natural GC, 8 cores):

| pool N | conns verified | achieved rps | implied service time (Little's law) | edge CPU |
|---|---|---|---|---|
| **0 (no pool)** | 1 | **56 360** / 59 665 (two deploys) | — | — |
| 4   | 4   | 11 867 | 0.34 ms | — |
| 16  | 16  | 36 299 | 0.44 ms | 0.85 cores |
| 64  | 64  | 42 603 | 1.50 ms | 4.3 cores |
| 256 | 256 | 44 329 | 5.8 ms  | — |

**No pool size beats no-pool.** Small N is concurrency-capped (the pool caps outstanding
calls at N, so throughput = N / service-time). Large N stops being capped but plateaus
~25% *below* the single shared connection, and burns far more CPU: N=64 used 4.3 cores for
42.6k rps vs N=16's 0.85 cores for 36.3k — ~5× the CPU for 17% more throughput, consistent
with per-connection transport goroutines and the loss of write coalescing on one connection.

**This re-derives an earlier project decision.** dsb_sn's specs apply `clientpool.Create`
only in `applyHTTPDefaults` (the wrk2api frontend — effectively inert, since wrk drives it
externally over HTTP) and never in `applyDockerDefaults`, the gRPC inter-service path. That
choice was undocumented; these numbers are the justification. **Do not add clientpool to
RPC paths.** In `apps/payloadbench` it stays available but off by default behind
`-internal-pool N` (0 = no pool) for exactly this kind of investigation.

Useful byproduct: the small-N points give the edge→internal service time at low
concurrency — **~0.34 ms** — which saturated runs cannot reveal. This is what makes the
remaining puzzle sharp (see below).

## grpc-go's 100-concurrent-stream trap: REAL, fixed, but NOT this app's bottleneck
## (it almost certainly IS dsb_sn's)

Found while hunting the ceiling, verified in grpc-go v1.82.1 source:

- `internal/transport/defaults.go`: `defaultMaxStreamsClient = 100` initialises the
  client's `streamQuota`; `http2_client.go newStream` **blocks** on
  `streamsQuotaAvailable` when it hits 0.
- `internal/transport/http2_server.go`: the server sends
  `SETTINGS_MAX_CONCURRENT_STREAMS` **only if** its value `!= math.MaxUint32`.
- `server.go:189`: the default **is** `math.MaxUint32`.
- Blueprint generated `grpc.NewServer()` with no options.

⇒ "unlimited" on the server means **no SETTINGS frame is ever sent**, so every client
stays pinned at **100 concurrent in-flight RPCs per connection**, with the rest queued
client-side — exactly as <https://grpc.io/docs/guides/performance/> describes. The
default is a trap: setting a limit explicitly is what *removes* the limit.

**Fixed** in `plugins/grpc/grpccodegen/servergen.go` — `grpc.MaxConcurrentStreams(8192)`,
overridable via `GRPC_MAX_CONCURRENT_STREAMS` (0 restores grpc-go's behaviour). This is a
**repo-wide** change: every Blueprint gRPC service inherits it.

**It did NOT lift payloadbench's ceiling** (measured, at 8 cores: 54.4k/54.0k/52.5k/48.3k
at c=128/256/512/1024 — unchanged within noise; at 16 cores it was *worse*, ~46k). Reason,
and the arithmetic error behind the hypothesis: in-flight streams are **throughput ×
per-call latency**, not client connection count — 56 000 × 0.34 ms ≈ **19 streams**, never
near 100. The 1024 client requests queue in the edge waiting for **CPU**, not for stream
quota. (The earlier "100 / 1.77 ms = 56.5k ≈ measured 56.4k" was numerology: the CPU model
predicts the same number, 8 cores × 87% / 122 µs ≈ 57k.)

**But it should bind dsb_sn**, whose services are ~100× slower per call:

| service | in-flight = rps × latency | vs 100-stream cap |
|---|---|---|
| payloadbench (0.34 ms) | 19 | does not bind |
| dsb_sn @ 5k rps (~30 ms) | 150 | **BINDS** |
| dsb_sn @ 10k rps (~30 ms) | 300 | **BINDS** |

⇒ **Testable prediction: dsb_sn services should gain throughput from this fix**, and any
prior dsb_sn campaign driven above ~100 concurrent in-flight RPCs per service pair was
partly measuring client-side stream queueing. Worth re-checking a bridges ramp.

## MECHANISM (pprof block/mutex/goroutine): per-request CPU cost that grows with
## parallelism — NOT a lock, NOT a fixed-rate serial resource

"Per-runtime limit" was a label, not a mechanism. perf only shows where CPU is *spent*;
a bottleneck that *waits* is invisible to it. `workflow/payloadbench/pprof.go` adds a
gated pprof endpoint (`PPROF_ADDR`, off by default; block/mutex rates separately gated
because they perturb measurements) — see `diagnose_block2.sh`. Under 90k offered load:

- **Goroutines**: 978 of 1038 parked in `google.golang.org/grpc/internal/transport`.
- **Block profile**: **100% of blocked time is `runtime.selectgo`** on
  `EdgeService.Call → retries.Echo → InternalService_GRPCClient.Echo →
  ClientConn.Invoke → clientStream.RecvMsg → select` — i.e. simply awaiting the RPC
  response. The edge is not self-limited.
- **Mutex profile**: **29 s total vs 66 029 s of waiting (0.04%)** ⇒ **lock contention is
  positively ruled out.**

### Throughput vs client concurrency (the decisive shape)

| client conns | achieved rps | implied RTT (conns/rps) |
|---|---|---|
| 32 | 43 810 | 0.73 ms |
| 64 | 50 227 | 1.27 ms |
| **128** | **56 428 (peak)** | 2.27 ms |
| 256 | 54 416 | 4.70 ms |
| 512 | 51 799 | 9.88 ms |
| 1024 | 48 707 | 21.02 ms |

A fixed-rate serial resource would hold throughput **flat** as concurrency rises.
Throughput instead **peaks at ~128 conns and then falls** ⇒ contention that *grows with
concurrency*, not a queue behind a serial server.

**⚠️ All campaigns in this note ran `-c 1024`, i.e. ~14% below peak throughput with 9×
inflated latency (2.3 ms → 21 ms) from self-inflicted queueing. Use `-t 16 -c 128` for
future runs.**

### So what is it?

Per-request cost is **~122 µs of CPU for a 2 KB round trip** — 6–10× a tuned Go gRPC echo
— and that cost *grows with parallelism*:

| | 8 cores | 16 cores |
|---|---|---|
| CPU used | 7.12 (89% of limit) | 8.96 (56% of limit) |
| achieved | ~58k | ~56k |

More cores ⇒ **more CPU consumed, less throughput**, because added Ps buy scheduler churn
(`stealWork` +5.7×, `procyield` +37%) while useful gRPC/proto work shrinks 14.6%→12.5%.
Multiple *processes* scale (below) because each has its own scheduler and GC domain
instead of more Ps contending inside one.

**The lever is the 122 µs**, and the profile ranks the targets: ~10% kube-proxy
nftables/conntrack per packet (bypassable via host networking / direct pod IPs), ~13% Go
alloc+GC (buffer reuse; also relevant to the GC question), ~14% gRPC/protobuf/HTTP2, ~47%
long tail. `time.Time.appendFormat` at 0.6% suggests per-request timestamp formatting
(logging?) in a *no-tracing* build — worth chasing.

## Process scaling: throughput scales with PROCESSES, not cores or connections

Decisive experiment — hold total CPU limit constant and vary the number of edge
*processes* (`kubectl scale`, single-replica vs multi-replica, identical image):

| config | total CPU limit | achieved rps | vs 1 process |
|---|---|---|---|
| 1 process × 16 cores | 16 | 46 251 | — |
| **2 processes × 8 cores** | **16 (identical)** | **74 280** | **+61%** |
| 4 processes × 8 cores | 32 | 129 191 | +179% |

**At identical total CPU, two Go processes beat one by 61%, and throughput scales
near-linearly with process count** (129k at 4 procs — >2× the "byte-rate ceiling" of
~60 MB/s that a single process could never exceed). So the wall is **per Go runtime**
(scheduler + GC domain), not CPU, network, client, or connection structure.

Consistent with everything else observed:
- more cores in ONE runtime does nothing (`stealWork` +5.7×, `procyield` +37%, useful
  gRPC/proto work *shrinks* from 14.6%→12.5%) — added Ps buy scheduler churn;
- more *connections* in one runtime (clientpool, any N) does nothing or hurts — which
  also **retires the single-`loopyWriter` hypothesis**: throughput scales with runtimes,
  not connections;
- two independent load-generator hosts add nothing (+0.7%) — not the client;
- at 16 cores neither pod is saturated (edge 56%, internal 23%, zero throttling) yet
  throughput is flat.

### Consequences for the earlier results in this note

- The "~58–60k ceiling" and the knee at 46–58k characterise **the Go runtime's
  per-process throughput limit, not the application or the payload**. Every
  near-saturation comparison in the campaigns above is therefore measuring that limit.
- **Result 2's byte-rate ceiling is not a byte-rate law.** It rested on a 2.4% payload
  difference (1000 vs 1024 B) producing a 2–3.5% rps difference — inside the ~7%
  deploy-to-deploy variance — and a single process is capped regardless. A proper test
  needs a 2× payload lever (1000 vs 2000 B); pending.
- **Keep experiments single-replica and well below the knee (≲40k rps).** Multi-replica
  raises the ceiling but changes the system under test (separate GC domains per pod,
  requests split across pods) and diverges from dsb_sn's single-process-per-service
  topology. The replica sweep above is a *diagnostic*, not a recommended config.
- Per-request cost is ~122 µs of CPU for a 2 KB round trip — 6–10× a tuned Go gRPC echo
  — so there is large headroom, but it is spread thin (see the profile below), not
  sitting behind one lock.

## Where the cycles actually go (perf, 8 cores, 174k samples)

Split **67.8% user / 31.7% kernel**. `memmove` is only ~1.2–1.8%, so **payload copying is
negligible** and the cost is per-*request*, not per-byte.

| bucket | 8 cores | 16 cores |
|---|---|---|
| netfilter / conntrack / kube-proxy (`nft_do_chain`, `comment_mt`, …) | 9.7% | 11.2% |
| alloc + GC (`mallocgcSmallScanNoHeader`, `tryDeferToSpanScan`, …) | 13.8% | 12.2% |
| gRPC / protobuf / HTTP2 (useful work) | 14.6% | 12.5% |
| syscall / net / kernel | 8.0% | 9.1% |
| lock / spin contention | 3.8% | 4.2% |
| Go scheduler | 1.3% | 2.2% |
| long tail (~1900 symbols) | ~47% | ~46% |

Notable: **~10% of all CPU is kube-proxy's nftables/conntrack rule evaluation** — pure
infrastructure tax per packet. Also `time.Time.appendFormat` at 0.60% suggests per-request
timestamp formatting (logging?) in a *no-tracing* build — worth chasing separately.

### perf-in-containers recipe (both traps cost a wasted run)

1. `kernel.perf_event_paranoid=-1` and `kptr_restrict=0` on the node (default here was
   paranoid=**4**, which yields zero samples).
2. Find the process by **most threads**: `crictl inspect`'s first `"pid"` is the
   shim/sandbox (1 thread, idle → 0 samples), and after a rollout the **old dying**
   process still matches by name (profiling it gives ~200 teardown samples:
   `zap_pte_range`, `free_unref_page_list`). See `profile_edge.sh`.
3. `perf record -F 997 --call-graph fp -p <pid> -- sleep N`; sanity-check the sample count
   (expect ~10^5, not ~10^2) before trusting any aggregation.

## The edge service is CPU-bound at 8 cores (~89%) — `kubectl top` was lying

`kubectl top pod` reported the edge at 0.85–5.7 cores of 8 all day, which drove a long
(wrong) hunt for a non-CPU bottleneck. metrics-server averages over a long window and lags;
the authoritative source is the pod's cgroup `cpu.stat`. Measured under load:

```
usage_usec  71229699   over ~10 s wall  =>  7.12 of 8 cores  (89%)
nr_periods  101   nr_throttled 0   throttled_usec 0      cpu.max: 800000 100000
```

**No CFS throttling, but ~89% CPU utilisation.** The edge service is the binding resource
after all. **Always use cgroup `cpu.stat` (or `nr_throttled`) for this, never `kubectl top`.**

This *restores* the Result-2 byte-rate interpretation with direct evidence:
7.12 cores ÷ 58 450 rps = **122 µs CPU/request** ≈ 61 ns per byte moved (2 KB round trip).
If per-request CPU is dominated by a per-byte term, max rps ∝ 1/bytes ⇒ a constant byte
rate — exactly what was measured across 1000 B and 1024 B payloads.

### Correction: "16 cores is worse" was NOT established

59 293 (8 cores) → 57 076 (16 cores) is −3.7%, inside the ~7% deploy-to-deploy variance
measured later (55 612 / 56 360 / 59 665 for *identical* no-pool code). The defensible
claim is weaker and more interesting: doubling the limit produced **no throughput gain**
while consuming **more** CPU (8.9 cores at the 16-core limit vs 7.12 at the 8-core limit) —
the signature of contention, where added parallelism burns cycles without producing work.
So CPU is binding *and* the service does not scale with cores; the serialization point
(plausibly the single HTTP/2 transport writer goroutine on the shared ClientConn, plus
forced-GC interaction) is what to investigate next, not the core count.

### Also eliminated (each by direct measurement)

- **Load generator**: two clients on separate nodes (node-0 + node-4, 45k each) aggregate to
  **57 756 rps** vs **57 253** from one client — identical within 0.9%. wrk2 was never the cap.
- **Retry amplification**: `-retries 0` gives 53 037 rps, i.e. no improvement (slightly lower,
  within variance). Not the cause.
- **Pool concurrency cap (the earlier confound)**: at `-internal-pool 2048` the pool grew to
  **1024 connections** (lazily, matching client concurrency) so the cap could not bind, and
  throughput was still **41 424 rps, ~28% below no-pool**, with 5.7 cores burned. Connection
  parallelism genuinely hurts; Result 3's conclusion stands, now without the confound.

## Superseded: the ceiling was UNEXPLAINED at this point in the investigation

With no pool, in-flight requests are bounded only by the client's 1024 connections, so
~58k rps implies ~17.6 ms mean latency — versus a measured **0.34 ms** service time at low
concurrency. A ~50× gap with no saturated server resource. Eliminated by direct
measurement, not inference:

| candidate | evidence against |
|---|---|
| service CPU limit | edge used 0.85–4.3 of 8 cores; **raising to 16 cores made throughput worse** |
| network bandwidth | iperf3: 9.40 Gbit/s bulk, **1.70 Gbit/s at 1 KB messages**, vs our 0.48 Gbit/s per direction |
| softirq / VXLAN / conntrack | busiest core 31% `%soft`, spread across cores; nothing pegged |
| gRPC stream limits | grpc-go v1.82.1 defaults `maxConcurrentStreams` = `math.MaxUint32` |
| single HTTP/2 connection | refuted: 256 connections was 25% *worse* (Result 3) |

Leading remaining suspects, untested:
1. **The load generator.** wrk2 sat at ~3 of 40 cores, and going to 24 threads / 2048
   connections made throughput *worse* (56.4k vs 59.3k) — client-side behaviour, not
   server. Decisive test: drive the same deployment from **two nodes at once** and see
   whether aggregate throughput exceeds ~60k.
2. **Retry amplification.** `retries.AddRetries(..., 3)` plus the generated client's 1 s
   timeout: once queueing latency exceeds 1 s, requests retry and triple offered work while
   still reporting 2xx. Would give a soft, self-limiting ceiling.

### Deploy-to-deploy variance (affects how these numbers may be read)

Identical no-pool code measured **56 360** and **59 665** on two rebuild/redeploy cycles
(~5.5% apart) — the same effect as dsb_sn's H3 in
`examples/dsb_sn/notes/nt_ceiling_below_sampled_tracing.md`. With n=1 per config only
differences ≳5% are meaningful; the pooling effects above (−25%) clear that bar, small
differences between adjacent N do not.

### Image-tag trap (cost one invalid measurement)

Every build pushes to the **same** tag (`<svc>-ctr:latest`). Re-applying an earlier
config's manifests therefore pulls whatever `latest` currently is — a "no-pool" re-measure
after the p256 push returned 44 346 rps, i.e. it silently re-measured p256. **Rebuild and
push immediately before measuring any config** (`sweep_pool.sh` does this per point).

## Caveat / not done

The 1000 B configurations do not quite reach full saturation by 62k offered (`f1000` was
still climbing: 60 815 achieved and not flattened the way the 1024 B run was at ~58.7k).
The three 1000 B campaigns remain mutually comparable (identical ramp), but a supplementary
62k→70k probe would be needed to pin their true knee. Not run.
