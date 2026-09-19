# Tomislav-RetCtx: detailed agent handoff — September 15, 2026

**Finalized 2026-09-15T07:01:52.391964+00:00. Live snapshot: 80/320 points, 5/20 ramps complete; repetition 2, cgpb, warmup. See section 24 for the fresh results and always re-read live status.**

This document transfers the implementation history, active experiment, operational
state, verified findings, corrections, and remaining work from the current agent.
The user explicitly requested a very detailed handoff because this conversation
is approaching usage limits. The experiment must continue across the handoff.

**Do not start another campaign process. A detached runner and monitor are already
running. Read the live status files first. Do not change application service
implementations.**

This is a timestamped handoff, not a continuously refreshed dashboard. The live
files named below take precedence over its progress snapshot. Later appendices
contain machine-derived status, results, exact configuration, and repository state
captured when the document was finalized.

## 1. Immediate orientation and first actions

The active objective is a real DeathStarBench Social Network end-to-end ramp,
using the paper and recovered historical deployment settings:

- Vanilla tracing uses the collector's stock `memory_limiter` processor.
- PB, CGPB, and SB use the custom `priority` processor.
- All four pipelines include batch processing and export to Jaeger/Elasticsearch.
- Bridges draw forward checkpoint distances uniformly from integers 2 through 6.
- Unscheduled non-root server leaves reject checkpointing with probability 1.
- Returned trusses use `inverse_depth`: probability `1 / origin_absolute_depth`
  independently at each eligible receiving span. This is not reverse TTL mode.
- The full campaign is **n=5 complete ramps per variant**, not n=1 across many
  settings. Each ramp has 16 rates, giving 20 ramps and 320 measured points.

Use this shell setup whenever building, running helpers, or inspecting the cluster:

```bash
source ~/.profile
source /users/tomislav/blueprint-docc-mod/.venv/bin/activate
cd /users/tomislav/blueprint-docc-mod
experiment_root=/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z
```

Read the actual current status, not just an old conversation update:

```bash
python - "$experiment_root" <<'PY'
import json
from pathlib import Path
import sys
r = Path(sys.argv[1])
for name in ('run-status.json', 'monitor-status.json', 'progress.json'):
    data = json.loads((r / name).read_text())
    if name == 'progress.json':
        data.pop('latest_point', None)
    print(name, json.dumps(data, indent=2))
for name in ('run.pid', 'monitor.pid'):
    pid = int((r / name).read_text())
    path = Path(f'/proc/{pid}/cmdline')
    print(name, pid, path.read_text().replace('\0', ' ') if path.exists() else 'NOT RUNNING')
PY
```

The known current runner PID is **1476294**, and monitor PID **1476307**. Verify
their command lines; PID reuse is possible. The runner is executing
`utils/run_dsb_sn_e2e.py run --out <experiment_root>`; the monitor is executing
`<experiment_root>/monitor_e2e.py`. Both are independent of the chat session.

Their active logs are:

```text
<experiment_root>/logs/run-corrected.log
<experiment_root>/logs/monitor-corrected.log
```

These logs can be empty during normal operation. Atomic status files and newly
written point artifacts are the normal progress indicators.

Before doing any mutation, establish that the runner is healthy and whether it is
deploying, seeding, warming up, measuring, or capturing traces. Do not redeploy,
seed, send extra requests, rebuild images, change CPU clocks, or alter collector
configuration merely to get oriented. Those operations would change the active
measurement. Read-only file analysis is appropriate.

## 2. User intent, preferences, and authorization

The user's standing requirements matter as much as the mechanics:

1. **Do not change actual service implementations.** The user emphasized this
   repeatedly and emphatically. SDK, instrumentation wrapper, infrastructure,
   configuration, generator, deployment, analysis, and documentation work are
   within scope; application business logic is not to be modified for this work.
2. Use the real DSB Lua workload and existing ID generation. Do not invent request
   formats, bypass seeding, replace IDs, or repair workload failures by changing
   service code. The current runner uses the original repository
   `examples/dsb_sn/scripts/compose-post.lua` unchanged.
3. Source the profile and virtual environment yourself. The user correctly pointed
   out that the agent running builds is responsible for its environment.
4. Questions must be asked in ordinary chat. The user cannot access the queued
   follow-up question menu and explicitly asked not to use it. Do not use the
   asynchronous question tool for this conversation.
5. Do not use LaTeX; it does not render in the user's terminal. Use plain-text
   arithmetic and tables.
6. Document changes concisely with `Tomislav-RetCtx:` so the user's edits are
   distinguishable from older comments. This handoff is intentionally detailed;
   routine source comments should remain brief.
7. Necessary builds, pushes to the local registry, deployments, seeding, and load
   for the agreed experiment were already authorized. Do not ask again just to
   continue the existing campaign. A change of experimental policy/protocol is a
   separate decision; the user's questions about results did not authorize
   silently tuning the admission controller.
8. Keep candidly reporting what the evidence establishes. The user is interested
   in checkpoint protection despite uneven load, but ordinary-span losses and
   actual throughput must remain visible. Do not simply agree with optimistic
   interpretations or conceal earlier reporting errors.
9. The user requested commits earlier; those commits are already made. **No new
   commit or Git push has been requested for the current end-to-end additions.**
10. This handoff is a request to transfer ongoing work, not to stop the campaign.
    Do not tear down the current deployment or terminate the runner when ending
    the current agent's turn.

The current session's developer instructions prohibit spawning subagents unless
explicitly authorized by the user or applicable local instructions. No subagents
are active. No relevant `AGENTS.md` was found in the repositories. No skill was
needed for this work. No explicit persistent goal was created.

The current environment is unrestricted filesystem/network access, with approval
policy `never`; do not pass a `sandbox_permissions` override. Re-evaluate tool
availability if the successor agent's environment differs.

## 3. Workspaces, commits, and what is not committed

| Item | Location / value |
| --- | --- |
| Home | `/users/tomislav` |
| Main repository | `/users/tomislav/blueprint-docc-mod` |
| Collector repository | `/users/tomislav/opentelemetry-collector-contrib` |
| DeathStarBench checkout | `/users/tomislav/DeathStarBench` |
| Main and collector branch | `retctx-reject-impl` |
| Python environment | `/users/tomislav/blueprint-docc-mod/.venv` |
| Paper supplied by user | `/users/tomislav/bridges.pdf` |
| Registry | `10.10.1.1:30000` |
| Kubernetes namespace | `dsb-sn` |
| Social Network HTTP endpoint | `http://10.10.1.1:23229` |
| Time zone | UTC |

Main repository HEAD:

```text
6997134e05ed87cb0ec902b85076ea7f616bbd51
Implement SDK reverse checkpointing and collector load experiments
```

This large commit contains the SDK reverse checkpoint work, forward CPD ranges,
Bloom sizing, arbitrary DAG examples, Hotel integration, lightweight spanload
generator, distribution import, and standalone collector experiment tooling.
Its parent is `72912cab` (self-describing AMQ geometry and generalized truss
segments). Do not assume the changes were split into many small feature commits.

Companion collector commit:

```text
ca8f540bd7
Validate discovered checkpoint ranges and reverse routing policies
```

The collector working tree was clean when inspected for this handoff. The custom
priority admission algorithm itself substantially predates the current campaign;
the current agent has not tuned it in response to these results.

Current uncommitted main-repository work includes:

- New `utils/prepare_dsb_sn_e2e.py`.
- New `utils/build_dsb_sn_e2e.py`.
- New `utils/run_dsb_sn_e2e.py`.
- New `utils/analyze_dsb_sn_e2e.py`.
- New `utils/test_run_dsb_sn_e2e.py` and `utils/test_analyze_dsb_sn_e2e.py`.
- New `docs/dev/dsb_sn_retctx_evaluation.md`.
- New `docs/dev/reverse_probability_simulator_primer.md`.
- Changes to `docs/dev/reverse_checkpoint_probability.md` and
  `docs/dev/tomislav_retctx_changes.md`.
- This handoff document and its link from the change map.

There are also generated build directories, generated placement YAMLs, older
untracked build artifacts, and Python bytecode caches. They are not service source
changes and should not be indiscriminately staged or deleted. The final appendix
records `git status --short` at handoff time.

The current experiment records SHA256 hashes of **50 application workflow/wiring
Go files** in `application-source-hashes.json`. They were unchanged during the
evaluation setup. The runner validates those hashes at startup. A successor must
preserve that constraint, even though older historical work included two Hotel
service fixes before the user imposed the service-implementation prohibition.

## 4. Current campaign: complete experimental design

The authoritative plan is `<experiment_root>/plan.json`, with concrete variants
in `cases.json` and pinned manifests under `builds/<kind>/`.

| Dimension | Value |
| --- | --- |
| Application | DSB Social Network ComposePost |
| Variants | `v`, `pb`, `cgpb`, `sb` |
| Measured offered rates | 2,000 through 5,000 requests/s, inclusive, step 200 |
| Points per ramp | 16 |
| Timed duration per point | 30 seconds |
| Repetitions | 5 full ramps per variant |
| Total full ramps | 20 |
| Total measured points | 320 |
| Warmup before each full ramp | 100 requests/s for 100 seconds |
| Warmup concurrency | 1 thread, 10 connections |
| Workload seeds | 1001, 1002, 1003, 1004, 1005 |
| Initial state | Fresh application/backend pods and fresh Reed98 seeding per ramp |
| Sampling | SDK sampling ratio 1 |
| SDK export retries | Off |
| Forward CPD | Uniform integer draw in [2,6] |
| Reverse policy | `inverse_depth` |
| Eligible leaf rejection | Probability 1 |

The rotation is:

| Repetition | Seed | Variant order |
| ---: | ---: | --- |
| 1 | 1001 | v → PB → CGPB → SB |
| 2 | 1002 | PB → CGPB → SB → v |
| 3 | 1003 | CGPB → SB → v → PB |
| 4 | 1004 | SB → v → PB → CGPB |
| 5 | 1005 | v → PB → CGPB → SB |

All four variants within a repetition use the same workload seed. Different
repetitions use different seeds. Checkpoint policy random draws are not claimed
to be paired by that Lua workload seed. The five replications are five separate
ramps with reset application/backend state, not five measurements extracted from
the same ramp.

Rates within a ramp are ordered and share the evolving state of that ramp. The
benchmark does not reset databases or redeploy between individual rates. Before
the next variant, the owned deployment resources are replaced and the graph is
seeded again. Point timing includes separate before/after capture operations;
there are measurement gaps between load invocations, and those are retained in
the raw timestamps.

The seed step creates **962 users and 37,624 directed follow relationships** using
the original DeathStarBench seeder, with concurrency limit 200. The runner checks
for both exact success counts and absence of `Failed:`. The workload sets
`max_user_index=962` and exports `RANDOM_SEED` to the Lua environment.

Ramp concurrency follows the recovered schedule:

```text
connections = ceil(offered_rate * offered_rate / 20000)
threads     = ceil(connections / 10)
```

Thus the 2,000 point has 200 connections and 20 threads; the 5,000 point has
1,250 connections and 125 threads. The runner raises its own file-descriptor
soft limit to at least 16,384 and records the effective soft/hard values for each
wrk invocation. The recorded hard limit is 1,048,576.

Each measurement calls the existing DSB fork of `wrk` with `-r -L`, the unchanged
`compose-post.lua`, and `-R <offered_rate>`. Do not replace this with hand-built
curl requests or a different workload while collecting the agreed comparison.

## 5. Paper provenance and known deviations

The paper SHA256 is:

```text
5561af7d07c0b5eb362dfb53d2c2ecec1d90e348dd855676ce96798a1b8daa71
```

Section 5.1 supplies the overall placement/resource methodology, fixed 2.2 GHz
clocks, and DSB wrk2 workload. Section 5.2 has missing values in the draft. The
missing ramp parameters and some concrete deployment settings were recovered from
historical controlled manifests and ramp scripts. **Do not claim that every value
in the plan is stated explicitly in the paper.**

Historical evidence is preserved in:

```text
/users/tomislav/deployments/dsb-sn/ramp-review-20260911/REVIEW.md
/users/tomislav/deployments/dsb-sn/ramp-review-20260911/APP_ALLOCATIONS.md
/users/tomislav/deployments/dsb-sn/ramp-review-20260911/controlled-allocation-evidence.json
/users/tomislav/deployments/dsb-sn/ramp-review-20260911/historical-evidence.json
```

Important current differences from older controlled manifests:

- The active OTLP receiver preserves gRPC metadata; otherwise the priority header
  is lost and missing metadata defaults to high priority.
- The bridge pipeline includes batch processing, following the paper's pipeline
  description. Some older controlled bridge manifests omitted batch.
- There are eight collectors on the eight application nodes, rather than the old
  nine-node DaemonSet.
- Both collector variants log at info level.
- Random CPD 2–6 and inverse-depth reverse returns are the user's current request,
  rather than the older fixed-distance/rejection-disabled configuration.
