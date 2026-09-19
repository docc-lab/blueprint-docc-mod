# Tomislav-RetCtx: Social Network return-policy evaluation

The September 15 evaluation runs the real ComposePost workload with vanilla
tracing, PB, CGPB, and SB. Service implementations and the original
`examples/dsb_sn/scripts/compose-post.lua` are unchanged.

## Protocol and provenance

The supplied `bridges.pdf`, section 5.1, supplies application placement, CPU
allocations, fixed 2.2 GHz clocks, and the DSB wrk2 workload. Section 5.2 still
contains missing ramp bounds. The recovered controlled manifests and historical
ramp scripts supply those missing settings; this is not a claim that every
parameter appears in the draft.

The recorded cluster has 40 logical CPUs on every node. Node-6 reports about
124.6 GiB usable RAM, while the other nodes report about 187.6 GiB; the draft
describes uniformly 192 GB machines. Node-1 also runs control-plane components
alongside its assigned workload. `before/node-capacities.json` and CPU-frequency
records preserve the actual hardware and placement for interpreting the results.

| Setting | Evaluation value |
| --- | --- |
| Application | 13 services, five MongoDB instances, five Redis instances |
| Placement | Eight application nodes; Jaeger and Elasticsearch on node-9 |
| Application resources | 8 CPUs and GOMAXPROCS=8 per service; caches 4 CPUs; databases 8 or 16 CPUs according to the saved placement |
| Backend resources | Jaeger 24 CPUs; Elasticsearch 8 CPUs and 4 GiB Java heap |
| Collectors | One per application node, using node-local service routing |
| Collector resources | CPU request/limit 500m; memory request/limit 256 MiB; GOMEMLIMIT=230MiB; GOGC=100 |
| Vanilla pipeline | OTLP receiver → stock memory_limiter → batch → OTLP/Jaeger |
| Bridge pipeline | OTLP receiver → priority processor → batch → OTLP/Jaeger |
| Memory control | 100 ms checks; 50% soft and 70% hard thresholds; priority checkpoint safety factor 1 |
| Batching/export | 8,192-span batches, 200 ms timeout; queue 1,000 batches, ten consumers; exporter retries enabled |
| SDK export | Sampling ratio 1; export retries disabled |
| Forward checkpoints | Uniform integer CPD 2–6, selected independently at roots and original checkpoints |
| Reverse checkpoints | `inverse_depth`: each returned truss gets probability 1/origin absolute span depth at each eligible receiver |
| Leaf rejection | Probability 1 at unscheduled non-root server leaves; original forward checkpoints are protected |
| Ramp | 2,000–5,000 requests/s in steps of 200, 30 seconds per point |
| Repetition | Five rounds, four variants per round, rotating variant order; 320 measured points |
| State and seeds | Fresh application/backend pods and Reed98 graph per run; paired workload seeds 1001–1005 |
| Warmup | 100 requests/s for 100 seconds, one thread and ten connections |
| Ramp concurrency | connections = ceiling(rate²/20000); threads = ceiling(connections/10) |

Application and backend memory remain uncapped, matching the recovered controlled
manifests. Collector GOMAXPROCS retains the runtime default. The active gRPC
receiver has `include_metadata: true`: otherwise absent priority headers are
interpreted as high priority. Both pipelines use batch and info-level logging.
The earlier controlled bridge manifests omitted batch; adding it follows the
pipeline described in the paper. Eight collector nodes replace the old nine-node
DaemonSet. These differences are recorded in `plan.json`.

An original checkpoint is determined from the **incoming** forward TTL. A
nonzero incoming TTL decrements on that span; decrementing to zero does not make
that span an original checkpoint. An unscheduled leaf refuses checkpointing,
returns its original span ID, absolute depth and truss bytes, and still exports
its ordinary span. Probability-mode return segments carry no reverse TTL.
Original checkpoints and roots absorb pending returns. Client and server spans
both participate, so absolute span depth is not RPC-hop count. See the
[simulator primer](reverse_probability_simulator_primer.md) for exact semantics.

## Reproducible preparation and execution

The experiment root is
`/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z`.
It contains the concrete plan, original deployment snapshots, source hashes,
image digests, generated manifests, logs, and atomic progress files. Large
`smoke/` and `run/` directories point into the dedicated `/storage` volume.

Source the profile and the repository virtual environment before using the
helpers. They consume the prepared experiment root:

```bash
source ~/.profile
source /users/tomislav/blueprint-docc-mod/.venv/bin/activate
python utils/prepare_dsb_sn_e2e.py --out "$experiment_root"
python utils/build_dsb_sn_e2e.py --out "$experiment_root"
python utils/run_dsb_sn_e2e.py smoke --out "$experiment_root"
python utils/run_dsb_sn_e2e.py run --out "$experiment_root"
```