- All ten nodes report 40 logical CPUs and were verified at fixed 2.2 GHz with
  performance governor and turbo disabled. Node-6 has approximately 124.6 GiB
  usable RAM; other nodes have approximately 187.6 GiB. The paper describes
  uniform 192 GB machines.
- Node-1 also carries control-plane components. The application placement was not
  silently rebalanced to remove that fact.

Preserved evidence includes `before/cpu-frequency.json`,
`before/node-capacities.json`, deployment snapshots, and the plan itself.

## 6. Exact application placement and resource allocations

There are **13 application services, five MongoDB backends, and five Redis
caches** in Social Network: 23 application/data components. Jaeger and
Elasticsearch add two Deployments, giving 25 Deployments. Eight collector
DaemonSet pods bring the expected Ready pod count to 33 for one active variant.

| Node | Application services | Data services |
| --- | --- | --- |
| node-1 | composepost, userid | None |
| node-2 | hometimeline, urlshorten | hometimeline-cache, urlshorten-db |
| node-3 | usermention, usertimeline | usertimeline-cache, usertimeline-db |
| node-4 | socialgraph, text | social-cache, social-db |
| node-5 | post-storage | post-cache, post-db |
| node-6 | media, uniqueid | None |
| node-7 | wrk2api | None |
| node-8 | user | user-cache, user-db |
| node-9 | Jaeger, Elasticsearch | Trace backend |

Each application service requests and is limited to **8 CPU cores**, with
`GOMAXPROCS=8`. Each Redis cache requests/limits 4 cores. MongoDB gets 8 cores,
except `social-db` and `usertimeline-db`, which get 16 cores. Jaeger gets 24 cores
and `GOMAXPROCS=24`; Elasticsearch gets 8 cores and a 4 GiB Java heap.
Application and backend container memory is uncapped, matching recovered controls.
Do not mistakenly report a 13-core MongoDB observation as violating an 8-core
limit: some databases explicitly have 16 cores.

Each node-1 through node-8 collector has CPU request=limit **500m** and memory
request=limit **256Mi**. `GOMEMLIMIT=230MiB`, `GOGC=100`, and `GC_INTERVAL_SEC=0`.
Collector `GOMAXPROCS` is left to the runtime default, as in the recovered
configuration; it was not forced to one for this end-to-end experiment.

The collector Service uses `internalTrafficPolicy: Local`. SDK OTLP and config
discovery therefore reach the collector on the same node. This explains why the
checkpoint traffic is uneven and why per-node analysis matters. Node-0 runs the
load generator/orchestration and is not an application collector node. Node-9
also has no application collector in the current DaemonSet.

Backend image families are MongoDB 4.4, Redis 8.6, the preserved Jaeger v1 image,
and Elasticsearch 7.17.20, all pinned by digest. Exact images are in
`base-image-digests.json` and `builds/<kind>/images.json`; the deployed manifest is
the authoritative resource/image record.

Database/cache/Elasticsearch volumes in these manifests are ephemeral. Resetting
their pods between ramps clears benchmark state. The active runner intentionally
leaves the last variant deployed after the full campaign; no final cleanup was
requested. System, registry, and unrelated NFS provisioner resources are preserved.

## 7. Collector pipelines and SDK environment

Vanilla traces:

```text
OTLP/gRPC receiver → stock memory_limiter → batch → OTLP exporter → Jaeger → Elasticsearch
```

Bridge traces:

```text
OTLP/gRPC receiver → custom priority processor → batch → OTLP exporter → Jaeger → Elasticsearch
```

The receiver listens on `0.0.0.0:4317` and has **`include_metadata: true`**.
The config-discovery receiver listens on port 8080 and is part of the existing
logs/config pipeline with a `debug/config` exporter. The SDK reads configuration
at startup. The live bridge discovery response is checked on all eight nodes:

```json
{"cpd_min": 2, "cpd_max": 6, "reverse_policy": "inverse_depth"}
```

Stock memory limiter settings:

```yaml
memory_limiter:
  check_interval: 100ms
  limit_percentage: 70
  spike_limit_percentage: 20
```

This gives soft=50% and hard=70% of the collector's cgroup memory limit.
Priority settings:

```yaml
priority:
  check_interval: 100ms
  soft_percentage: 50
  hard_percentage: 70
  cp_safety_factor: 1
  force_gc: true
  gc_soft_interval: 1s
  gc_ultrasoft_interval: 0s
```

For a 256 MiB limit, soft is 134,217,728 bytes (128 MiB), and hard is
187,904,819 bytes. Matching soft/hard limits does not mean the algorithms make
the same admission decisions below soft.

Both use `batch.send_batch_size=8192`, `timeout=200ms`; the current end-to-end
configuration does not set a separate `send_batch_max_size`. The OTLP export
queue has 1,000 batches and ten consumers, with `block_on_overflow=false`.
Exporter retries are enabled, initial interval 5s, maximum interval 30s, and
maximum elapsed time 0s (unbounded retry duration). TLS is insecure inside this
test deployment. Detailed collector metrics are exposed on `0.0.0.0:8888`.

Application SDK environment for bridge variants includes:

```text
BRIDGE_KIND=pb|cgpb|sb
GOMAXPROCS=8
OTEL_SAMPLE_RATIO=1
OTLP_RETRY=off
REVERSE_TRUSS=on
RT_LEAF_REJECT=1
RT_ROOT=off
RT_SAMPLE=10000
```

Vanilla uses `BRIDGE_KIND=v`, `REVERSE_TRUSS=off`, and `RT_LEAF_REJECT=0`.
`RT_SAMPLE=1` was used only for smoke diagnostics; measured ramps use 10,000.
`RT_SAMPLE` controls diagnostic logging, not trace sampling. Actual trace roots
still absorb returns with `RT_ROOT=off`; that variable is an additional process
boundary override, not the actual-root detector.

The known good collector image is:

```text
10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
```

All 52 application images (13 services × four variants) were built and pushed.
Application, collector, database, cache, and backend image references in the
active manifests are pinned by digest. Variant resource names are exactly of
the form `pb-esrtx20260915t041603z`, not `pb-es-rtx...`.

## 8. Why the priority processor sheds before the soft limit

Read the actual implementation in the sibling collector repository:

```text
processor/priorityprocessor/priority.go
processor/priorityprocessor/config.go
```

**Its README and DESIGN documents describe older buffering/eviction designs and
are stale relative to the active implementation.** Do not infer the current
pipeline shape or behavior from those documents. Some comments and the
initialization version label in `priority.go` also describe previous analytical
revisions; the executable arithmetic is authoritative.

The current processor classifies an entire OTLP batch from gRPC metadata
`bridges-priority`. A value `lp` means ordinary/low-priority; anything else,
including missing metadata, is high-priority. The SDK sends separate priority
batches. The processor performs passthrough admission, not the older internal
LIFO-queue eviction architecture.

It checks Go `runtime.MemStats.Alloc` every 100 ms. Soft/hard states refuse both
classes, subject to its forced-GC checks. Below soft, it derives an earlier LP
shedding threshold (`us_bytes`) from predicted incoming allocation and GC swing.
Relevant constants are a one-second horizon (10 ticks), a 4× proto-to-heap
overhead factor, sliding arrival rates, and smoothed checkpoint/ordinary byte
sizes measured from one in 32 batches. Arrival rates count admitted plus refused
spans, so dropping LP does not remove those arrivals from the predictor.

The current arithmetic, in plain text, is roughly:

```text
total_rise = (checkpoint_commit_envelope + ordinary_commit_envelope) * horizon
ordinary_share = ordinary_commit_envelope / total_commit_envelope
size_gain = estimated_checkpoint_bytes_per_span / estimated_ordinary_bytes_per_span
margin = remaining_GC_swing + cp_safety_factor * size_gain * (1 / ordinary_share) * total_rise
ordinary_threshold = clamp(soft_limit - margin, 0, soft_limit)
```

It admits all LP batches below that threshold and refuses all LP batches above
it. The aggregate fraction shed is a result of time spent in each state, not a
per-span probability. High checkpoint byte rates, larger checkpoint spans, or
a small ordinary byte share can reduce the threshold substantially. The current
configuration disables the optional ultrasoft forced-GC tier (`0s`).

The controller intentionally retains some envelopes through idle gaps. Therefore
between-step pauses should not be casually assumed to reset its admission state.
Its one-second info logs expose `hp_admitted`, `lp_admitted`, `hp_refused`,
`lp_refused`, `alloc_bytes`, `us_bytes`, `lp_share`, estimated bytes per class,
soft/hard limits, state, and GC count. Use these rather than guessing from total
receiver refusals alone.

Low measured heap after shedding does not establish that shedding was unnecessary:
the control action has already changed what was admitted. Conversely, zero HP
refusals alone does not establish that the amount of LP sacrifice was optimal.
The current campaign is measuring the existing policy, not tuning it mid-run.

## 9. SDK reverse checkpointing: what was implemented

The original discussion started by moving checkpoint rejection out of wrappers
and into the tracing SDK. The user clarified that rejection means **rejecting
checkpointing**, not dropping a span from an export buffer. A rejected leaf still
exports its ordinary span. Its truss returns upstream with the original span ID
and depth, where a later span may emit it as checkpoint data.

There are three different phenomena that must not be conflated:

| Term | Meaning |
| --- | --- |
| SDK leaf checkpoint rejection | An eligible leaf declines checkpoint status and returns its truss; the ordinary span remains exportable |
| Collector ordinary-span refusal | The priority processor refuses an incoming low-priority OTLP batch under its admission policy |
| Collector checkpoint refusal | The priority processor refuses incoming high-priority checkpoint spans under soft/hard pressure |

The user intentionally enabled the first mechanism. The experiment measures the
second and third. Because SDK export retries are off, collector refusals show up
as SDK send failures/drops rather than eventual successful retries. Collector
exporter retries downstream to Jaeger are separately enabled.

The main implementation files in the Blueprint repository are:

```text
runtime/core/backend/checkpoint.go
runtime/core/backend/reversetruss.go
runtime/plugins/otelcol/reverse_checkpoint.go
runtime/plugins/otelcol/reverse_policy.go
runtime/plugins/otelcol/checkpoint_distance.go
runtime/plugins/otelcol/checkpoint_window.go
runtime/plugins/otelcol/pb_processor.go
runtime/plugins/otelcol/cgpb_processor.go
runtime/plugins/otelcol/sb_processor.go
plugins/opentelemetry/ir_ot_client.go
plugins/opentelemetry/ir_ot_server.go
```

### 9.1 Lifecycle: before End is essential

After a downstream RPC returns, the client wrapper attaches the returned context
to the still-recording span as `__bag.rev_in`, invokes
`backend.PrepareCheckpoint`, then reads unconsumed output from `__bag.rev` into
the enclosing request's accumulator. The server wrapper does the same after its
children finish and child/event counts are available.

Preparation is SDK-owned and idempotent. It must happen **before `Span.End`**
because the OTel SDK freezes span attributes before invoking the processor's
`OnEnd`. Waiting until ordinary processor `OnEnd` to attach incoming reverse
context is too late. `OnEnd` exports the prepared classification and data.

Wrappers transport and merge returns; they do not choose the checkpoint policy.
If a span is not recording or a preparation hook is unavailable, received context
passes through. Private decision/carrier attributes are excluded from exported
attributes and forward baggage. The exported receiving payload is
`bridges.checkpoint`. An emitting bridge span carries its own `_br` plus that
returned-truss envelope. An eligible rejected leaf exports ordinary `_d`/`_o`
metadata while returning its own truss upstream.

### 9.2 Forward scheduling and original-checkpoint identity

With `cpd_min`/`cpd_max`, roots and original scheduled checkpoints each draw an
integer distance D from the inclusive range. Their outgoing baggage begins with
a mutable byte TTL equal to D-1. Ordinary downstream spans inspect the incoming
TTL, decrement it if nonzero, and propagate the result.

For D=2, the root sends 1; the next span sees 1 and sends 0; the next sees 0 and
is a checkpoint. A span that decrements 1 to 0 is **not** itself an original
checkpoint. Original status comes from the incoming TTL at `OnStart` and is
preserved immutably through end processing.

Both client and server spans participate. Forward checkpoint distance is measured
in span steps, not service-to-service RPC hops. At a fanout, siblings receive
copies of the same outgoing state and advance independently. A scheduled
checkpoint draws once for its descendant paths; siblings do not mutate one
shared TTL byte.

The collector validates paired integer bounds in 1–256, allowing TTL 0–255. The
active experiment uses 2–6. Legacy fixed `cpd` remains supported with its legacy
forward format. Do not assume switching to `cpd: 2` is byte-for-byte equivalent
to the new ranged format with min=max=2.

### 9.3 Bloom filters follow the selected distance

An early experimental revision sized all ranged-window Blooms for `cpd_max`.
The user explicitly rejected that. The corrected implementation sizes each
PB/CGPB Bloom for its **selected intended window distance**, using capacity
`max(1, D-1)` and false-positive target 0.0001.

For D=2,3,4,5,6, the verified Bloom byte sizes are 3,5,8,10,12 respectively.
The mutable TTL is insufficient to decode geometry after it changes, so the
truss also carries an immutable distance descriptor D-1.

```text
ranged PB/CGPB truss:
    uvarint(depth) || checkpoint_span_id(8 bytes)
    || byte(window_distance - 1) || Bloom || optional_CGPB_hash_array

forward baggage:
    base64url(TTL_byte || truss)
```

A checkpoint exports the incoming window and starts a new outgoing one. Incoming
and returned trusses keep their original Bloom geometry. Early server leaves do
not resize the truss to their shorter actual path. SB has no path Bloom and
retains its own truss format.

### 9.4 Per-truss return carrier and fan-in

The RPC return context remains a string containing base64 JSON with the existing
`fp`, `parent`, `m`, `k`, and `segs` envelope. A checkpoint segment contains:

```text
k: checkpoint.pb | checkpoint.cgpb | checkpoint.sb | checkpoint.v
d: base64(original_span_id(8 bytes) || uvarint(original_absolute_depth) || exact_truss_bytes)
ttl: optional separate JSON integer in 0..255
```

The depth inside `d` is absolute span depth with root zero. It is immutable in
transit. PB/CGPB have native absolute depth; SB additionally propagates
`__rt_depth` because its own truss depth is window-relative. The returned binary
payload always preserves the original span ID, depth, and exact truss bytes.

Probability-mode segments omit `ttl` entirely. The optional TTL is separate
segment metadata; it is not inserted into the original-ID/depth/truss payload.
`backend.DecodeReturnedCheckpoints` exposes origin, depth, bytes, and optional
`ReverseTTL`. Explicit TTL segments retain countdown behavior even in a mixed
bundle. A legacy TTL-free checkpoint under the default TTL policy waits for an
original checkpoint/root rather than inventing a TTL.

`MergeRetCtx` and the request accumulator append sibling and descendant segments.
They do not OR Bloom filters, overwrite origins, or combine all segments under
the deepest sibling's depth. A mutex protects concurrent fan-in. Merge itself
does not spend a reverse hop. Ordering follows return arrival order. An enclosing
server wrapper must wait until child calls and their wrappers finish before
asking the SDK to prepare its checkpoint.

### 9.5 Rejection and mandatory absorption

`RT_LEAF_REJECT` is the rejection probability for an unscheduled, non-root,
recording **server leaf**. Original forward checkpoints are protected, including
leaves whose incoming TTL was zero. The experiment sets the probability to 1.

At an original checkpoint or actual trace root, all pending returns are emitted
with the span's own checkpoint data, without probability trials. An ordinary
span may accept only a subset of returned trusses. Becoming a checkpoint because
it accepted a return does not make it an original checkpoint and does not cause
it to absorb the rejected siblings or reset the forward window retroactively.

`RT_ROOT=on` additionally treats a process's server boundary as terminal; it does
not make every client span in that process a root. The experiment uses off.
SB's explicit synthetic `__bag.force_lp` spans remain ordinary and forward returns
without spending a reverse hop/trial. SB delayed-end-event processing remains at
`OnEnd`. Former provider-wide `RT_POLICY` / `RT_DEPTH` bundle-routing settings
were removed from SDK routing; old deployment arguments are compatibility-only
and deprecated.

## 10. Reverse probability modes and simulator handoff

The exact primer prepared for the user's simulator agent is:

```text
docs/dev/reverse_probability_simulator_primer.md
```

Also read `docs/dev/reverse_checkpoint_probability.md` and
`docs/dev/reverse_truss.md`. Some older doc paragraphs say a revision was not yet
deployed; those describe their historical writing date. The current campaign has
rebuilt and deployed the SDK and validated it live.

Let n be the rejected leaf's immutable absolute depth, and d the current receiving
span's absolute depth. All of the following probability trials are per-truss and
conditional on that truss actually reaching an ordinary eligible receiver:

| Mode | Probability / behavior | Implemented? |
| --- | --- | --- |
| `ttl` | Independently draw reverse D; send TTL D-1; emit where incoming TTL is zero, or at an earlier original checkpoint | Yes; default |
| `probability` | Constant configured `reverse_probability`, in [0,1] | Yes |
| `inverse_depth` | `1 / n`, constant along this truss's return path | Yes; current campaign |
| `depth_linear` | `2 * (d+1) / (n * (n+1))` | Yes |
| Increasing upstream pressure | Proposed `1 / (d+1)` | **No; proposal only, no SDK configuration name** |

Do not describe the proposed increasing-pressure policy as implemented. The user
discussed it and requested a simulator primer, but it was not added to the SDK.

For a leaf at n=6, `inverse_depth` gives 1/6 at each ordinary receiver. It is not
1/current-depth, not 1/maximum-depth-across-siblings, and not uniform selection
of one final emission location. A deeper receiver gets the first opportunity;
upstream probabilities are multiplied by the chance that earlier trials failed.
The original checkpoint absorbs the remaining mass. Fixed probability zero
therefore still emits at an original checkpoint/root.

For `depth_linear`, the deeper receiver has a larger conditional acceptance
probability. Normalizing the sum of these per-node probabilities to one does not
make the final emission probabilities sum in the same way: survival factors and
mandatory absorption matter.

The proposed `1/(d+1)` mode increases conditional acceptance as the truss moves
upstream. For a depth-6 leaf on a simple chain without an intervening checkpoint,
it yields equal final mass at the six receiving positions. With an original
checkpoint at depth c, that checkpoint absorbs the remaining `(c+1)/n`. These
properties assume consecutive eligible receiving depths. They do not imply that
the proposed policy is always cheaper or has a longer return distance.

Deployment scripts accept `--reverse-policy`; fixed probability additionally
requires `--reverse-probability`. Invalid policy names, missing/nonfinite/out-of-
range fixed probabilities, and irrelevant probability settings fail validation.
Forward CPD selection and Bloom geometry remain independent of the reverse mode.
SDKs fetch the settings at startup, so consistency across a call path matters.

The current experiment tests only `inverse_depth` with CPD 2–6. A sweep of all
implemented/proposed probability policies is a possible subsequent evaluation,
not part of the presently authorized 320-point matrix.

## 11. Historical example applications and deployment work

### 11.1 Environment bootstrap

The one-shot scripts the user originally asked to locate are:

```text
utils/setup_environment.sh
utils/install_blueprint_deps.sh
```

`setup_environment.sh` installs base packages, obtains the sibling collector and
DeathStarBench repositories, builds DSB wrk2, installs the Go/protobuf/kompose
toolchain, creates the Python environment, configures Docker group access,
deploys the local registry, and pins CPU clocks. The current environment is
already set up. It is not necessary or appropriate to rerun this bootstrap in
the middle of the campaign. In particular, it can select a newer Go toolchain;
the collector compatibility issue below is relevant.

### 11.2 Configurable synthetic DAG application

The reusable example is documented in `examples/leaf/FANOUT.md`. It supports
arbitrary fanout counts, concurrent fanout, per-request parallelism limits, shared
downstream services, and JSON/YAML topology input. The user explicitly accepted
acyclic graphs. The graph must be rooted, reachable, and acyclic.

Source and specification files:

```text
examples/leaf/workflow/leaf/fanoutnode.go
examples/leaf/workflow/leaf/patternnode.go
examples/leaf/wiring/specs/docker_fanout.go
examples/leaf/wiring/specs/topology.schema.json
examples/leaf/wiring/specs/fanout.json
examples/leaf/wiring/specs/patterns.yaml
examples/leaf/wiring/specs/deep8.yaml
```

Node-level parallelism 0/omitted means unrestricted child concurrency within that
request, 1 means sequential, and N>1 limits concurrent children. Nested patterns
support `call`, `sequence`, `parallel`, `repeat`, and `sleep`, plus timeouts.
Shared services are deployed once but receive a separate call for each incoming
call edge; the system does not deduplicate those calls. Patterns describe call
behavior rather than arbitrary application business logic or dataflow.

The deep8 example has eight service-to-service RPC hops, nine services on its
longest path including the root, 15 services overall, and fanouts at service
depths 1, 3, and 6. It has 14 RPC calls and seven leaf calls per successful request;
`Process(7)` returns 56. Its transport timeout is 10s. Eight RPC hops are not eight
span-depth steps, since both client and server spans are instrumented.

Generator support changes include variadic constructor binding, workflow and
namespace wiring, Go quoting of IR literals, exclusion of test-only constructors,
and `DeployWithTimeout` while ordinary `Deploy` retains a 1s transport timeout.
Nested concurrent-wrapper tests exercise return aggregation. None of these
examples is the application being measured in the current campaign.

### 11.3 Hotel Reservation work

Hotel was checked, built, pushed, deployed, exercised with a small request set,
then torn down when the user switched to Social Network. Evidence is retained at:

```text
/users/tomislav/deployments/dsb-hotel/cgpb-es-20260910/
```

It includes `DEPLOYMENT.md`, `hotel-build.json`, image records, verification
responses/traces, `trace-depths.json`, and `trace-depth-example.json`. Consult those
artifacts for exact old trace-depth results rather than guessing from the current
Social Network topology.

Hotel has **8 services, 6 MongoDB backends, and 3 memcached backends**: 17 service/
database/cache components, excluding tracing infrastructure. Social Network instead
uses 13 services, five MongoDBs, and five Redis caches; do not mix the inventories.

Hotel's one-shot deployer is `utils/build_deploy_hotel.sh` with Python logic in
`utils/build_deploy_hotel.py`. The shell entry point sources the profile and venv.
It supports bridge variants, fixed/ranged CPD, reverse policies, collector and
Jaeger/Elasticsearch options, namespaces, placement, image builds/pushes,
sampling/rejection, probes, and optional apply. Read `examples/dsb_hotel/README.md`
for exact options and request formats.

The Hotel wiring now routes SDK traces through otelcol and config discovery,
using the same node-local pattern as Social Network. Constructors seed the sample
data, including 80 profiles and Cornell users. Backend storage is ephemeral and
initialization is not designed for arbitrary restart against existing data.

Before the later strict service-preservation instruction, two Hotel service
repairs were made: frontend search propagates availability errors, and reservation
cache counters use atomic increments. Those and their regression tests are in
the existing commit and change map. Do not pretend the entire conversation never
changed any service, and do not extend those repairs now without authorization.

## 12. Earlier CPD byte experiments: what they did and did not show

The corrected window-sized-Bloom comparison is preserved at:

```text
/users/tomislav/deployments/dsb-sn/cgpb-es-window-cpd-20260910/RESULTS.md
/users/tomislav/deployments/dsb-sn/cgpb-es-fixed6-20260910/RESULTS.md
```

It superseded the earlier experiment that incorrectly pinned all ranged Blooms
to the maximum CPD. The corrected low-rate CGPB comparison used the original Lua
workload, matched request inputs, and rejection disabled:

| Configuration | Checkpoints/request | Decoded bridge bytes/request | Bloom bytes/request |
| --- | ---: | ---: | ---: |
| Fixed 2, legacy format | 12.00 | 173.00 | 36.00 |
| Fixed 4 | 11.00 | 228.00 | 88.00 |
| Fixed 6 | 9.00 | 230.00 | 108.00 |
| Random 2–6 | 11.56 | 243.62 | 95.07 |

These comparisons use 99 matching requests per phase. The fixed-6 run completed
100 distinct posts/traces, with one completion after the generator's timed cutoff.
The complete ComposePost traces had 23 spans and a deepest root-to-leaf path of
7 spans (absolute depths 0 through 6). There were five server leaves at depth 4
and three at depth 6.

The user expected fixed-6 bytes to grow more by linear extrapolation. In fact,
larger filters were partly offset by fewer checkpoints. For this graph, fixed-6
has a root and eight leaf checkpoints, with no interior checkpoint. Randomization
can land extra checkpoints on client spans while unscheduled leaves still
checkpoint if rejection is disabled. This graph's depth aligns especially cleanly
with some fixed distances. Those observations motivated rejecting unscheduled
leaves and returning their trusses.

These old byte totals **do not measure the new rejection/return policy** and must
not be presented as current end-to-end ramp results. The older deployment and
observer artifacts remain, but the current campaign replaces that application
deployment with its own pinned variants. The old traffic observer was explicitly
stopped and recorded in `observer-paused.json`.

## 13. Lightweight spanload generator and payload distributions

The user supplied `bridges.pdf` while asking to recover a missing collector span
load generator. A lightweight standalone generator was rebuilt in:

```text
utils/spanload/
utils/spanload/README.md
utils/build_spanload.sh
docs/dev/collector_load_generator.md
```