Preparation calls the existing one-shot wiring script with `--skip-build`.
The builder checks every Docker build and push, freezes base images, and pins
every deployed application, collector, database, cache and backend image by
digest. This avoids the generated deployment tool's behavior of continuing after
an individual image-build failure. Only generated build files are adjusted.

The collector is rebuilt through its existing `build-and-push.sh
10.10.1.1:30000` using `GOTOOLCHAIN=go1.24.13`. A Go 1.27 build succeeded but its
runtime validation panicked in the old `SermoDigital/jose` dependency. That image
was never deployed. All four collector configurations passed validation with the
Go 1.24 image. Application images retain their generated Go 1.27 toolchain.

Smoke mode replaces only owned DSB resources in `dsb-sn`, stops the identified
old request-generating observer, verifies all 33 pods and image/resource settings,
seeds 962 users and 37,624 directed follows, and sends 10 requests/s for 30 seconds.
Bridge checks cover live discovery values, reverse-checkpoint counters, stored
checkpoint payloads, high/low priority admission, and cgroup-derived thresholds.
Verbose return sampling is enabled only for smoke checks. Measured runs sample
one checkpoint diagnostic per 10,000 checkpoint events.

Completed runs are preserved when the runner resumes. An interrupted run is
archived and restarted from fresh state: continuing halfway through a stateful
ramp would change its history. Do not run two campaign processes concurrently.
Each variant starts with new database/cache/Elasticsearch pods; the manifest
contains no persistent database volumes. The last deployment remains available
for inspection after the campaign.

## Measurement interpretation

The existing DSB wrk2 binary schedules open-loop requests. Its actual `Sent`
count, completed requests, successful responses, HTTP errors, socket errors and
generator CPU time are retained separately from the configured offered rate.
The runner exports `RANDOM_SEED` to Lua and uses the original request format.
Latency comes from the explicit HDR percentile spectrum; full stdout and stderr
are retained. This DSB fork's Thread Stats header is `99%`, whereas upstream wrk
uses `Max`; the earlier review's maximum/p99 criticism did not apply to this fork.

This DSB fork records elapsed time from actual socket write to response and can
send another request before the preceding response completes. Its latency
therefore includes HTTP connection/request queueing. Each invocation resets the
HDR histogram during calibration at about ten seconds, so the 30-second point's
latency distribution covers roughly its final 20 seconds; request counts and
throughput cover the full invocation. Its timeout counter samples old connection
send timestamps, so it must not be interpreted as a count of distinct failed
requests. The binary and original workload are preserved for paper comparability.

Before/after snapshots preserve collector Prometheus counters, SDK and priority
processor logs, pod identity/restarts, and kubelet CPU/memory samples. First-use
error counters contribute their observed count; missing or reset counters are
flagged. Counter windows include snapshot boundaries and are not exact wrk timing
windows. Stored Jaeger trace samples are archived independently of HTTP success.
Successful application responses do not establish that all their spans survived.
Jaeger searches select a bounded, nonrandom cohort; these samples do not estimate
an unbiased whole-workload completeness fraction. Heavy SDK error logging can
also push periodic SDK counters out of the diagnostic log tail. The analyzer
marks missing SDK log windows and keeps the collector's independent high/low
priority counters separate.

Tomislav-RetCtx: the first live ramp exposed asynchronous indexing: a recent
one-span search result later contained all 23 spans when fetched by ID. The runner
now waits at least 30 seconds after each ramp, checking collector/Jaeger queues
and Elasticsearch writes for up to 120 seconds, then fetches the saved IDs again.
`settled-traces.json.gz` accompanies each unchanged immediate sample. Drain status
and query failures are explicit. This runs after all timed points, before fresh
state replaces the backend. If the immediate search found no indexed traces,
its original time-bounded search is repeated after drain in `delayed-search/`.
The first attempt stopped during generator initialization at 4,800 requests/s
and is retained separately as an interrupted run.

Tomislav-RetCtx: the inherited 1,024-file-descriptor soft limit could not support
the final worker/connection counts. The runner raises its own soft limit to at
least 16,384 before starting wrk and records the effective limits. The partial
first attempt is excluded from the primary results; the full ramp restarts with
fresh state. Application resources and request-format code are unchanged.

`utils/analyze_dsb_sn_e2e.py --out "$experiment_root"` audits raw request counts,
per-thread seeds, run coverage and fresh-state seed logs; `--partial` allows a
provisional report during execution. It decodes returned origins and depths,
checks TTL-free carriers and PB window-sized Blooms, and retains missing-parent
and missing-origin counts. These describe stored samples; they are not a
reconstruction-accuracy claim. CSV/JSON data and response-time/throughput figures
show means with sample standard deviation across repetitions.
Throughput calculations use wrk's reported requests/s and successful-response
fraction, avoiding bias from its rounded human-readable duration string.

The test module checks HDR parsing, units, error accounting, truncated output,
and counter appearance/reset behavior. Live smoke checks supply deployment and
return-policy evidence before measured ramps begin.