It builds OTLP protobuf directly rather than making SDK spans. It supports gRPC
and HTTP, zero/custom/example attribute profiles, vanilla and binary bridge-size
proxies, rate ramps, reproducible seeded payload draws, aggregate accounting,
bounded queues, and optional collector metrics. Bridge payload proxies model byte
cost; they are not semantically valid application trusses or complete trace graphs.

Payload sizes can be fixed, weighted-histogram, empirical, or inclusive-uniform
distributions, including per-CPD table entries. Each checkpoint's draw is stable
by seed/sequence position across transport, worker count, and batch boundaries.
The generator sends exact sampled byte prefixes without allocating a new payload
for every checkpoint. Results retain distribution provenance, attempted payload
bytes, and bounded histograms.

The user accidentally asked this agent to obtain simulator histograms, then
clarified that another agent would supply them. The supplied
`payload_percentages.json` was imported using
`utils/generate_spanload_distributions.py`. Committed profiles are in:

```text
utils/spanload/profiles/uber-day1-random2-8/
```

The input describes Uber day 1, random CPD 2–8, with source bridge labels
PB0/CGP0/SB3. The converter checks histogram integrity and subtracts the declared
4-byte attribute key/type overhead to get the proxy payload size. All bins,
including SB's rare tail, are preserved. The checkpoint share is
**52.50588161787073%**, sampled reproducibly using seed 42 in the collector suite.
This marginal distribution is separate from the current DSB CPD 2–6 application
experiment. Do not replace one with the other.

Generator/source tools include:

```text
utils/run_spanload_ramp.py
utils/check_spanload_capacity.py
utils/run_spanload_suite.py
utils/analyze_spanload_suite.py
utils/spanload/collector.yaml
utils/spanload/collector-realistic.yaml
```

Local race tests, binary/container builds, transport checks, and distribution
round-trip accounting were already performed. Detailed records are linked in
`docs/dev/collector_load_generator.md`; no need to rerun those tests merely to
resume monitoring the current end-to-end campaign.

## 14. Completed standalone collector experiments — do not restart

The standalone collector suite is **finished**, fully audited, and cleaned up:

```text
/users/tomislav/deployments/collector-load/spanload-dense-ramps-20260914T185629Z
```

It completed September 15 around 00:43 UTC, before the current DSB evaluation.
The root contains `RESULTS.md`, `SUMMARY.json`, `completion.json`,
`final-verification.json`, `results.csv`, `averaged-points.json`, raw ramp/control
directories, copied source/configuration records, and `throughput-combined` in
PDF/PNG/SVG. **There is no `analysis/` subdirectory in this old result root.**

Its configuration was one collector, one CPU, 4 GiB/no swap:

```text
OTLP/gRPC → memory_limiter → batch → protobuf file exporter → /dev/null
```

No Jaeger, Elasticsearch, or remote backend was involved. The user explicitly
corrected an earlier misunderstanding about export work: protobuf serialization
to `/dev/null` is the intended collector-only test. It is separate from the
current real application experiment, which does include Jaeger/Elasticsearch.

Standalone settings: 100 ms limiter checks, hard 3,072 MiB, spike allowance
512 MiB, `GOMEMLIMIT=2400MiB`, `GOMAXPROCS=1`, `GOGC=100`, batch target/max 8,192,
batch timeout/file flush 200 ms. Four independent native open-loop senders shared
the aggregate offered rate, each with four available CPU cores, eight RPC workers,
64 queued batches, 512 spans/request, 2s RPC timeout, and 5s drain deadline.

It measured all 16 zero-attribute rates (100k–1.6M spans/s in 100k increments) and
all 20 ten-attribute rates (10k–200k in 10k increments), four variants, and three
repetitions: **432 ramp points**, plus eight matched limiter controls, total 440.
Each point offered load for 45s, discarded the first 10s, and used at least 30s
of the four senders' common active interval, excluding drain.

Final plateau means, defined from the last three rates in each run:

| Variant | Zero attributes, spans/s | Ten example attributes, spans/s |
| --- | ---: | ---: |
| Vanilla | 981,073 | 108,835 |
| PB | 542,138 | 101,626 |
| CGPB | 539,233 | 101,696 |
| SB | 533,413 | 101,220 |

Collector CPU saturated; the busiest sender used at most 1.065 of its four cores.
All raw accounting reconciled. Excess offered load was counted as generator queue
drops; there were zero RPC-failed/partial-rejected spans and zero collector
refusals in this suite. Peak collector RSS was about 272.4 MiB. Thus it recovered
the near-million-span/s vanilla result and supported the collector, rather than
the generator's CPU, being the throughput limit.

The final combined plot is **4.4 × 2.1 inches**, with two panels, one shared y-axis
label, relatively large fonts, and means with one sample standard deviation.
The user previously requested individual 2.2-inch-wide panels and then combining
them. Standard deviation bands are not confidence intervals.

Earlier standalone artifacts remain as history:

```text
spanload-zero-ramp-20260914T144131Z       initial JSON export, superseded for intended test
spanload-semconv10-ramp-20260914T153113Z  matching JSON ten-attribute run
spanload-proto-ramps-20260914T155957Z     corrected protobuf preliminary ramps/controls
spanload-dense-ramps-20260914T185629Z     final full-grid repeated suite
```

Do not accidentally revive those workloads or mix their n=3 repetition count,
one-CPU/4-GiB collector, semconv profile, or 2–8 payload mixture with the current
DSB n=5 / 500m / 256Mi / CPD2–6 experiment.

## 15. Current results, interpretations, and corrections already given to the user

The machine-generated appendices below contain a refreshed first-repetition table.
The following explains the findings and the conversational context that a new
agent must not lose.

### 15.1 First vanilla ramp

The first primary vanilla ramp completed all 16 points with no HTTP/socket errors,
no observed pod restarts, and no snapshot errors. Its first receiver refusals
appeared at offered **2,800 requests/s**. Selected refusal percentages, computed
as refused/(accepted+refused) over receiver counter windows:

| Offered requests/s | Vanilla receiver refusal percentage |
| ---: | ---: |
| 2,000–2,600 | 0% |
| 2,800 | 7.79% |
| 3,000 | 20.56% |
| 3,200 | 33.03% |
| 3,400 | 38.00% |
| 3,600 | 45.45% |
| 5,000 | 43.80% |

Completed throughput peaked at **3,453.58 requests/s** at offered 3,600 and fell
to **2,312.39/s** at offered 5,000. p99 rose from tens of milliseconds at low rates
to approximately **15.79 seconds** at offered 5,000. No HTTP errors among the
responses that completed does not mean all offered/sent requests finished within
the 30-second invocation, nor that their traces were complete.

Vanilla eventually refused spans on all eight collectors. First observed rates:
node1=2800, node2=3400, node3=3400, node4=3000, node5=4200, node6=3200,
node7=3600, node8=4400. These first-positive points were checked against raw
Prometheus text, not merely an aggregated plot.

### 15.2 PB low-rate ordinary loss and the node-1 explanation

At the first PB point, offered 2,000 requests/s, receiver counters recorded
**305,332 refused spans**, about 22.18% of all incoming span attempts. The
priority log window recorded 300,427 ordinary refusals and 614,162 ordinary
admissions: **32.85% of ordinary spans refused**. The counters have slightly
different boundaries, hence the differing absolute counts. Do not combine the
two as if they came from one identical time window.

Across offered 2,000/2,200/2,400/2,600, the ordinary refusal fractions were
approximately 32.8%/34.2%/35.0%/36.6%. Vanilla had zero span refusals at those
rates. This is a large ordinary-span sacrifice, not a negligible handful.

All first-step PB refusals came from node-1's collector, colocated with
composepost and userid. That collector refused **94.2% of its local ordinary
arrivals**: 300,427 refused versus 18,432 admitted in its periodic counter window.
The other seven collectors had zero ordinary refusals at this first point.

Its sampled heap during the step was 31–62 MiB against a 128 MiB soft limit; its
derived LP threshold sometimes fell very low, reaching zero at one sampled
instant. At 05:47:12.949Z, heap was 48.6 MiB and LP threshold 15.2 MiB. This is
the predictive admission controller acting below soft, not the stock limiter's
simple threshold behavior.

The user asked whether node-1 simply received many more checkpoints. That was
checked and **yes, absolute checkpoint load was much higher**. Steady-subwindow
checkpoint arrival rates at the 2,000 point:

| Collector | Checkpoint spans/s | Estimated checkpoint proto bytes/span |
| --- | ---: | ---: |
| node-1 | 5,207 | 349 |
| node-2 | 1,880 | 221 |
| node-3 | 1,132 | 145 |
| node-4 | 2,619 | 327 |
| node-5 | 476 | 145 |
| node-6 | 957 | 195 |
| node-7 | 2,423 | 317 |
| node-8 | 475 | 239 |

Node-1 received about **34% of all checkpoints**, twice the next collector's
checkpoint count rate and approximately 2.1 times its estimated checkpoint byte
rate. Its checkpoint share by span count was about 32%, similar to node-4; node-7
was proportionally more checkpoint-heavy, about 61%. The important distinction
was absolute volume and byte cost, not uniquely the highest checkpoint fraction.
Byte sizes are the controller's smoothed proto-size estimates, not exact network
packet measurements.

The assistant initially judged the low-rate policy as overly aggressive from the
low heap observation. After examining checkpoint traffic, it explicitly qualified
that judgment: the low heap is measured after shedding, and the concentration
explains why node-1 was selected. Whether refusing 94% of its ordinary spans was
necessary is unproven. Do not present either policy optimality or a proven
controller bug as an established result.

Supporting artifacts:

```text
analysis/low-rate-refusals.md
analysis/low-rate-refusals.json
analysis/low-rate-refusals-by-node.json
```

### 15.3 Critical correction: PB did lose checkpoints before offered 4,000

**An earlier assistant statement saying "zero checkpoint refusals through 4,200"
was wrong.** It checked low-rate and later-point results without inspecting every
intermediate point. The user was explicitly informed of the error. Never repeat
that statement from an older summary or dashboard.

The first PB ramp actually showed:

| Offered requests/s | Checkpoint refusals | Refused share of checkpoints |
| ---: | ---: | ---: |
| 2,000–3,200 | 0 | 0% |
| 3,400 | 92,440 | 12.20% |
| 3,600 | 101,794 | 13.56% |
| 3,800 | 33,832 | 4.73% |
| 4,000–5,000 | 0 in those later point windows | 0% |

All HP refusals were on node-1. Its cumulative collector count reached
**232,935**, including between-point traffic. This exceeds the sum of the three
timed-window deltas because the snapshot gaps are not included in that sum.

The final first-PB SDK capture covered all 13 services, with empty buffers and
`cp_received = cp_sent + cp_dropped` for every service. Composepost reported exactly
232,935 checkpoint drops, matching the collector lifetime count; all other SDKs
reported zero checkpoint drops. This is stronger evidence than looking at a
possibly truncated SDK log tail during one loaded point.

The later 4,000–5,000 targets were **offered rates**. Completed throughput fell
from 2,946.85/s at offered 4,000 to 2,294.25/s at offered 5,000, and checkpoint
arrival counts also fell. Zero later refusals do not establish retention at
4,000–5,000 completed requests/s. The final queues drained, collector exporter
failures were zero, and the 5,000-point Jaeger counters had zero drop/save errors;
that still is not an ID-level reconciliation of every checkpoint into storage.

See `analysis/pb-checkpoint-refusals.md` and `.json` for the correction, all points,
and final SDK accounting.

### 15.4 CGPB, SB, and the useful preliminary comparison

First CGPB checkpoint refusals appeared at offered **3,800**, with 3.02% refused
there and 4.18% at 4,000. The other measured points were zero. Those refusals were
again confined to node-1. First-ramp CGPB peak completed throughput was 3,342.99/s;
at offered 5,000 it completed 2,287.75/s, with p99 about 15.88 seconds.

First SB checkpoint refusals appeared at offered **3,400**: 6.89% at 3,400,
11.66% at 3,600, and 2.01% at 3,800. During the user-facing update through 4,000,
all other completed SB points had zero checkpoint refusals and all checkpoint
refusals were node-1. The finalized handoff appendix extends this through the
latest complete point; use it or current raw data for the remaining rates.

The user observed that vanilla's first span losses at 2,800 versus PB checkpoint
losses first at 3,400 is a **600 offered requests/s**, approximately 21%, shift
in observed onset. CGPB's first checkpoint refusal at 3,800 is 1,000 higher than
vanilla's first refusal. That is a valid description of this first repetition on
the 200-step grid. It is not a fitted capacity increase, a claim of more completed
application throughput, or a five-repetition statistical result.

The user finds the evidence compelling because seven other collectors retained
their checkpoints while vanilla eventually refused spans at all eight. The
appropriate interpretation is promising checkpoint protection under uneven load,
with substantial ordinary-span loss as the tradeoff. Reconstruction quality and
repeatability remain to be established. Vanilla has no directly corresponding
checkpoint classification in its exported stream; label its refusals as ordinary
vanilla span refusals rather than inventing a vanilla HP/LP split.

### 15.5 First-repetition per-node refusal onset reported before SB finished

| Node | Vanilla any span | PB ordinary span | CGPB ordinary span |
| --- | ---: | ---: | ---: |
| node-1 | 2,800 | 2,000 | 2,000 |
| node-2 | 3,400 | 3,400 | 3,600 |
| node-3 | 3,400 | 3,600 | 4,000 |
| node-4 | 3,000 | 3,200 | 2,000 |
| node-5 | 4,200 | None through 5,000 | None through 4,200 at that update |
| node-6 | 3,200 | 3,400 | 4,000 |
| node-7 | 3,600 | 2,200 | 2,000 |
| node-8 | 4,400 | None through 5,000 | None through 4,200 at that update |

The original `analysis/refusals-by-node.md/.json` was written while CGPB had
completed through 4,200 and SB had not started. It is a dated analysis, not a
live report. Do not assume it contains later SB data or all repetitions. The
handoff's generated appendix refreshes this comparison without erasing the
original diagnostic evidence.

## 16. Current experiment helpers and their responsibilities

### 16.1 Preparation and image building

`utils/prepare_dsb_sn_e2e.py` creates the four cases using the existing
`utils/build_deploy_dsb.sh` wiring path with `--skip-build`. It produces concrete
collector YAML, manifests, placement, resources, environments, probes, and case
metadata. It filters the unused trace-pressure helper out of the measured
application. It does not apply manifests while preparing.

`utils/build_dsb_sn_e2e.py` explicitly checks every Docker build and push, pins
base/backend images, and finalizes deployed manifests by digest. This was needed
because the generated d2k8s tool could continue after an individual image build
failed. Generated Dockerfiles were adjusted for cache mounts/pinned bases; service
implementations were not edited. All 52 image builds/pushes completed.

These preparation/build phases are finished. Do not rerun them on the active
experiment root. That could invalidate manifest hashes or replace the code being
compared between repetitions. The runner verifies prepared manifest hashes against
`build-complete.json` before applying each case.

### 16.2 Runner

`utils/run_dsb_sn_e2e.py` supports `smoke` and `run`. It validates application
source hashes, completed preparation, and successful smoke results; rotates the
variant order; deploys/reseeds; runs warmup and all rate points; captures telemetry;
then settles/requeries trace samples before replacing the backend for the next
case.

Owned-resource replacement is scoped to the DSB resources recognized by its
labels in `dsb-sn`. It is not a cluster-wide delete. The initial old observer was
stopped once through `observer-paused.json`. The runner checks the expected 33
Ready pods, digest references, collector node placement/resources, SDK environments,
and each bridge collector's live config-discovery response.

Completed cases are skipped when resumed. A directory without `complete.json`
is archived with `-interrupted-<time_ns>` and the entire case reruns from fresh
state. **It does not resume at the next point of a half-finished stateful ramp.**
Preserve interrupted artifacts; the analyzer excludes such directory names.

The running Python process loaded its functions when launched. Editing its source
on disk does not change that process. Avoid changing the runner/protocol in place
and assuming subsequent points use the new version. Any necessary future runner
change requires explicit provenance and a planned restart, with affected runs
kept distinct. No such restart is currently needed.

### 16.3 Durable monitor

`<experiment_root>/monitor_e2e.py` is a separate file-based process. It polls every
30 seconds, reads completed point files, and writes:

```text
progress.json
progress-events.jsonl
STATUS.md
monitor-status.json
```

It performs no workload traffic and no Kubernetes queries. It detects runner
failure or disappearance. It does not automatically relaunch a failed campaign.
On successful campaign completion, it runs the full analyzer, writes
`RESULTS.md`, and records `monitor-status.json` as complete. Its final report is
a useful starting point, not a substitute for the final independent interpretation
of checkpoint drops, telemetry gaps, and reconstruction limitations.

The monitor's progress can lag the runner by 30 seconds. A `result.json` can exist
before the runner adds `kind` after its after-snapshot; skip that provisional file
when counting completed points. The monitor and analyzer already do this.

### 16.4 Analyzer and tests

`utils/analyze_dsb_sn_e2e.py` independently checks raw wrk output, request counts,
latency ordering, per-thread initialization and seeds, rate coverage, and fresh
seeding. It computes resource deltas over snapshot windows and class-specific
SDK/priority log deltas separately. It validates sampled returned-truss origin
IDs, varints, no TTL in probability carriers, unique returned origins per trace,
PB/CGPB geometry, and exact PB Bloom width.

Outputs are under `<experiment_root>/analysis/`:

```text
audit.json
points.json
points.csv
curves.json
end-to-end.pdf
end-to-end.svg
end-to-end.png
```

The current end-to-end figure is 6.6 × 2.25 inches with three panels: mean response
time, p99, and successful throughput. Curves use means and sample standard deviation
across available repetitions; n=1 naturally has zero estimated SD in the plot.
Do not confuse this figure with the completed collector-only suite's 4.4 × 2.1
two-panel figure.

Seven focused Python tests passed for the current runner/analyzer: five runner
tests and two payload-analysis tests. They cover meaningful parsing/accounting
errors and malformed payloads. `git diff --check` also passed. Do not repeatedly
rerun broad SDK/build tests unless source changes or a real concern justify it.

## 17. Measurements: exact interpretation and known blind spots

### 17.1 The actual DSB wrk2 fork

Installed binary `/usr/local/bin/wrk` matches the built DSB wrk2 binary:

```text
SHA256 dd1a7c23636edb127d698bec50a04c9afeb109503d66cf38107ab0ca7127a6bb
```

Source is `/users/tomislav/DeathStarBench/wrk2/src/wrk.c` and `wrk.h`.
`wrk-methodology.json` records source hashes and the inspected behavior.

- It schedules open-loop requests and supports more than one outstanding HTTP
  request per connection.
- It records latency from actual socket write to response, including HTTP
  connection/request queueing. It does not synthesize an arbitrary corrected
  latency value from the target arrival schedule.
- It resets the HDR histogram around 10 seconds after invocation. Thus a 30-second
  point's latency distribution is approximately its last 20 seconds; request
  counts/throughput cover the full invocation.
- This DSB fork's Thread Stats header is **99%**, not upstream wrk's Max. An older
  review incorrectly criticized that field; the review was corrected and the user
  informed. Do not repeat that criticism without reading this fork.
- The timeout counter samples old connection send timestamps and is not a count
  of distinct failed requests. Keep it separate from non-2xx/3xx responses.
- Latencies can exceed a nominal 1s internal RPC timeout because requests wait
  before handlers/RPCs begin. That alone is not a parser bug.

Keep configured offered RPS, actually sent requests, completed requests, completed
RPS, and successful RPS distinct. At high offered rates, sent minus completed can
be large. A zero HTTP error count does not resolve that difference. The analyzer
uses wrk's reported `Requests/sec` times successful fraction because the human
readable duration string is rounded (e.g. minutes). An older
`successful_rps_from_display_duration` is retained for provenance.

### 17.2 Collector and SDK counters

Receiver refusal percentage is `refused / (accepted + refused)`. It counts span
admission attempts; it is not directly a fraction of unique trace IDs lost.
Priority checkpoint refusal percentage is `hp_refused / (hp_admitted + hp_refused)`;
ordinary refusal percentage uses the LP counterparts. Always state the denominator.

Prometheus snapshots bracket the request window with additional capture time.
Priority logs are periodic, currently one second, with their own timestamps. SDK
periodic logs can be absent from the 2,000-line tail when refusal logging is heavy.
Reverse diagnostics are also sampled and periodic. Counter deltas from these
sources therefore must not be expected to match exactly per point.

Useful analyzer output is `periodic_counters`, with:

```text
deltas.sdk
deltas.reverse
deltas.priority
missing_logs
counter_resets
pod_restarts_changed
```

Check that all eight collector log windows are present before claiming no HP
refusals. SDK missing windows do not erase valid collector evidence; they do limit
claims about all SDK losses. At a completed ramp, final SDK logs after the quiet
period may cover all 13 services and allow lifetime reconciliation, as they did
for PB. Periodic reverse diagnostic field names are `leaf_rejects`, `checkpoints`,
`received`, and `local_checkpoints` in `BRIDGES_RT`, not invented `rtReject` fields.

Refusals between timed points can appear in lifetime counters but not in the sum
of point deltas. Keep both measurements. Never claim zero "through" a rate by
checking only its final point; scan every earlier point and cumulative counts.

### 17.3 CPU/memory/backend observations

Snapshots preserve per-pod UID, node, CPU nanoseconds, and working set from kubelet
stats. CPU differences are divided by the snapshot interval, not automatically
by exactly 30 seconds. Group summaries distinguish applications, collectors,
database/cache, and backend. Preserve pod identity when diagnosing a maximum.

First-ramp collector CPU observations were well below each 0.5-core cap, while
memory/queue pressure triggered refusals. Do not describe this end-to-end refusal
onset as demonstrated collector CPU saturation. Application and backend pressure
also affect completed throughput; the generator itself had ample CPU headroom.

Raw backend captures include Jaeger metrics on port 14269 and Elasticsearch node
thread-pool/JVM/process statistics. Check Jaeger queue drops/save errors and ES
write rejections before claiming storage retained every collector-accepted span.
Even zero counters are not an ID-level storage reconciliation.

### 17.4 Trace sampling and asynchronous indexing

Each point saves up to 100 Jaeger traces from a time-bounded search. Search results
can be partially indexed: one observed result initially had one span and later
had all 23 when fetched by ID. Immediate missing spans are not proof of collector
loss.

After all 16 timed points, the runner waits at least 30 seconds and checks eight
collector export queues, Jaeger queue length, and Elasticsearch pending writes,
with a maximum drain wait of 120 seconds. It then re-fetches saved trace IDs in
batches of 50, saving `settled-traces.json.gz` separately from the original sample.
If the immediate search was empty, it repeats the original time-window search in
`delayed-search/` and then fetches those IDs.

The first vanilla ramp's drain reached the 120-second limit with small remaining
queues/writes, so `trace-drain.json` has `queues_empty=false`. All 16 cohorts were
still re-fetched afterward. This is an explicit limitation, not grounds to silently
discard or replace the workload. PB drained successfully in about 30 seconds.
Later ramp drain results must be checked individually.

The trace cohorts are **bounded, nonrandom search samples**. They support payload
format and retained-structure inspection, not an unbiased global completeness or
reconstruction accuracy estimate. A sample of 100 complete traces at a point with
some collector refusals is possible due to time/selection correlation.

The analyzer prefers settled samples. It records missing parent references and
missing origin spans, but those counts are not reconstruction success rates.
The user is interested in whether lost ordinary spans can be recovered from
checkpoints; that end-to-end reconstruction assessment is still outstanding.

### 17.5 Audit limitations to preserve in the final report

The analyzer's top-level `audit.json` records some snapshot/reset/restart/sample
issues. At present it does **not** automatically promote every nested missing
periodic SDK log or `queues_empty=false` drain into that top-level list. Therefore
`issues: []` is not a claim that all telemetry windows were complete or every
queue drained. Inspect nested `points.json` fields and each `trace-drain.json`.

Likewise, `complete: true` means required campaign coverage and raw checks passed,
not that the reconstruction hypothesis was proven. Partial analysis while a gzip
sample is still being written can race that write; rerun after the case completes
if this happens, preserving the measurement. The full final analysis after all
cases complete avoids that normal in-flight race.

## 18. Artifact layout and provenance map

The experiment root is deliberately outside the repository:

```text
/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z
```

Its large `run/` and `smoke/` directories are symlinks to:

```text
/storage/tomislav-retctx-e2e/retctx-e2e-cpd2-6-inverse-20260915T041603Z/run
/storage/tomislav-retctx-e2e/retctx-e2e-cpd2-6-inverse-20260915T041603Z/smoke
```

Node-0 root storage has only about 9 GiB free; the dedicated `/storage` volume
has approximately 780 GiB free. Node-9's data volume was also checked and had
ample space. Check current usage before creating additional large artifacts.
`Path.rename` across root and `/storage` will fail with cross-device errors;
use a preserving `shutil.move`/copy-and-verify approach if moving between them.
Renaming two sibling run directories within `/storage` is fine.

| Artifact | Purpose |
| --- | --- |
| `plan.json` | Authoritative experiment settings and provenance gaps |
| `cases.json` | Variant names, build directories, pinned collector digest |
| `application-source-hashes.json` | 50 service/workflow/wiring Go hashes |
| `before/` | Original deployments/configs/pods, hardware/clock evidence, Git records |
| `builds/<kind>/manifest.yaml` | Final pinned Kubernetes manifest |
| `builds/<kind>/collector.yaml` | Exact collector configuration |
| `builds/<kind>/case.json` | Variant preparation metadata |
| `builds/<kind>/images.json` | Image build/push/digest records |
| `builds/<kind>/build-complete.json` | Build completion and manifest hash |
| `collector-build-status.json` | Good collector build status/toolchain |
| `prepare-status.json` | Preparation completion |
| `image-build-status.json` | All 52 application images complete |
| `smoke-complete.json` | All four smoke variants passed |
| `smoke-independent-audit.json` | Independent return-format and deployment checks |
| `run-provenance.json` | Original interrupted launch provenance; preserve |
| `run-provenance-corrected.json` | Current corrected runner launch provenance |
| `capture-revision-provenance.json` | Historical capture-fix provenance |
| `wrk-methodology.json` | Binary/source hashes and latency/timeout semantics |
| `generator-fd-check/` | Full worker/connection initialization check |
| `run-status.json` | Current authoritative runner stage |
| `monitor-status.json` | Independent monitor health |
| `run.pid`, `monitor.pid` | Current process IDs; verify command lines |
| `progress.json`, `STATUS.md` | Readable progress, refreshed about every 30s |
| `progress-events.jsonl` | Point-completion history |
| `analysis/` | Reproducible point/curve outputs and dated diagnostic reports |
| `WORK_STATE.md` | Working notes; earliest paragraphs are stale, later corrections supersede them |
| `HANDOFF.md` | Copy of this detailed handoff |
| `handoff-snapshot.json` | Machine-derived snapshot finalized with this document |

A completed case is named `run/<two-digit-repetition>-<kind>/`, e.g. `run/01-pb/`.
It contains `applied.yaml`, `ready-pods.json`, `deleted-resources.json`, `seed.log`,
bridge `discovery-node-*.json`, `warmup/`, 16 `rate-XXXXX/` directories,
`trace-drain.json`, `final/`, final trace capture, and `complete.json`.

A measured point contains:

```text
command.json             exact wrk arguments, seed, concurrency, FD limits, start
wrk.stdout               full raw output, HDR histogram, Sent/completed rates
wrk.stderr               thread initialization, seeds, diagnostics
result.json              parsed workload data and collector deltas
before/snapshot.json     parsed telemetry, CPU/restarts, collection errors
after/snapshot.json      same after the load interval
before|after/pods.json   UID/node/resource image mapping
before|after/*.txt.gz    raw collector Prometheus, SDK/collector logs, node/backend data
sample-traces.json.gz    original immediate Jaeger sample, if captured
settled-traces.json.gz   later ID-based requery, if captured
settled-query.json       requery metadata
delayed-search/          repeated original search if the initial one was empty
```

Query-error filenames are explicitly retained if capture fails. Consult the
runner source for all exact names instead of assuming absence of a sample means
zero traces were generated.

Do not discard old provenance because it has a failed state. The handoff's next
section identifies which failures are historical and which files are live.

## 19. Resolved failures and traps the next agent should not repeat

### 19.1 Go 1.27 collector runtime panic

The user explicitly wanted the collector rebuilt through its existing script:

```text
./build-and-push.sh 10.10.1.1:30000
```

Building with the environment's Go 1.27.1 succeeded and pushed an image, but
runtime configuration validation panicked in the old `SermoDigital/jose`
dependency at `crypto.RegisterHash(0)`. **That bad image was never deployed.**
The collector was rebuilt with `GOTOOLCHAIN=go1.24.13` using the same script. The
good pinned digest is recorded in section 7, and all four collector configs
passed actual binary validation. Generated application images retained Go 1.27.1.

If a future rebuild is necessary, preserve the distinction between the collector
toolchain and app toolchain. A successful Docker build/push alone is insufficient;
validate the collector binary/config before deploying. No rebuild is needed now.

### 19.2 Failed first attempt due to descriptor limits — excluded

The initial primary runner (old PID 1426737) began around 05:02 UTC and failed
at 4,800 around 05:15:47 when the inherited 1,024-descriptor soft limit could not
initialize all worker/Lua contexts:

```text
cannot open compose-post.lua: Too many open files
unable to create thread109 ... Too many open files
```

Its 14 completed points (2,000–4,600) were preserved, with generator errors already
visible at 4,600. They are **excluded from the primary five-repetition analysis**.
The archive is:

```text
run/01-v-interrupted-fdlimit-1789449826731845987
```

The current runner raises the FD soft limit to at least 16,384. A local loopback
HTTP fixture verified all 125 workers/1,250 connections with the original Lua;
peak observed descriptors were 1,263, exit status zero. This sent no extra DSB
traffic. Its artifacts are in `generator-fd-check/`; the temporary server exited.

The corrected campaign restarted from fresh state at **05:25:06 UTC**, with the
current runner/monitor PIDs. First vanilla successfully reached 4,800 and 5,000.
Do not count both the excluded attempt and corrected run as extra replications.

### 19.3 Historical handoff/capture helper states are not current failures

An earlier `finish_first_run_handoff.py` guard was intended to pause between runs
to add settled capture. It never performed that handoff because the old runner
failed first. Its `handoff-status.json` can say failed; that is historical.
**Do not relaunch this guard.** The similarly named detailed `HANDOFF.md` in this
request is a document, not that old process.

Other one-off scripts and status files include `capture_interrupted_run.py`,
`interrupted-capture-status.json`, `restart_corrected_campaign.py`,
`restart-status.json`, `restart.pid`, and old PID files. Those actions are done.
The restart helper initially encountered a cross-device rename while archiving
analysis into the `/storage` run tree; it was fixed using `shutil.move` and
completed. `logs/restart.log` therefore contains an old error followed by success.

Do not rerun the old one-off restart script merely because its filename sounds
useful. Use live `run-status.json`, current PIDs, and the runner's documented
resume behavior. Old monitor PID 1433140 and old handoff PID 1462694 are not the
current monitor or runner.

### 19.4 Initial trace samples looked incomplete before indexing settled

This was a capture timing issue, not evidence by itself of SDK loss. The corrected
runner waits after each full ramp and re-queries trace IDs, preserving both
immediate and settled samples. All 14 cohorts from the excluded attempt were
recovered before its backend was replaced. Do not repeat manual capture against
the wrong newly deployed backend and interpret missing old IDs as lost data.

### 19.5 Do not infer current behavior from old docs or headline counters

- Priority README/DESIGN describe old internal buffering; read active code.
- Some reverse docs say "not deployed" at the time they were written; the current
  campaign has rebuilt/deployed and passed smoke tests.
- Total span refusals do not reveal checkpoint refusals; use HP/LP counters.
- Zero last-point HP refusals do not imply zero earlier HP refusals.
- Low heap while shedding is not the unthrottled memory requirement.
- Offered 5,000 requests/s is not 5,000 completed requests/s.
- Empty top-level `audit.issues` is not full telemetry completeness.
- Immediate Jaeger search results are not settled traces or unbiased samples.
- The standalone generator campaign is complete and used a different collector
  configuration, backend, workload, and repetition count.

## 20. Monitoring and analysis commands for the next agent

After the shell setup in section 1, these commands are read-only or regenerate
analysis files; they do not send application load.

Inspect recent runner/monitor text logs:

```bash
tail -n 30 "$experiment_root/logs/run-corrected.log"
tail -n 30 "$experiment_root/logs/monitor-corrected.log"
```

Count complete measured points, excluding interrupted cases and in-flight result
files; summarize health and actual completion rates:

```bash
python - "$experiment_root" <<'PY'
import json
from pathlib import Path
import sys
r = Path(sys.argv[1])
total = 0
for case in sorted((r / 'run').iterdir()):
    if '-interrupted-' in case.name:
        continue
    rows = []
    for path in sorted(case.glob('rate-*/result.json')):
        row = json.loads(path.read_text())
        if 'kind' in row:
            rows.append(row)
    if not rows:
        continue
    total += len(rows)
    last = max(rows, key=lambda row: row['offered_rps'])
    print(case.name, len(rows), 'complete=', (case / 'complete.json').exists(),
          'last offered=', last['offered_rps'], 'completed/s=', last['completed_rps'],
          'HTTP errors=', sum(row['non_2xx_3xx'] for row in rows),
          'restarts=', sum(row['restarts_changed'] for row in rows))
print('Measured points:', total, '/ 320')
PY
```

Generate a provisional analysis (prefer after a complete case if a sample file is
currently being written):

```bash
python -B utils/analyze_dsb_sn_e2e.py --out "$experiment_root" --partial
```

The same command **without `--partial`** requires all 320 points and successful
`run-complete.json`. The monitor runs that automatically on completion. Partial
analysis can overwrite `analysis/points.json` and figures; its `complete` field
remains false. It does not erase the separately named diagnostic reports.

Inspect every point's per-class refusal counters directly from raw collector log
JSON, including the responsible nodes:

```bash
python - "$experiment_root" 01-pb <<'PY'
import gzip
import json
from pathlib import Path
import sys
case = Path(sys.argv[1]) / 'run' / sys.argv[2]
for point in sorted(case.glob('rate-*')):
    result_path = point / 'result.json'
    if not result_path.exists():
        continue
    result = json.loads(result_path.read_text())
    if 'kind' not in result:
        continue
    if result['kind'] == 'v':
        raise SystemExit('Vanilla has receiver counters, not HP/LP classification')
    pods = json.loads((point / 'after/pods.json').read_text())['items']
    totals = dict(hp_admitted=0, hp_refused=0, lp_admitted=0, lp_refused=0)
    refusing_nodes = []
    for pod in pods:
        name = pod['metadata']['name']
        if not name.startswith('otelcol-'):
            continue
        samples = []
        for side in ('before', 'after'):
            with gzip.open(point / side / f'logs-{name}.txt.gz', 'rt') as stream:
                lines = stream.read().splitlines()
            line = next(line for line in reversed(lines)
                        if 'priority_processor_metrics' in line)
            samples.append(json.loads(line[line.index('{'):]))
        delta = {key: samples[1][key] - samples[0][key] for key in totals}
        if any(value < 0 for value in delta.values()):
            raise ValueError((point, name, 'counter reset'))
        for key, value in delta.items():
            totals[key] += value
        if delta['hp_refused']:
            refusing_nodes.append(pod['spec']['nodeName'])
    denominator = totals['hp_admitted'] + totals['hp_refused']
    percent = 100 * totals['hp_refused'] / denominator if denominator else None
    print(result['offered_rps'], totals, 'HP refusal %=', percent,
          'HP refusing nodes=', refusing_nodes)
PY
```

For vanilla, use `before/after/snapshot.json` → `collectors` →
`otelcol_receiver_refused_spans_total`, mapped to nodes by `after/pods.json`.
Raw Prometheus files are available to verify the parsed values. Use only counters
with matching pod identities and report missing/reset windows.

Cluster inspection when needed:

```bash
kubectl -n dsb-sn get pods -o wide
kubectl -n dsb-sn get deployments,daemonsets,services
```

Reading saved manifests and snapshots is usually sufficient and minimizes extra
cluster activity. Do not run extra application requests, modify log verbosity,
or attach a competing collector load generator during the campaign.

## 21. What to do if the runner or monitor actually fails

No such failure was present at the final pre-handoff checks; this section is for
contingency, not an instruction to restart now.

1. Verify `run.pid` and `monitor.pid` against `/proc/<pid>/cmdline`. Read the live
   status and active logs. Identify whether the error is the runner, monitor,
   analyzer, deployment/readiness, seed step, wrk process, snapshot, or trace query.
2. Preserve all raw data, logs, source hashes, manifests, and the failed status.
   Distinguish an intentionally observed collector refusal from a harness failure.
   Do not discard an entire point just because it has an unfavorable result.
3. Ensure no orphaned wrk or other campaign process is still sending load. A
   second runner would race deployment resets and invalidate both experiments.
4. Fix only the actual harness/environment problem necessary to resume. Record
   source/configuration changes and their scope. Preserve application service
   hashes. Do not alter resource limits, admission policy, CPD, seeds, or durations
   to make a failing performance result look better.
5. Resume the same root with the runner's `run` mode. Completed cases are skipped;
   the interrupted case is archived and reruns from fresh state. Record a new
   provenance file and distinct log filename. The original 05:25 launch's files
   should remain unchanged.
6. Relaunch the file-based monitor if it exited, after the runner has written its
   new running/complete status. Update the correct PID files. The monitor reading
   a stale failed status immediately after launch would simply exit again.

The process invocation to resume, **only after establishing the existing runner
has exited and no load child remains**, is:

```text
/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u \
  /users/tomislav/blueprint-docc-mod/utils/run_dsb_sn_e2e.py run \
  --out /users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z
```

Use a detached process with a new log file and write its PID to `run.pid`, as was
done for the current launch. The monitor command is the same Python interpreter
with `-B -u <experiment_root>/monitor_e2e.py`, detached with its own log and PID.
Do not run smoke mode again on the active root as a casual health check: it
replaces resources and seeds application state.

If only the monitor fails while the runner is healthy, leave the runner alone.
Repair/relaunch only the monitor, or run the final analyzer manually after the
campaign completes. The monitor is not needed to keep load generation alive.

## 22. Remaining work and completion criteria

The active campaign should finish all **20 ramps / 320 primary points**. Continue
monitoring case completion, readiness, actual workload errors, collector and SDK
refusals, pod restarts, snapshot completeness, backend errors, and available disk.
All four variants need five repetitions. Do not treat the complete first round
as the whole task being finished.

When all runs finish:

1. Verify `run-status.json` and `run-complete.json`, and that every expected
   repetition/variant/rate tuple occurs once outside interrupted directories.
2. Verify each case's complete marker, fresh-state seed counts, workload seed,
   unchanged request format, FD limits, source hashes, and pinned manifest/image
   provenance.
3. Run/inspect the full analyzer and inspect the generated figure, not only its
   successful exit status.
4. Independently aggregate receiver refusals and HP/LP refusals across all nodes,
   rates, and repetitions. Keep per-point and lifetime counts distinct. Report
   first observed onset per repetition and variability rather than cherry-picking
   the first promising ramp.
5. Compare checkpoint-retention improvement against ordinary-span loss and actual
   application throughput/latency. Distinguish offered rate from completed rate.
   Report first-repetition observations as such until n=5 is available.
6. Inspect final SDK accounting for all services, empty buffers where available,
   priority counters, collector exporter failures, Jaeger drops/save errors,
   Elasticsearch rejections, and pod restarts. Flag missing diagnostic windows.
7. Inspect every `trace-drain.json` and settled capture; explicitly note the first
   vanilla drain deadline and any later failures. Do not hide those behind a
   top-level empty audit issue list.
8. Present retained-trace/payload validation separately from reconstruction
   accuracy. The current bounded Jaeger cohorts do not establish an unbiased
   global reconstruction success rate. Designing a stronger reconstruction
   evaluation is subsequent work to discuss with the user.
9. Provide self-contained final results with links to the report, CSV/JSON data,
   and plot. Mention the exact pipeline/resources, n=5, material deviations from
   the paper, and limitations needed to interpret the comparison.
10. Leave the final deployment available unless the user asks to tear it down.
    No new Git commit/push was requested; report the uncommitted additions if
    relevant and wait for the user's normal commit instruction.

Potential future work discussed but not part of the running matrix includes:
evaluating the other reverse probability policies; implementing the proposed
increasing-upstream-pressure mode; assessing reconstruction under measured loss;
and examining whether the controller can retain more ordinary spans without
sacrificing checkpoint protection. None should silently replace or change the
current comparison.

## 23. Important reference index

All relative source paths below are relative to
`/users/tomislav/blueprint-docc-mod`, unless marked collector repository.

| Topic | Reference |
| --- | --- |
| Overall session changes | `docs/dev/tomislav_retctx_changes.md` |
| SDK reverse lifecycle and wire format | `docs/dev/reverse_truss.md` |
| Forward CPD and window-sized Bloom | `docs/dev/randomized_checkpoint_distance.md` |
| Implemented probability modes | `docs/dev/reverse_checkpoint_probability.md` |
| Simulator primer including proposal | `docs/dev/reverse_probability_simulator_primer.md` |
| Current evaluation protocol | `docs/dev/dsb_sn_retctx_evaluation.md` |
| Lightweight generator recovery | `docs/dev/collector_load_generator.md` |
| Generator usage/distributions | `utils/spanload/README.md` |
| One-shot deployment options | `utils/README.md`, `utils/build_deploy_dsb.sh`, `utils/build_deploy_hotel.py` |
| Synthetic DAGs and concurrent fanout | `examples/leaf/FANOUT.md` |
| Hotel integration | `examples/dsb_hotel/README.md` |
| Live preparation/build/run/analyze | `utils/{prepare,build,run,analyze}_dsb_sn_e2e.py` |
| Actual priority algorithm | collector repository `processor/priorityprocessor/priority.go` |
| Collector config validation | collector repository `receiver/configdiscoveryreceiver/config.go` |
| Current status | experiment root `run-status.json`, `monitor-status.json`, `progress.json` |
| Corrected PB checkpoint findings | experiment root `analysis/pb-checkpoint-refusals.md` |
| PB low-rate diagnosis | experiment root `analysis/low-rate-refusals.md` |
| Per-node onset diagnosis | experiment root `analysis/refusals-by-node.md` |
| Completed collector-only report | `/users/tomislav/deployments/collector-load/spanload-dense-ramps-20260914T185629Z/RESULTS.md` |

## 24. Finalized handoff snapshot and machine-derived appendices

The appendices below are generated from the live status and saved raw artifacts
when this handoff is finalized. They are a point-in-time record. Re-read live
status before acting. No campaign settings are changed by creating this document.

<!-- TOMISLAV_HANDOFF_SNAPSHOT -->

### A. Live status at finalization

Captured at **2026-09-15T07:01:52.391964+00:00**. Completed **80/320 measured points** and **5/20 full ramps**.

Live runner: repetition **2**, variant **cgpb**, stage **warmup**, offered rate **not yet measuring**.

```json
{
  "run": {
    "state": "running",
    "mode": "run",
    "repetition": 2,
    "kind": "cgpb",
    "stage": "warmup",
    "updated": "2026-09-15T07:01:03.606072+00:00"
  },
  "monitor": {
    "state": "watching",
    "updated": "2026-09-15T07:01:43.526718+00:00",
    "runner_pid": 1476294
  },
  "processes": {
    "run": {
      "pid": 1476294,
      "cmdline": "/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u /users/tomislav/blueprint-docc-mod/utils/run_dsb_sn_e2e.py run --out /users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z "
    },
    "monitor": {
      "pid": 1476307,
      "cmdline": "/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u /users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z/monitor_e2e.py "
    }
  }
}
```

All 50 recorded application source hashes still match. Free space: root 9.14 GiB; /storage 781.26 GiB.

### B. Case progress and measurement health

| Case | Complete | Points | Latest offered/s | Peak completed/s | HTTP errors | Socket errors | Restart events |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 01-cgpb | True | 16 | 5000 | 3342.99 | 0 | 0 | 0 |
| 01-pb | True | 16 | 5000 | 3343.34 | 0 | 0 | 0 |
| 01-sb | True | 16 | 5000 | 3330.26 | 0 | 0 | 0 |
| 01-v | True | 16 | 5000 | 3453.58 | 0 | 0 | 0 |
| 02-pb | True | 16 | 5000 | 3339.69 | 0 | 0 | 0 |

These are per-case observations, not an n=5 aggregate. The full checkpoint-retention table below includes every completed point from every begun case at snapshot time.

### C. First-repetition completed application throughput

| Offered requests/s | Vanilla completed/s | PB completed/s | CGPB completed/s | SB completed/s |
| ---: | ---: | ---: | ---: | ---: |
| 2000 | 1996.07 | 1995.58 | 1995.48 | 1992.13 |
| 2200 | 2190.99 | 2190.79 | 2191.87 | 2186.78 |
| 2400 | 2369.32 | 2370.34 | 2367.78 | 2368.21 |
| 2600 | 2568.65 | 2576.77 | 2567.90 | 2567.96 |
| 2800 | 2784.58 | 2776.44 | 2791.60 | 2784.93 |
| 3000 | 2949.45 | 2954.46 | 2950.88 | 2955.50 |
| 3200 | 3153.68 | 3152.44 | 3147.41 | 3152.44 |
| 3400 | 3302.89 | 3343.34 | 3342.99 | 3330.26 |
| 3600 | 3453.58 | 3309.74 | 3285.22 | 3302.53 |
| 3800 | 3202.03 | 3144.82 | 3146.32 | 3093.07 |
| 4000 | 2994.93 | 2946.85 | 2943.52 | 2920.86 |
| 4200 | 2828.79 | 2790.72 | 2799.56 | 2774.12 |
| 4400 | 2685.20 | 2632.26 | 2630.25 | 2603.39 |
| 4600 | 2523.51 | 2488.07 | 2486.88 | 2460.82 |
| 4800 | 2419.49 | 2367.17 | 2382.22 | 2340.61 |
| 5000 | 2312.39 | 2294.25 | 2287.75 | 2259.04 |

### D. Checkpoint refusals in all begun bridge ramps

A zero below is the counter delta for that point window, not a statement that all earlier points were zero. All values are percentages of checkpoint arrival attempts in the periodic priority window.

| Case | First measured HP refusal rate | Peak HP refusal % in completed points | HP refusing nodes across those points |
| --- | ---: | ---: | --- |
| 01-cgpb | 3800 | 4.177% | node-1 |
| 01-pb | 3400 | 13.563% | node-1 |
| 01-sb | 3400 | 11.658% | node-1 |
| 02-pb | 3400 | 9.565% | node-1 |

| Case | Offered/s | HP admitted | HP refused | HP refusal % | Ordinary refused |
| --- | ---: | ---: | ---: | ---: | ---: |
| 01-cgpb | 2000 | 451,367 | 0 | 0.000% | 291,827 |
| 01-cgpb | 2200 | 496,683 | 0 | 0.000% | 354,594 |
| 01-cgpb | 2400 | 537,022 | 0 | 0.000% | 462,110 |
| 01-cgpb | 2600 | 579,652 | 0 | 0.000% | 540,603 |
| 01-cgpb | 2800 | 631,237 | 0 | 0.000% | 638,607 |
| 01-cgpb | 3000 | 669,296 | 0 | 0.000% | 759,056 |
| 01-cgpb | 3200 | 714,854 | 0 | 0.000% | 830,614 |
| 01-cgpb | 3400 | 758,273 | 0 | 0.000% | 886,048 |
| 01-cgpb | 3600 | 750,520 | 0 | 0.000% | 900,084 |
| 01-cgpb | 3800 | 695,002 | 21,674 | 3.024% | 940,483 |
| 01-cgpb | 4000 | 642,659 | 28,015 | 4.177% | 885,334 |
| 01-cgpb | 4200 | 637,637 | 0 | 0.000% | 834,619 |
| 01-cgpb | 4400 | 593,195 | 0 | 0.000% | 717,210 |
| 01-cgpb | 4600 | 571,262 | 0 | 0.000% | 669,537 |
| 01-cgpb | 4800 | 543,721 | 0 | 0.000% | 633,748 |
| 01-cgpb | 5000 | 528,964 | 0 | 0.000% | 594,739 |
| 01-pb | 2000 | 449,274 | 0 | 0.000% | 300,427 |
| 01-pb | 2200 | 495,267 | 0 | 0.000% | 344,117 |
| 01-pb | 2400 | 535,422 | 0 | 0.000% | 381,495 |
| 01-pb | 2600 | 582,974 | 0 | 0.000% | 433,831 |
| 01-pb | 2800 | 625,413 | 0 | 0.000% | 473,134 |
| 01-pb | 3000 | 671,746 | 0 | 0.000% | 533,335 |
| 01-pb | 3200 | 712,808 | 0 | 0.000% | 642,071 |
| 01-pb | 3400 | 665,009 | 92,440 | 12.204% | 1,039,923 |
| 01-pb | 3600 | 648,733 | 101,794 | 13.563% | 1,150,101 |
| 01-pb | 3800 | 681,193 | 33,832 | 4.732% | 1,014,502 |
| 01-pb | 4000 | 680,302 | 0 | 0.000% | 581,738 |
| 01-pb | 4200 | 632,972 | 0 | 0.000% | 585,326 |
| 01-pb | 4400 | 603,716 | 0 | 0.000% | 570,977 |
| 01-pb | 4600 | 569,914 | 0 | 0.000% | 572,211 |
| 01-pb | 4800 | 549,709 | 0 | 0.000% | 621,869 |
| 01-pb | 5000 | 536,627 | 0 | 0.000% | 542,034 |
| 01-sb | 2000 | 450,712 | 0 | 0.000% | 385,261 |
| 01-sb | 2200 | 495,722 | 0 | 0.000% | 412,864 |
| 01-sb | 2400 | 536,177 | 0 | 0.000% | 440,392 |
| 01-sb | 2600 | 581,527 | 0 | 0.000% | 468,690 |
| 01-sb | 2800 | 632,967 | 0 | 0.000% | 517,324 |
| 01-sb | 3000 | 657,294 | 0 | 0.000% | 528,560 |
| 01-sb | 3200 | 713,599 | 0 | 0.000% | 807,714 |
| 01-sb | 3400 | 697,341 | 51,595 | 6.889% | 974,374 |
| 01-sb | 3600 | 659,391 | 87,018 | 11.658% | 1,161,259 |
| 01-sb | 3800 | 687,644 | 14,133 | 2.014% | 1,031,386 |
| 01-sb | 4000 | 666,384 | 0 | 0.000% | 931,465 |
| 01-sb | 4200 | 636,262 | 0 | 0.000% | 767,517 |
| 01-sb | 4400 | 600,607 | 0 | 0.000% | 713,239 |
| 01-sb | 4600 | 565,880 | 0 | 0.000% | 670,045 |
| 01-sb | 4800 | 533,027 | 0 | 0.000% | 637,019 |
| 01-sb | 5000 | 524,202 | 0 | 0.000% | 620,232 |
| 02-pb | 2000 | 451,031 | 0 | 0.000% | 219,619 |
| 02-pb | 2200 | 496,151 | 0 | 0.000% | 341,561 |
| 02-pb | 2400 | 531,582 | 0 | 0.000% | 372,233 |
| 02-pb | 2600 | 580,525 | 0 | 0.000% | 434,910 |
| 02-pb | 2800 | 630,010 | 0 | 0.000% | 489,009 |
| 02-pb | 3000 | 671,534 | 0 | 0.000% | 549,813 |
| 02-pb | 3200 | 709,873 | 0 | 0.000% | 817,668 |
| 02-pb | 3400 | 681,150 | 72,045 | 9.565% | 1,093,066 |
| 02-pb | 3600 | 702,891 | 55,907 | 7.368% | 1,241,986 |
| 02-pb | 3800 | 693,846 | 18,011 | 2.530% | 993,432 |
| 02-pb | 4000 | 664,122 | 0 | 0.000% | 638,881 |
| 02-pb | 4200 | 631,576 | 0 | 0.000% | 656,755 |
| 02-pb | 4400 | 604,475 | 0 | 0.000% | 655,603 |
| 02-pb | 4600 | 570,341 | 0 | 0.000% | 538,835 |
| 02-pb | 4800 | 550,554 | 0 | 0.000% | 479,169 |
| 02-pb | 5000 | 527,356 | 0 | 0.000% | 441,609 |

### E. First-repetition per-node first-refusal rates, refreshed

| Node | Vanilla any | PB ordinary | PB checkpoint | CGPB ordinary | CGPB checkpoint | SB ordinary | SB checkpoint |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| node-1 | 2800 | 2000 | 3400 | 2000 | 3800 | 2000 | 3400 |
| node-2 | 3400 | 3400 | None observed | 3600 | None observed | 3400 | None observed |
| node-3 | 3400 | 3600 | None observed | 4000 | None observed | 3600 | None observed |
| node-4 | 3000 | 3200 | None observed | 2000 | None observed | 2000 | None observed |
| node-5 | 4200 | None observed | None observed | None observed | None observed | None observed | None observed |
| node-6 | 3200 | 3400 | None observed | 4000 | None observed | 3600 | None observed |
| node-7 | 3600 | 2200 | None observed | 2000 | None observed | 2000 | None observed |
| node-8 | 4400 | None observed | None observed | None observed | None observed | None observed | None observed |

Coverage is the latest point for each case in appendix B. Refusals need not persist at every higher offered rate.

### F. Capture limitations and final SDK checks

| Case | Settled point samples | Drain queues empty | Points missing some SDK metric windows | Points missing priority windows |
| --- | ---: | --- | ---: | ---: |
| 01-cgpb | 16/16 | True | 11 | 0 |
| 01-pb | 16/16 | True | 7 | 0 |
| 01-sb | 16/16 | True | 11 | 0 |
| 01-v | 16/16 | False | 10 | 0 |
| 02-pb | 16/16 | True | 9 | 0 |

A pending drain/capture on an active run is normal. Missing periodic SDK windows are documented even if the top-level analysis audit reports no issues.

- 01-cgpb: final checkpoint accounting available for 13 service pods; cumulative SDK checkpoint drops 54,157; reconciliation failures: none.
- 01-pb: final checkpoint accounting available for 13 service pods; cumulative SDK checkpoint drops 232,935; reconciliation failures: none.
- 01-sb: final checkpoint accounting available for 13 service pods; cumulative SDK checkpoint drops 164,129; reconciliation failures: none.
- 02-pb: final checkpoint accounting available for 13 service pods; cumulative SDK checkpoint drops 149,517; reconciliation failures: none.

These final counts include between-point traffic and initialization/warmup; do not directly compare their totals to sums of timed-window deltas.

### G. Exact plan JSON

```json
{
  "application": "DSB Social Network",
  "variants": [
    "v",
    "pb",
    "cgpb",
    "sb"
  ],
  "namespace": "dsb-sn",
  "ramp_rates": [
    2000,
    2200,
    2400,
    2600,
    2800,
    3000,
    3200,
    3400,
    3600,
    3800,
    4000,
    4200,
    4400,
    4600,
    4800,
    5000
  ],
  "seconds_per_rate": 30,
  "warmup_rps": 100,
  "warmup_seconds": 100,
  "repetitions": 5,
  "seeds": [
    1001,
    1002,
    1003,
    1004,
    1005
  ],
  "fresh_state_per_run": true,
  "bridge_cpd_min": 2,
  "bridge_cpd_max": 6,
  "reverse_policy": "inverse_depth",
  "leaf_rejection": "every unscheduled non-root server leaf; original TTL-selected checkpoints protected",
  "sampler_ratio": 1,
  "sdk_export_retry": false,
  "collector_cpu": "500m",
  "collector_memory": "256Mi",
  "collector_gomemlimit": "230MiB",
  "collector_gomaxprocs": "runtime default, as in controlled manifests",
  "collector_nodes": [
    "node-1",
    "node-2",
    "node-3",
    "node-4",
    "node-5",
    "node-6",
    "node-7",
    "node-8"
  ],
  "soft_percent": 50,
  "hard_percent": 70,
  "priority_cp_safety_factor": 1,
  "batch_size": 8192,
  "batch_timeout": "200ms",
  "exporter": "OTLP to Jaeger/Elasticsearch on node-9",
  "collector_build_toolchain": "go1.24.13",
  "app_cpus": 8,
  "cache_cpus": 4,
  "db_cpus": "8 or 16, per controlled node-pinning",
  "jaeger_cpus": 24,
  "elasticsearch_cpus": 8,
  "paper": "/users/tomislav/bridges.pdf",
  "paper_sha256": "5561af7d07c0b5eb362dfb53d2c2ecec1d90e348dd855676ce96798a1b8daa71",
  "timing_provenance": "historical five-round 2k-5k schedule; paper section 5.2 has missing values",
  "resource_provenance": "paper section 5.1 plus July controlled manifests",
  "deviations_from_old_ctl": [
    "active OTLP receiver retains priority metadata",
    "priority pipeline includes batch, as described in paper",
    "eight collector nodes, matching application nodes in paper",
    "collector logs at info for both policies",
    "random 2-6 and inverse-depth reverse returns per current request"
  ],
  "warmup_connections": 10,
  "warmup_threads": 1,
  "raw_data_storage": "/storage/tomislav-retctx-e2e/retctx-e2e-cpd2-6-inverse-20260915T041603Z",
  "generator_min_file_descriptors": 16384,
  "post_ramp_trace_capture": {
    "minimum_quiet_seconds": 30,
    "maximum_drain_seconds": 120,
    "recheck_id_batch": 50,
    "repeat_empty_search_after_drain": true
  }
}
```

### H. Exact collector YAML, vanilla and PB

**v**

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        include_metadata: true
processors:
  batch:
    send_batch_size: 8192
    timeout: 200ms
  memory_limiter:
    check_interval: 100ms
    limit_percentage: 70
    spike_limit_percentage: 20
exporters:
  otlp:
    endpoint: jaeger-v-esrtx20260915t041603z-ctr:4317
    tls:
      insecure: true
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 0s
    sending_queue:
      enabled: true
      num_consumers: 10
      queue_size: 1000
      block_on_overflow: false
service:
  telemetry:
    logs:
      level: info
    metrics:
      level: detailed
      readers:
      - pull:
          exporter:
            prometheus:
              host: 0.0.0.0
              port: 8888
  pipelines:
    traces:
      receivers:
      - otlp
      exporters:
      - otlp
      processors:
      - memory_limiter
      - batch
```

**pb**

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        include_metadata: true
  configdiscovery:
    endpoint: :8080
    config_map:
      cpd_min: 2
      cpd_max: 6
      reverse_policy: inverse_depth
processors:
  batch:
    send_batch_size: 8192
    timeout: 200ms
  priority:
    check_interval: 100ms
    soft_percentage: 50
    hard_percentage: 70
    cp_safety_factor: 1
    force_gc: true
    gc_soft_interval: 1s
    gc_ultrasoft_interval: 0s
exporters:
  otlp:
    endpoint: jaeger-pb-esrtx20260915t041603z-ctr:4317
    tls:
      insecure: true
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 0s
    sending_queue:
      enabled: true
      num_consumers: 10
      queue_size: 1000
      block_on_overflow: false
  debug/config:
    verbosity: basic
service:
  telemetry:
    logs:
      level: info
    metrics:
      level: detailed
      readers:
      - pull:
          exporter:
            prometheus:
              host: 0.0.0.0
              port: 8888
  pipelines:
    traces:
      receivers:
      - otlp
      exporters:
      - otlp
      processors:
      - priority
      - batch
    logs/configdiscovery:
      receivers:
      - configdiscovery
      processors: []
      exporters:
      - debug/config
```

CGPB and SB use the same bridge processor/discovery settings, with their own Jaeger endpoint names. Their exact files remain under builds/cgpb and builds/sb.

### I. Repository state at handoff

**blueprint**: `/users/tomislav/blueprint-docc-mod`; branch `retctx-reject-impl`; HEAD `6997134e05ed87cb0ec902b85076ea7f616bbd51`.

```text
 M docs/dev/reverse_checkpoint_probability.md
 M docs/dev/tomislav_retctx_changes.md
?? docs/dev/dsb_sn_retctx_evaluation.md
?? docs/dev/retctx_agent_handoff_2026-09-15.md
?? docs/dev/reverse_probability_simulator_primer.md
?? examples/dsb_sn/build_cgpb_e2e_20260915t041603z/
?? examples/dsb_sn/build_cgpb_es_k8s_20260910/
?? examples/dsb_sn/build_cgpb_es_ttl_20260910/
?? examples/dsb_sn/build_cgpb_es_window_20260910/
?? examples/dsb_sn/build_pb_e2e_20260915t041603z/
?? examples/dsb_sn/build_sb_e2e_20260915t041603z/
?? examples/dsb_sn/build_v_e2e_20260915t041603z/
?? examples/dsb_sn/node-pinning-cgpb_e2e_20260915t041603z.yaml
?? examples/dsb_sn/node-pinning-pb_e2e_20260915t041603z.yaml
?? examples/dsb_sn/node-pinning-sb_e2e_20260915t041603z.yaml
?? examples/dsb_sn/node-pinning-v_e2e_20260915t041603z.yaml
?? utils/__pycache__/analyze_dsb_sn_e2e.cpython-310.pyc
?? utils/__pycache__/analyze_spanload_suite.cpython-310.pyc
?? utils/__pycache__/build_deploy_hotel.cpython-310.pyc
?? utils/__pycache__/build_dsb_sn_e2e.cpython-310.pyc
?? utils/__pycache__/check_spanload_capacity.cpython-310.pyc
?? utils/__pycache__/checkpoint_distance.cpython-310.pyc
?? utils/__pycache__/generate_spanload_distributions.cpython-310.pyc
?? utils/__pycache__/inject_perf_env.cpython-310.pyc
?? utils/__pycache__/prepare_dsb_sn_e2e.cpython-310.pyc
?? utils/__pycache__/reverse_policy.cpython-310.pyc
?? utils/__pycache__/run_dsb_sn_e2e.cpython-310.pyc
?? utils/__pycache__/run_spanload_ramp.cpython-310.pyc
?? utils/__pycache__/test_build_deploy_hotel.cpython-310.pyc
?? utils/__pycache__/test_checkpoint_distance.cpython-310.pyc
?? utils/__pycache__/test_generate_spanload_distributions.cpython-310.pyc
?? utils/__pycache__/test_reverse_policy.cpython-310.pyc
?? utils/__pycache__/test_run_spanload_ramp.cpython-310.pyc
?? utils/analyze_dsb_sn_e2e.py
?? utils/build_dsb_sn_e2e.py
?? utils/prepare_dsb_sn_e2e.py
?? utils/run_dsb_sn_e2e.py
?? utils/test_analyze_dsb_sn_e2e.py
?? utils/test_run_dsb_sn_e2e.py
```

**collector**: `/users/tomislav/opentelemetry-collector-contrib`; branch `retctx-reject-impl`; HEAD `ca8f540bd742378541d4fde6e00349c61e72085f`.

```text
(working tree clean)
```

No new commit or Git push was made for this handoff. The existing campaign processes remained running throughout document generation.

### J. Handoff copies and data

Canonical document:

`/users/tomislav/blueprint-docc-mod/docs/dev/retctx_agent_handoff_2026-09-15.md`

Identical copies are written to:

- `/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z/HANDOFF.md`
- `/users/tomislav/RETCTX_AGENT_HANDOFF_2026-09-15.md`

Machine-readable snapshot: `/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z/handoff-snapshot.json`.

The next agent should start with section 1, then re-read the live status. This document preserves the prior conversation; it does not stop or replace the active experiment.
