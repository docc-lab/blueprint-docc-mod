# Tracing through gRPC interceptors instead of wrapper services

Branch: `retctx-grpc-interceptors` (created from `retctx-reject-impl` at `d0f40602`).

This document has three parts:

1. **Plan**: what we are changing and why, written before any code.
2. **Testing**: the test scripts in this directory and how to run them by hand.
3. **Log**: what was actually done, in order, including the things that went wrong.

---

## Part 1: Plan

### The goal

Today, tracing a service means wrapping it in two generated Go "wrapper" services, one on the
caller side and one on the callee side. The wrappers change the service's interface (every method
gets an extra argument and an extra return value), and gRPC then ships that changed interface.

We want gRPC services to be traced with gRPC interceptors instead. The service interface stays
exactly as the application defines it, and the trace context rides in gRPC metadata. The
tracing behaviour (spans, attributes, the bridges' checkpoint logic) should stay effectively the
same.

### How it works today

`opentelemetry.Instrument(spec, service, collector)` adds two nodes around `service`:

* a **server wrapper** (`plugins/opentelemetry/ir_ot_server.go`), which sits between the gRPC
  server and the real service implementation, and
* a **client wrapper** (`plugins/opentelemetry/ir_ot_client.go`), which sits between the caller
  and the gRPC client.

The server wrapper changes every method from

    Method(ctx, args...) (rets..., err)

to

    Method(ctx, args..., traceCtx string) (rets..., retCtx string, err)

and `grpc.Deploy` then generates protobuf messages for that changed signature. So `traceCtx`
(the forward trace context: span context plus baggage, as JSON) travels as an extra field of the
request message, and `retCtx` (the bridges' reverse context) travels as an extra field of the
response message.

What the **server wrapper** does on every call:

1. If `traceCtx` starts with `U:`, the trace was not sampled upstream. Restore the remote span
   context, call the service, and return. Nothing else happens.
2. Decode the JSON carrier into a remote span context plus baggage, and put both on `ctx`.
3. If the baggage has `__seq` (the caller's child ordinal), put it on `ctx` as `"seqNum"` so the
   bridge processor sees it when the span starts.
4. Start the server span `<Service>Server_<Method>` with tracer `<Service>`.
5. Merge baggage that the processor attached to the new span (`__bag.` / `__bagdel.`
   attributes and `backend.AppendSpanBaggage`) and put it back on `ctx`.
6. Put per-request state on `ctx` for the client side to use: a child counter
   (`"childCount"`), and depending on the variant, end-event lists, mutexes, critical-path
   trackers and the return-merge accumulator (`backend.WithRetMerge`).
7. Call the service. Record the error on the span if there is one.
8. Set the `childCount` attribute (and for SB, the `remEndEvents` attribute).
9. For bridges with `REVERSE_TRUSS=on`: compute `retCtx` from the merged children and
   `backend.PrepareCheckpoint`, and return it.

What the **client wrapper** does on every call:

1. If the current span is not sampled, send the compact `U:<traceid><spanid>` carrier and skip
   everything else.
2. Copy the baggage from `ctx`.
3. Take the next child ordinal from the parent's `"childCount"`, put it on `ctx` as `"seqNum"`
   and in the baggage as `__seq`.
4. Start the client span `<Service>Client_<Method>`.
5. Merge span baggage (as in the server), encode the carrier with
   `backend.EncodeTraceCarrier`, and make the call with it.
6. For bridges: run `backend.PrepareCheckpoint` on the returned `retCtx`, add what is left to
   the parent's merge (`backend.AddToMerge`), and record end events.

There are four versions of each wrapper (vanilla, PB, CGPB, SB). The build picks one through the
`OT_BRIDGE` environment variable. The span processor that the tracer uses is picked separately at
run time through `BRIDGE_KIND` (`runtime/plugins/otelcol/trace.go`); that part does not change.

### The new design in one paragraph

The gRPC plugin learns to install interceptors: a list of unary server interceptors on the gRPC
server and a list of unary client interceptors on the gRPC client. The OpenTelemetry plugin gets
a new wiring call, `opentelemetry.InstrumentGRPC`, that registers a tracing server interceptor and
a tracing client interceptor for a gRPC-deployed service. The interceptors do the same steps as
the wrappers above, but read the trace context from request metadata and send `retCtx` back in
response trailers. The service interface is untouched. The interceptor code lives in a normal Go
runtime package, not in code templates.

### Step 1: interceptor support in the gRPC plugin

**Only unary interceptors.** Blueprint's gRPC plugin generates one request/one response per
method, never streams, so stream interceptors are not needed.

**Wiring API** (in `plugins/grpc/wiring.go`):

    grpc.AddServerInterceptor(spec, serviceName, interceptorName)
    grpc.AddClientInterceptor(spec, serviceName, interceptorName)

`interceptorName` is the name of another node in the wiring spec. These calls only record the
interceptor as a property on the service's gRPC nodes, so they work whether they are called
before or after `grpc.Deploy`. When the gRPC server (or client) node is built, it looks up the
recorded interceptor nodes and adds them as dependencies. Order of the calls is the order in
which the interceptors run (first added = outermost).

**What an interceptor node is.** Any IR node that can be instantiated (`golang.Instantiable`)
and whose instance is a `grpc.UnaryServerInterceptor` (or `grpc.UnaryClientInterceptor`)
value. These are the standard grpc-go function types, so the gRPC plugin does not need to know
anything about tracing. The plugin defines two small interfaces that such nodes implement:

    type ServerInterceptor interface { golang.Instantiable; ImplementsGRPCServerInterceptor() }
    type ClientInterceptor interface { golang.Instantiable; ImplementsGRPCClientInterceptor() }

**Generated code changes** (`plugins/grpc/grpccodegen`):

* Server handler constructor gets a trailing variadic parameter:

      func New_X_GRPCServerHandler(ctx, service X, serverAddress string,
          interceptors ...grpc.UnaryServerInterceptor)

  and `Run` creates the server with `grpc.ChainUnaryInterceptor(interceptors...)` when the list
  is not empty.
* Client constructor: today it is `New_X_GRPCClient(ctx, serverAddress string, timeout ...string)`.
  The generated DI code is its only caller and always passes exactly one timeout, so the timeout
  becomes a plain `timeout string` and the variadic slot is used for interceptors:

      func New_X_GRPCClient(ctx, serverAddress string, timeout string,
          interceptors ...grpc.UnaryClientInterceptor)

  The dial adds `grpc.WithChainUnaryInterceptor(interceptors...)` when the list is not empty.

**A small change in the golang plugin.** When the generated DI code fetches a constructor
argument from the namespace, it declares a local variable of the argument node's type. For nodes
that are not services, that type currently defaults to `string` (which is right for addresses
and config values). Interceptor nodes are not services, so they would be fetched as `string`
and fail at run time. Fix: an optional interface

    type TypedInstance interface { InstanceType() gocode.TypeName }

and `DeclareConstructor` uses that type when the node implements it. Nodes that do not implement
it behave exactly as before.

**Compatibility.** A service with no interceptors gets the same behaviour as today. The
generated text changes slightly (new parameter, a length check), so the regression test is: build
an existing spec before and after, diff the generated code, and check the diff is only those
lines; then make sure it compiles and runs.

(2026-10-13, later: the generated server also turns on gRPC stream workers, reused goroutines for
incoming calls, set by `GRPC_STREAM_WORKERS` at run time, default 32, 0 for off. Not part of the
plan; it came out of profiling. See Part 3, "Stack growth".)

### Step 2: tracing interceptors

**Runtime package**: `runtime/plugins/opentelemetry/interceptors` (Go package `interceptors`).
Two constructors:

    func NewServerInterceptor(ctx, tracer backend.Tracer, kind string) (grpc.UnaryServerInterceptor, error)
    func NewClientInterceptor(ctx, tracer backend.Tracer, kind string) (grpc.UnaryClientInterceptor, error)

`kind` is the bridge kind (`v`, `pb`, `cgpb`, `sb`). It comes from `OT_BRIDGE` at build time,
same as the wrapper templates today, and is baked into the generated constructor call as a
string. (Wiring arguments can replace the environment variable later.)

**Wiring**: `opentelemetry.InstrumentGRPC(spec, serviceName, collectorName)` defines two nodes,
`<service>.server.ot_interceptor` and `<service>.client.ot_interceptor`, and registers them with
`grpc.AddServerInterceptor` / `grpc.AddClientInterceptor`. The service must be deployed with
`grpc.Deploy`. Services deployed over HTTP (the SN entry service `wrk2api`) keep using
`opentelemetry.Instrument`.

**Default and fallback.** Once the interceptors work, the DSB specs call `InstrumentGRPC` for
every gRPC service. Setting `OT_GRPC_MODE=wrapper` at build time makes `InstrumentGRPC` fall back
to the old wrappers, so the two can be compared with the same spec.

**Where things travel:**

| what | wrappers (today) | interceptors |
|---|---|---|
| forward trace context | extra `traceCtx` field in the request message | request metadata key `bp-trace-ctx` |
| reverse context | extra `retCtx` field in the response message | response trailer key `bp-ret-ctx` |

Both strings are plain ASCII already (`traceCtx` is JSON or the `U:` form, `retCtx` is base64),
so they can go in normal (non `-bin`) metadata without extra encoding.

> **Changed after profiling (see the log, "Profiling: where the interceptors' extra CPU went"):**
> the forward context no longer travels as the wrappers' JSON. As a header that JSON was the
> biggest single cost, so between interceptors it now travels in a compact binary form
> (`runtime/plugins/opentelemetry/interceptors/carrier.go`, 28 bytes instead of 150 for vanilla)
> under the binary metadata key `bp-trace-ctx-bin`. It decodes to exactly what the JSON decodes to.

**Span and tracer names stay the same.** With interceptors, gRPC deploys the plain interface, so
the full method name is `/grpc.<Service>/<Method>`. From it the interceptors rebuild exactly the
names the wrappers use today:

| | tracer name | span name |
|---|---|---|
| server | `<Service>` | `<Service>Server_<Method>` |
| client | `<Service>_OTServerWrapperInterface` | `<Service>Client_<Method>` |

(The client tracer name is odd, but it is what the wrappers produce, so we keep it.)

**Per-request state on `ctx` stays the same.** The interceptors put the same values under the
same keys (`"childCount"`, `"seqNum"`, `"endEvents"`, `"childrenMutex"`, the return-merge
accumulator, ...). This matters because a wrapped HTTP service (`wrk2api`) calls
interceptor-traced gRPC services: its server wrapper sets up the state and the client
interceptor uses it. So mixed setups work.

**Known differences we accept but will measure:**

* The trace context moves from a protobuf field into an HTTP/2 header (HPACK-compressed). The
  bytes on the wire differ.
* The server span now also covers Blueprint's conversion of the protobuf request into Go
  arguments (and back), because the interceptor wraps the whole gRPC handler. The client span no
  longer covers the client's argument conversion. Span durations shift a little.
* Error case: today, when the service returns an error, the gRPC handler drops the whole response,
  `retCtx` included. Trailers would survive the error. To keep behaviour identical, the server
  interceptor does **not** send `retCtx` when the call failed.

**Order of work:**

1. Vanilla interceptors, validated against the vanilla wrappers.
2. Then PB, CGPB and SB, each validated against its wrapper.

### How we decide it works (validation)

Decisions are driven by tests, not by re-reading code.

1. **Codegen regression (Step 1).** Build `docker_v_es_nw` before and after the change and diff
   the generated code. Only the expected lines may differ.
2. **Go tests for the runtime package.** Real gRPC over an in-memory connection (`bufconn`),
   an in-memory span recorder, a small call tree. They check span names, kinds, parent/child
   links, attributes, the `U:` path, `retCtx` round trips and the per-request state.
3. **End-to-end A/B on the real app.** Build DSB SocialNetwork no-work twice, once with the
   wrappers (`OT_GRPC_MODE=wrapper`) and once with interceptors. Run all its processes locally
   against a small fake collector that records every exported span, send the same requests to
   both, and compare the traces:
   * vanilla: every trace must have the same span names, kinds, tree shape and attribute keys
     and values (ignoring IDs, timestamps and durations);
   * bridges: additionally the same checkpoint and reverse-context behaviour (which spans become
     checkpoints, which attributes the processors emit), compared per trace after normalising IDs.

---

## Part 2: Testing

Test scripts live in this directory (`utils/grpc_interceptors/`), except Go unit tests, which
live next to the package they test. Every script has a usage comment at the top; this section is
the overview.

Work directory used in the examples: `~/grpc-interceptors-work` (anything outside the repo works).

### Requirements

Everything `utils/setup_environment.sh` installs (Go, protoc, wrk2, ...), plus `goimports`:

    go install golang.org/x/tools/cmd/goimports@latest

No docker or kubernetes is needed. `run_local.sh` uses `unshare -rn` (a private network
namespace, no root needed).

### The scripts

| script | what it does |
|---|---|
| `build_sn.sh <spec> <out> [kind] [mode]` | Compile an SN wiring spec with Blueprint into `<out>` (about 100 s). `kind` sets `OT_BRIDGE`, `mode` sets `OT_GRPC_MODE` (`interceptor` or `wrapper`). Runs `goimports` on the result like the deploy script does. |
| `codegen_diff.sh <build A> <build B>` | Diff the generated Go and .proto files of two builds (skips the copied runtime and workflow modules). |
| `build_binaries.sh <build>` | `go build` every service process of a build into `<build>/bin/` (about 2 minutes). Fails if anything does not compile. |
| `fakecollector/` | Small Go program standing in for the OpenTelemetry collector: records every exported span to a JSON-lines file and serves `/getFullConfig` for the bridges. `run_local.sh` builds and starts it. |
| `run_local.sh <build> <results> [kind] [seconds] [rate]` | Run a compiled build locally: fake collector, all services, then wrk2 with the compose-post Lua script over ONE connection with a fixed seed (`RANDOM_SEED`, default 1). Writes `spans.jsonl`, `wrk.out` and one log per service to `<results>`. Sets the run-time env like the experiments do (`BRIDGE_KIND`, `REVERSE_TRUSS`, ...). |
| `compare_traces.py <run A> <run B> [--ignore-attr K]` | Compare two runs trace by trace (in request order). Ignores IDs and timing and the order of concurrent siblings; compares names, tracer names, kinds, status, attributes, events and tree shape. Exit code 0 = same. |
| `selftest_compare.py <run>` | Checks that `compare_traces.py` notices differences: corrupts copies of a run six ways and expects DIFFERENT for each, SAME for the unmodified copy. |
| `trace_stats.py <run> [--expect-spans N]` | Counts traces, spans per trace and spans per name in one run. With `--expect-spans 23` it fails if any trace is incomplete (used for the sampling check, where runs cannot be paired). |
| Go unit tests: `runtime/plugins/opentelemetry/interceptors/interceptors_test.go` | Interceptors over real in-memory gRPC with an in-memory span recorder. Run: `cd runtime && go test ./plugins/opentelemetry/interceptors/`. Covers what the no-work app cannot: errors, `retCtx` round trips, unsampled traces. |

| `attr_stats.py <run A> <run B> [<run C>...]` | For attributes that cannot match trace by trace (random checkpoint draws, order of concurrent calls): per span name and attribute, the distribution of values (short values exactly, long ones by length, `<absent>` counted too), and the total variation distance (TVD) of each run against run A. |
| `ab_trio.sh <wrapper build> <interceptor build> <kind> <prefix> [s] [rate]` | The bridge comparison in one go: runs the wrapper build twice and the interceptor build once *at the same time* (same machine load), then `compare_traces.py` ignoring `_br _rc _o`, and `attr_stats.py`. Wrapper-vs-wrapper gives the noise level to judge wrapper-vs-interceptor against. |

`run_local.sh` also reads from the environment:

* `OTEL_SAMPLE_RATIO` (default 1): head sampling ratio of the services.
* `GOMAXPROCS` (default 2).
* `BRIDGE_CONFIG`: the policy the fake collector hands the bridge processors. Default is the SN
  experiments' `{"cpd_min":2,"cpd_max":6,"reverse_policy":"inverse_depth"}` (random draws). For
  repeatable checkpoint decisions use `{"cpd_min":3,"cpd_max":3,"reverse_policy":"ttl"}`.

### What the bridge attributes look like (as exported)

Found by looking at the recorded spans, not by reading the processors:

| attribute | on | value | can it match between two runs? |
|---|---|---|---|
| `_d` | non-checkpoint spans | one byte (a depth / countdown) | yes, exactly, with the fixed config |
| `_br` | checkpoint spans | bytes; contains span IDs and a Bloom filter of IDs | only its presence and size |
| `_rc` | spans that carry reverse context | base64; contains span IDs | only presence and size |
| `_o` | every span (SB) | one byte: the span's start ordinal among its siblings | no: depends on which concurrent call starts first |

`hasChildren`, `childCount` and `remEndEvents`, which the interceptors set, do not show up in the
export: the processors treat them as in-process helpers and skip them when building the export
(`runtime/plugins/otelcol/pb_processor.go` around line 782, `sb_processor.go` around line 998).
That is why the unit tests check them directly.

| `latency_ab.sh <wrapper build> <interceptor build> <kind> <out>` | Controlled end-to-end latency comparison (see "Latency methodology" below): runs wrapper, interceptor, interceptor, wrapper, one at a time, each as an unloaded run and a load ramp. |
| `latency_summary.py <out> [--csv F]` | Summarises `latency_ab.sh`: mean / p50 / p99 latency and throughput per load level, interceptor minus wrapper, next to the gap between two repetitions of the same build. |

| `profile_ab.sh <wrapper build> <interceptor build> <kind> <out> [rate] [s]` | Where the extra CPU goes: runs both builds one after the other at the same fixed rate (below saturation), prints CPU milliseconds per request for every service, and the function-level profile difference (`go tool pprof -diff_base`) for the services whose CPU grew most. |

| `cpu_ab.sh <wrapper build> <interceptor build> <kind> <out> [rate] [s] [rounds]` | (`LABEL_A`/`LABEL_B` rename the two builds, e.g. `old`/`new` for two interceptor builds; no underscores.) CPU per request with repetitions: runs wrapper, interceptor, interceptor, wrapper (`rounds` times), one at a time, no profiling; prints CPU per request of every run, the mean and spread per build, and the difference of the means next to the noise. Use this, not a single `profile_ab.sh` run, to decide whether one build is cheaper. |
| `cpu_env_ab.sh <build> <kind> <out> <env A> <env B> [rate] [s] [rounds]` | The same as `cpu_ab.sh`, but one build under two run-time settings, e.g. `"GRPC_STREAM_WORKERS=0"` vs `"GRPC_STREAM_WORKERS=8"`: runs A, B, B, A (`rounds` times); prints CPU per request per run, mean and spread per arm, the difference, and every service's CPU per request per arm. `PROFILE_SECONDS` set also profiles every run. |
| `stack_growth.py <profiles...> [--top N] [--chains]` | How much CPU goes into growing goroutine stacks (`runtime.newstack`), by the function that crossed the limit and by goroutine. Takes `run_local.sh` profiles (`<run>/pprof/*.pb.gz`). |
| `cpu_areas.py <profile dir> [<profile dir>]` | CPU seconds by area (interceptors' own code, span start/end, processor, export, HTTP/2 header compression, gRPC transport, protobuf, GC, allocation, stack growth, scheduler, syscalls), for one run or two side by side. Takes `<run>/pprof` directories. |
| `tput_ab.sh <build A> <build B> <kind> <out> [overload rate] [s] [rounds]` | Peak throughput with repetitions: per run a fresh start, a warm-up step at 700 req/s (its CPU per request is reported too) and an overload step (default 1600 req/s offered, 45 s), order A-B-B-A per round. Summary: achieved req/s and service CPU per run, mean and spread per build, and the difference within each ABBA round (cancels the machine's drift). Runs whose services got clearly less CPU (`STARVED`) or whose warm-up stalled are flagged and left out (see the 2026-10-15 log entry). `LABEL_A`/`LABEL_B` (default `old`/`new`); `SUMMARY_ONLY=1` recomputes the summary of an existing output directory. |

| Go benchmark: `runtime/plugins/opentelemetry/interceptors/bench_test.go` | One traced call through real gRPC (in-memory connection) and both interceptors, per variant (no tracing, vanilla, bridge with a reverse context), each also with `/HeaderTable0` (server option `grpc.HeaderTableSize(0)`), `/Workers` (gRPC stream workers on the server), `/GoPerCall` (each call on a new goroutine, as the SN workflow does) and `/GoPerCall+Workers`: ns and allocations per call. Seconds instead of a 40-minute experiment, for trying ideas; confirm winners with `cpu_ab.sh`. Run: `cd runtime && go test -run '^$' -bench . -benchmem ./plugins/opentelemetry/interceptors/`. |

All measurement scripts take a lock (`MEASURE_LOCK`, default `/tmp/grpc_interceptors_measure.lock`):
`latency_ab.sh`, `cpu_ab.sh`, `cpu_env_ab.sh` and `profile_ab.sh` exclusively, `run_local.sh` shared. A measurement
refuses to start while anything else holds the lock, and a local run refuses to start during a
measurement, so two experiments can no longer share the machine by accident.

`run_local.sh` always records CPU per process for every load step (`cpu.txt` / `cpu-<rate>.txt`:
CPU seconds each service used during the step, from `/proc/<pid>/stat`). With `PROFILE_SECONDS=30`
every service also gets its own pprof port (`BRIDGES_PPROF`, `runtime/plugins/otelcol/pprof.go`) and
a CPU profile of every service is saved 15 s into each load step (`pprof[-<rate>]/<proc>.pb.gz`).

To total the CPU a profile spends in one area (e.g. HTTP/2 header compression), take the
cumulative time of the top frame among the samples whose stack passes through the area:

    go tool pprof -top -cum -nodecount=1 -focus='http2/hpack' <profile.pb.gz>

`run_local.sh` options added for the latency runs: a comma-separated rate list runs a ramp against
the same running services (`wrk-<rate>.out` per step); `WRK_THREADS`, `WRK_CONNECTIONS`;
`WRK_CPUS`, `SERVICE_CPUS` (CPU pinning with `taskset`); `RECORD_SPANS=0` (fake collector only
counts spans, `fakecollector -discard`). wrk2 always runs with `-L` (latency percentiles).

### Recipe: compare two builds end to end

    W=~/grpc-interceptors-work
    utils/grpc_interceptors/build_sn.sh docker_v_es_nw $W/A        # e.g. with OT_GRPC_MODE=wrapper
    utils/grpc_interceptors/build_sn.sh docker_v_es_nw $W/B        # e.g. with interceptors
    utils/grpc_interceptors/build_binaries.sh $W/A
    utils/grpc_interceptors/build_binaries.sh $W/B
    utils/grpc_interceptors/run_local.sh $W/A $W/runs/A v 5 10    # 50 requests
    utils/grpc_interceptors/run_local.sh $W/B $W/runs/B v 5 10
    utils/grpc_interceptors/compare_traces.py $W/runs/A $W/runs/B

Note that in the no-work app every ComposePost request makes the same 23 calls whatever its
content, so traces of different requests already look alike. That is why the comparer's own test
(`selftest_compare.py`) corrupts spans instead of changing the requests.

### Latency methodology

Question: does tracing with interceptors instead of wrappers change end-to-end response time?

What is measured: the response time wrk2 sees for ComposePost on the no-work SN app (HTTP request
into `wrk2api`, the whole call tree, response back), plus the achieved throughput.

Setup (`latency_ab.sh`, one bridge kind at a time):

* **Machine**: one 8-CPU node (Xeon E5530, clock pinned at its 2.394 GHz base; see
  `utils/pin-cpu-22ghz.sh`). All 14 services, the fake collector and wrk2 run on it, inside a
  private network namespace. It is also the cluster's control-plane node, so Kubernetes' own
  processes run in the background; they are the same for every run.
* **CPU split**: wrk2 on CPUs 6-7, services and fake collector on CPUs 0-5 (`taskset`), so the
  load generator does not take CPU from the services. Services run with `GOMAXPROCS=2`, `GOGC=100`
  and the experiments' tracing settings for the kind (`BRIDGE_KIND`, `REVERSE_TRUSS`, ...,
  `OTEL_SAMPLE_RATIO=1`, the real SN bridge config: CPD 2..6, `inverse_depth`).
* **Collector**: the fake collector only counts spans (`-discard`). It still receives and decodes
  every export, like a real collector would, but does not spend CPU turning spans into JSON.
* **One run at a time, ABBA order**: wrapper, interceptor, interceptor, wrapper. Each run starts
  every service fresh. If the machine drifts during the session (thermal, background work), the
  drift hits both builds equally. Two repetitions per build; the gap between the two repetitions
  of the same build is reported as "noise" next to every difference.
* **Two measurements per repetition**, each its own run:
  * **unloaded**: 1 connection, 100 req/s, 60 s. Requests do not queue, so this is the
    per-request cost of a ComposePost (its whole call tree).
  * **ramp**: 32 connections (2 wrk2 threads), 300, 600, 900, 1050, 1200 req/s, 35 s per step,
    5 s pause between steps, against the same running services. This shows queueing, the tail
    (p99) and where throughput stops growing.
* **wrk2 details**: open loop: it sends at the set rate whether or not earlier responses came
  back, and measures latency from the time a request *should* have been sent (coordinated
  omission correction), so overload shows up as latency, not as a lower offered rate. It does not
  record the first ~10 s of each run or step (calibration). A 10 s step therefore records
  nothing; this is why steps are 35 s and the unloaded run 60 s.

Findings from setting this up (calibration, vanilla wrappers):

* This machine saturates at about 1,130-1,170 req/s (offered 1,600 and 2,400 req/s: achieved
  throughput stays at ~1,130 and latency goes to seconds). Hence the ramp stops at 1,200.
* With 32 connections the latency is about 16-17 ms already at 200 req/s, against 2.75 ms with one
  connection at the same rate. The extra time is queueing: with many connections requests arrive
  in bunches and wait in the services (pinning made no difference: 17.2 ms pinned, 15.8 ms
  unpinned). It is the same for both builds, but it is why the unloaded measurement is separate.

Limits: everything shares one machine (no network between services), so per-hop network cost is
loopback cost; this measures the CPU cost of the two tracing paths, not a real deployment.

---

## Part 3: Log

### 2026-10-13: setup

* Created branch `retctx-grpc-interceptors` from `retctx-reject-impl` (`d0f40602`).
* Built the unmodified SN no-work vanilla spec as a baseline for the codegen regression check:

      cd examples/dsb_sn
      OT_BRIDGE=v go run wiring/main.go -w docker_v_es_nw -o ~/grpc-interceptors-work/baseline_v_nw

  Takes about 100 seconds on this machine.
* Read the generated wrappers to get the exact names used today (tables above).

### 2026-10-13: Step 1, interceptor support in the gRPC plugin

What changed:

* `plugins/golang/ir.go`: new optional interface `golang.TypedInstance` (a node says what Go type
  its runtime instance has).
* `plugins/golang/gogen/namespacebuilder.go`: when a constructor argument is a node that is not a
  service but implements `TypedInstance`, the generated code fetches it into a variable of that
  type instead of `string`.
* `plugins/grpc/interceptors.go` (new): the `ServerInterceptor` / `ClientInterceptor` node
  interfaces, plus `ServerInterceptorNode` / `ClientInterceptorNode` to embed.
* `plugins/grpc/wiring.go`: `AddServerInterceptor` and `AddClientInterceptor` record interceptor
  names as a property on `<service>.grpc_server` / `<service>.grpc_client`; the server and client
  build functions look them up.
* `plugins/grpc/ir_grpc_server.go`, `ir_grpc_client.go`: keep the interceptor nodes and pass them
  as extra constructor arguments.
* `plugins/grpc/grpccodegen/servergen.go`, `clientgen.go`: the generated constructors take
  `interceptors ...grpc.Unary{Server,Client}Interceptor` and install them only when there are any.
  The client's `timeout ...string` became `timeout string` to free the variadic slot (the generated
  DI code was its only caller and always passed one value).

Tests:

1. Codegen diff, before vs after, `docker_v_es_nw`:

       utils/grpc_interceptors/codegen_diff.sh $W/baseline_v_nw $W/after_step1_v_nw

   31 files differ: the 19 generated gRPC clients and 12 generated gRPC servers, and only in the
   lines the template change touches. The process `main` / DI files are unchanged (the DI code
   still calls `New_X_GRPCClient(ctx, addr, "1s")`).
2. Everything compiles: `build_binaries.sh` built all 14 processes.
3. Same behaviour: ran the old build and the new build locally (50 requests each) and compared:
   **50/50 traces identical** (1,150 spans each, 23 per request).
4. Noise floor of the comparison: the same build run twice gives 50/50 identical.
5. The comparer catches differences: `selftest_compare.py` passed (all 6 corruptions detected).

Problems hit on the way:

* `goimports` was not installed on this machine; installed it with `go install`.
* The repo's top-level `go.work` breaks building the standalone `fakecollector` module; the
  scripts build it with `GOWORK=off`.
* An nginx on this CloudLab node listens on `0.0.0.0:8080`, which is exactly the port the bridge
  processors use for `/getFullConfig`. The fake collector died on startup and every export failed
  (`send_unavailable`). Fix: `run_local.sh` runs everything inside a private network namespace
  (`unshare -rn`), where all ports are free.
* A first negative control (same build, different request seed) came back SAME. Not a bug: the
  no-work ComposePost makes the same calls for every request. Replaced it with
  `selftest_compare.py`.

Not yet tested: that interceptors actually get installed and run. That needs a real interceptor
node, which Step 2 provides.

### 2026-10-13: Step 2a, vanilla interceptors

What changed:

* `runtime/plugins/opentelemetry/interceptors/` (new package):
  * `interceptors.go`: the two constructors (`NewServerInterceptor`, `NewClientInterceptor`, picking
    the implementation by bridge kind), the metadata keys, span/tracer naming from the gRPC method
    name, and the steps every variant shares (carrier decoding, `__seq` -> `seqNum`, baggage merge,
    `retCtx` handling). Each helper says which wrapper step it replaces.
  * `vanilla.go`: the vanilla server and client interceptors, step by step the same as
    `serverTemplateVanilla` / `clientSideTemplateVanilla`.
* `plugins/opentelemetry/ir_ot_interceptors.go` (new): IR nodes that declare
  `interceptors.NewServerInterceptor(ctx, <collector client>, "<OT_BRIDGE>")` (and the client one).
  They embed `grpc.ServerInterceptorNode` / `grpc.ClientInterceptorNode` from Step 1.
* `plugins/opentelemetry/wiring.go`: `InstrumentGRPC(spec, service, collector)`. Defines the two
  interceptor nodes and registers them with `grpc.AddServerInterceptor` / `AddClientInterceptor`.
  With `OT_GRPC_MODE=wrapper` at build time it calls the old `Instrument` instead.
* `examples/dsb_sn/wiring/specs/docker_nw.go`: gRPC services use `InstrumentGRPC`. The HTTP entry
  service (`wrk2api`) still uses `Instrument`, as it must.

Checked the generated code of the interceptor build: the process code fetches the interceptor as
`grpc.UnaryServerInterceptor` (the Step 1 `TypedInstance` change doing its job), creates it with
`interceptors.NewServerInterceptor(n.Context(), tracer, "v")`, and passes it to
`New_TextService_GRPCServerHandler`. The `.proto` services are now the plain interfaces
(`service TextService`), and no `ot/` wrapper code is generated for gRPC services.

Tests:

1. End-to-end, wrappers vs interceptors, vanilla, 50 requests each:

       utils/grpc_interceptors/build_sn.sh docker_v_es_nw $W/v_wrapper v wrapper
       utils/grpc_interceptors/build_sn.sh docker_v_es_nw $W/v_interceptor v interceptor
       utils/grpc_interceptors/build_binaries.sh $W/v_wrapper
       utils/grpc_interceptors/build_binaries.sh $W/v_interceptor
       utils/grpc_interceptors/run_local.sh $W/v_wrapper $W/runs/v_wrapper v 5 10
       utils/grpc_interceptors/run_local.sh $W/v_interceptor $W/runs/v_interceptor v 5 10
       utils/grpc_interceptors/compare_traces.py $W/runs/v_wrapper $W/runs/v_interceptor

   **50/50 traces identical** (1,150 spans each): same span names, tracer names, kinds,
   attributes, events and tree shape. This includes the mixed case: `wrk2api` (HTTP, wrapper)
   calling `composepost` (gRPC, interceptors) works, so the context state is compatible.
2. Sampling at 50% (`OTEL_SAMPLE_RATIO=0.5`, 200 requests): both builds export only complete
   traces (`trace_stats.py --expect-spans 23`: 0 incomplete). Wrapper: 82 traces sampled,
   interceptor: 87. Sampling is random per trace, so the two runs cannot be paired; completeness
   is what shows the `U:` fast path works across all services.
3. Go unit tests (`go test ./plugins/opentelemetry/interceptors/`), all pass:
   span names, kinds, tracer names and parent links over two hops; baggage two hops down;
   unsampled traces keep their trace ID and produce no spans; `retCtx` travels back two hops when
   the call succeeds and does not when it fails; errors are recorded on both spans.

This also completes the Step 1 check that was still open: interceptors registered through the
gRPC plugin are installed and run.

### 2026-10-13: Step 2b, one implementation for all bridge kinds

Diffing the four wrapper templates against each other (extracted to files, then `diff -b`) showed
that the bridges differ from vanilla in only three small ways:

| | child counter + `seqNum` / `__seq` | attribute after the call | SB end-event list -> `remEndEvents` |
|---|---|---|---|
| vanilla | no | none | no |
| pb | yes | `hasChildren` (bool) | no |
| cgpb | yes | `childCount` (int) | no |
| sb | yes | `childCount` (int) | yes |

So instead of four copies of the interceptors there is one server interceptor (`server.go`) and
one client interceptor (`client.go`), numbered step by step like the templates, and a small table
(`variant` / `variantFor` in `interceptors.go`) that switches those three things on per kind.
`variantFor` maps `OT_BRIDGE` values exactly like the template selection does, including the
fallback to the path bridge for unknown values. `vanilla.go` was folded into this.

Two deliberate deviations from the templates, neither changes what is traced:

* The SB server reads the end-event list under its mutex (the template reads it without; all
  child calls have returned by then, so this only keeps the race detector quiet).
* Missing per-request state in a bridge client (a call made outside a traced server) panics, as
  it does in the wrappers. Kept identical on purpose.

Unit tests added (`TestBridgeChildState`, `TestVanillaNoBridgeState`): for pb, cgpb and sb, a
server that calls another server three times; the callee must see child ordinals 1, 2, 3 both in
`__seq` and as `seqNum`; the caller's span must carry `hasChildren=true` (pb) or `childCount=3`
(cgpb, sb), and for sb `remEndEvents` must be exactly base64 of the bytes `3 2 1 2`; the leaf must
say `hasChildren=false` / `childCount=0` and have no `remEndEvents`. Vanilla must send no `__seq`
and set no attributes. All tests pass, also with `-race`. The vanilla end-to-end comparison is
rerun on the restructured code below.

About comparing bridge runs: the bridge processors draw checkpoint distances (and reverse
routing) from Go's global, unseeded random source (`runtime/plugins/otelcol/checkpoint_distance.go`,
`reverse_checkpoint.go`), shared by all concurrent requests. Two runs of the *same* build will pick
different checkpoints. Plan: first compare the wrapper build with itself (A/A) to see which
attributes vary from randomness alone; then compare wrapper vs interceptor ignoring only those;
then compare the distributions of the ignored attributes.

### 2026-10-13: Step 2c, bridge interceptors validated (PB, CGPB, SB)

All bridge builds: `build_sn.sh docker_<kind>_es_nw $W/<kind>_{wrapper,interceptor} <kind> {wrapper,interceptor}`
plus `build_binaries.sh`. All comparisons below ran with `ab_trio.sh` (two wrapper runs and one
interceptor run at the same time, 2,000 requests each, 46,000 spans per run) unless noted.

**Harness problems found and fixed on the way** (none of them in the interceptors):

* The fake collector answered `/getFullConfig` with a bare JSON object; the processors expect
  `{"config": {...}}` and gave up after 30 s of retries. Fixed in `fakecollector`.
* The bridge processors export to `<collector host>:4317` (the agent's OTLP port on Kubernetes),
  not to the collector address in `.local.env` (vanilla uses the address). `fakecollector -otlp`
  now takes several addresses and `run_local.sh` listens on both.
* Load started while some services were still coming up. `run_local.sh` now waits until every
  service port listens, and then `SETTLE_SECONDS` (15 s): gRPC clients that dialed a server that was
  not up yet are in a reconnect backoff, and calls during the backoff fail with "connection
  refused". This showed up as 34-41 failed requests in the first 2,000-request vanilla runs (three
  runs starting 45 processes at once made startup slow); after the fix, zero.
* A bash default `${BRIDGE_CONFIG:-{...}}` ends at the first `}`; replaced by a plain variable.

**How the bridge runs can be compared.** Two runs of the *same* bridge build already differ:

1. The processors draw checkpoint distances (and, with `inverse_depth`, reverse routing) from Go's
   global random source.
2. SB's start ordinal `_o`, and the size of CGPB/SB `_br` records, depend on the order in which
   ComposePost's concurrent calls start and finish (it runs 4, then 3, calls in goroutines).
   `GOMAXPROCS=1` did not make that repeatable (22/50 traces).

So each bridge is checked two ways:

* **Trace by trace, exact**, ignoring only `_br`, `_rc`, `_o` (and `_d` under the random config):
  tree shape, span and tracer names, kinds, status, all other attributes. With
  `BRIDGE_CONFIG='{"cpd_min":3,"cpd_max":3,"reverse_policy":"ttl"}'` (fixed distance, deterministic
  routing; `RT_LEAF_REJECT=1` is a rate of 1.0) every bridge decision depends only on structure, so
  `_d` is exact too, and for PB even the presence and size of `_br`/`_rc` match per span
  (`compare_traces.py --mask-attr _br --mask-attr _rc`: 50/50 for wrapper vs wrapper and for
  wrapper vs interceptor).
* **Distributions** (`attr_stats.py`): per span name and attribute, the distribution of values
  (or sizes); total variation distance (TVD) for every pair of runs. The interceptor must look
  like a third wrapper run.

**Results** (TVD: largest over all span/attribute rows):

| kind | config | trace by trace, wrapper vs wrapper | trace by trace, wrapper vs interceptor | TVD w-w | TVD w-i (both pairs) |
|---|---|---|---|---|---|
| v | SN | 2000/2000 | 2000/2000 | 0.000 | 0.000, 0.000 |
| pb | SN (random) | 2000/2000 | 2000/2000 | 0.044 | 0.046, 0.055 |
| pb | fixed | 50/50 (`_br`,`_rc` masked, not ignored) | 50/50 | | |
| cgpb | fixed | 2000/2000 | 2000/2000 | 0.220 | 0.212, 0.011 |
| cgpb | SN (random) | 2000/2000 | 2000/2000 | 0.054 | 0.043, 0.050 |
| sb | fixed | 2000/2000 | 2000/2000 | 0.218 | 0.168, 0.368 |
| sb | SN (random) | 2000/2000 | 2000/2000 | 0.199 | 0.127, 0.323 |

PB, CGPB and vanilla: the interceptor is indistinguishable from a wrapper run.

**SB: a real, explained difference.** The SB distribution gap is bigger for one pair, so SB was
rerun with two runs of each build (`INTERCEPTOR_RUNS=2 ab_trio.sh ...`, fixed config): wrapper vs
wrapper TVD 0.322, interceptor vs interceptor 0.098, wrapper vs interceptor 0.26-0.44. The two
interceptor runs agree with each other and differ from the wrappers in the same way, so this is
systematic. All of it is in `_o` (and the `_br` sizes that encode it) of ComposePost's concurrent
callees, e.g.:

| callee of ComposePost | start ordinal, wrapper runs | start ordinal, interceptor runs |
|---|---|---|
| UniqueId | 3: 44-62% | 3: 87-88% |
| Media | 4: 0-1% | 4: 15-20% |

Cause: the child ordinal is taken when the caller's tracing code runs. With wrappers that is the
very first thing in the calling goroutine. With interceptors it is inside gRPC, after Blueprint has
converted the call's arguments to protobuf (Text sends a 256-character post, Media lists of IDs),
so calls with bigger arguments reach the counter relatively later, and they win the race among
concurrent siblings less often. This is the "client span no longer covers argument conversion"
difference from the plan, now measured. The SB logic itself is unchanged:

* ordinals are always a valid numbering (1-4 for the first wave, 5-7 for the second);
* in both builds the ordinals follow the actual start order of the client spans equally often
  (wrapper 72.8% / 81.3%, interceptor 74.6% / 75.2%; never 100% because the ordinal is taken just
  before the span starts, in both builds, so concurrent goroutines can slip in between).

### 2026-10-13: Step 2d, interceptors become the default; other checks

* `examples/dsb_sn/wiring/specs/docker.go` (SN real-work), `examples/dsb_hotel/wiring/specs/original.go`
  (all HotelReservation specs, via `applyDefaults`), `examples/leaf/wiring/specs/docker_fanout.go`
  and `docker_tree.go`: gRPC services now use `opentelemetry.InstrumentGRPC`; HTTP services keep
  `opentelemetry.Instrument`. `OT_GRPC_MODE=wrapper` at build time gives the old behaviour.
  Not changed: upstream's `examples/sockshop` (not part of RetCtx).
* Compile check of the switched specs with interceptors (`build_sn.sh` + `build_binaries.sh`;
  `build_sn.sh` now takes `APP=dsb_hotel` for the hotel app):
  * SN real-work `docker_sb_es`: all processes compile; 12 server interceptors and 19 client
    interceptors created with `"sb"`.
  * HotelReservation `docker_sb_es`: all processes compile; 7 server and 7 client interceptors
    (every service except the HTTP frontend).
  These were not run: their services need MongoDB / Redis / memcached, which the local harness
  does not provide.
* The vanilla end-to-end comparison was rerun on the restructured code (2,000 requests): no
  errors, 2000/2000 identical, TVD 0.000.
* `runtime/go.mod`: `google.golang.org/grpc` is now a direct dependency (dropped `// indirect`).
  A full `go mod tidy` would also make unrelated changes (the module was not tidy before), so it
  was not run.
* `go test ./plugins/opentelemetry/` fails in `TestGeneratedReverseCheckpointWrappers`
  ("application/SDK contract: result=42 err=application error calls=0"). This test covers the
  old wrapper templates, which were not changed, and it fails the same way on the untouched base
  commit `d0f40602` (checked in a separate worktree). Pre-existing; not investigated here.

### 2026-10-13: end-to-end latency, round 1 (before any optimisation)

Method: "Latency methodology" above (`latency_ab.sh`; results in `~/grpc-interceptors-work/lat/`).
Mean latency in ms, average of two repetitions per build; "noise" is the largest gap between two
repetitions of the same build.

Vanilla:

| load | wrapper | interceptor | difference | noise |
|---|---|---|---|---|
| unloaded, mean (p99) | 2.85 (4.63) | 2.99 (4.41) | +0.13 (-0.22) | 0.09 (0.82) |
| 300 req/s, mean (p99) | 17.3 (27.7) | 19.1 (31.9) | +1.7 (+4.2) | 0.4 (0.2) |
| 900 req/s, mean (p99) | 18.2 (35.8) | 20.4 (47.0) | +2.3 (+11.2) | 0.4 (5.5) |
| 1050 req/s, mean (p99) | 21.3 (55.0) | 29.9 (93.8) | +8.6 (+38.8) | 1.5 (14.5) |
| throughput at 1200 req/s offered | 1,181 req/s | 1,114 req/s | -5.7% | |

SB: unloaded 3.27 -> 3.53 ms (+0.26, noise 0.08); throughput at 1,200 req/s offered 954 -> 873
req/s (-8.5%); at 900 req/s the interceptor build was already past its knee (mean 546 ms vs 31 ms).

Every difference was well above the noise: the interceptor build cost more CPU per request, and
near saturation that showed up as queueing. (One odd point, left out of the table: at 600 req/s
the vanilla *wrapper* build was faster than at 300 req/s, 11.9 vs 17.3 ms, in both repetitions;
the interceptor build was not, 19.0 ms. Not investigated, so the large 600 req/s gap, +7.1 ms,
should not be read as more than the neighbouring points show.)

An earlier quick look, from the trace-comparison runs, had shown no difference. Those runs were not
built for latency (several copies of the app on the same CPUs at once, one connection, 20 req/s),
and they were too noisy to see a 0.1-0.2 ms effect.

### 2026-10-13: Profiling: where the interceptors' extra CPU went, and the fixes

Method: `profile_ab.sh` (above): both builds, one after the other, at the same fixed rate below
saturation (vanilla 800 req/s, bridges 700 req/s, 60 s), CPU per request per service from
`/proc`, and a 30 s CPU profile of every service. Results in `~/grpc-interceptors-work/prof/`.

**First profile (vanilla, original interceptor code): +0.238 ms of CPU per request (+5.4%).**
Every leaf service, which handles one incoming gRPC call per request, paid about +12 us; ComposePost
(1 incoming, 7 outgoing calls) +59 us. So: a cost per gRPC call, on both sides. In ComposePost's
profile (30 s, CPU seconds, wrapper -> interceptor):

| area | wrapper | interceptor |
|---|---|---|
| HTTP/2 header compression (hpack, Huffman coding) | 1.16 | 1.46 |
| gRPC metadata package | 0.28 | 0.57 |
| string concatenation (span names) | 0.03 | 0.21 |
| gRPC header building (`createHeaderFields`) | 0.62 | 0.68 |

Inefficiencies found in the interceptor code, and the fixes:

1. **The server copied all request metadata on every call** (`metadata.FromIncomingContext`
   builds a new map of every header). Now `metadata.ValueFromIncomingContext`, which copies only
   our key.
2. **The client asked gRPC for the response trailer on every call**, even when no reverse context
   can come back (vanilla, or `REVERSE_TRUSS=off`). gRPC then copies the trailer map into our
   variable on every call. Now only when `REVERSE_TRUSS=on`.
3. **Span and tracer names were built by string concatenation on every call** (the wrappers have
   them as constants). Now built once per method and cached (`namesFor`).
4. **`REVERSE_TRUSS` was read with `os.Getenv` on every call** (as the wrappers do). Now once, when
   the interceptor is created; the setting cannot change while a process runs.

After these: **+0.069 ms per request (+1.5%)**, leaf services about +3 us per call.

**The carrier.** What was left was mostly gRPC turning our trace carrier into an HTTP/2 header.
The carrier was the wrappers' JSON (`{"baggage":{...},"trace_ctx":{"TraceID":"...","SpanID":"...",
"TraceFlags":"01","TraceState":"","Remote":false}}`, 150 bytes with empty baggage) to carry about
25 bytes of information. Inside a protobuf message that costs a copy; as a header, HTTP/2
Huffman-encodes it on the way out, decodes it on the way in, and, since the value is different on
every call, keeps inserting it into (and evicting other headers from) the connection's header
table. So between interceptors the context now travels in a compact binary form
(`carrier.go`): `0x01 | trace ID | span ID | flags | trace state | baggage`, 28 bytes for vanilla,
36 with a bridge's `__seq` (38 / 48 after gRPC's base64 for `-bin` headers), under
`bp-trace-ctx-bin`. The unsampled `U:` text form became `0x00 | trace ID | span ID` (25 bytes).
`carrier_test.go` checks that for 5,000 random span contexts, flags, trace states and baggage maps
(including characters JSON must escape and non-ASCII) the compact form decodes to exactly what
`backend.GetSpanContext` returns for the JSON; plus empty baggage, the unsampled form, and
malformed input (which, like unparseable JSON, makes the server start a new trace).

After the compact carrier: **vanilla +0.020 ms per request (+0.4%)**, i.e. even with the wrappers.

**Bridges.** SB was still at +0.212 ms (+4.1%). Its profile showed the extra in the reverse
context: gRPC copies the response trailer on every call (`MD.Copy` from `grpc.Trailer`, 0.35 s of
ComposePost's 30 s) and the base64 reverse context is a header too. Two small fixes in our trailer
code: build the trailer map directly instead of `metadata.Pairs` (which lower-cases the key every
call) and read it with a map lookup instead of `MD.Get` (same). With the final code:

| kind | CPU per request, wrapper -> interceptor |
|---|---|
| vanilla | 4.448 -> 4.467 ms (+0.4%) |
| PB | 5.111 -> 5.123 ms (+0.2%) |
| SB | 5.204 -> 5.355 ms (+2.9%) |

PB returns its reverse context through the same trailer path and is even, so the trailer is not
what is left in SB. In SB's ComposePost the bridge's truss / Lehmer encoding (encoding the order of
sibling calls) took 0.97 s instead of 0.61 s. The size of what SB exports per request is the same
(about 1,310 characters of bridge attributes per request in both builds), so this is not bigger
records; Lehmer coding costs depend on the order being encoded, and the interceptors change which
concurrent sibling tends to start first (Step 2c). That is the likely explanation, not a proven one.
It is in the SB processor (`runtime/plugins/otelcol`), not in the interceptor code.

Every check was rerun on the final code: unit tests (with `-race`), vanilla traces 50/50
identical to the wrapper build, PB and SB 50/50 identical with the fixed bridge config (PB even
including presence and size of `_br` / `_rc`).

**Code layout change (requested):** helpers used by only one file now live in that file:
`interceptors.go` keeps only what both sides use (constructors, the variant table, `namesFor`,
`mergeSpanBaggage`), `carrier.go` the carrier, `server.go` and `client.go` their own helpers. Every
call to a function defined in another file says where it is, e.g.
`ctx = backend.WithRetMerge(ctx) // runtime/core/backend/reversetruss.go` or
`names := namesFor(info.FullMethod) // interceptors.go`; calls to helpers in the same file say
`// below`.

Incident: the connection to the session dropped while the SB/PB profiles were running. The
profiling run itself survived and completed (results kept). A trace check started just before the
drop was still running when the second latency experiment started, so that experiment was
stopped at once (all its processes killed by PID), its partial results left in
`~/grpc-interceptors-work/lat2/` and not used, and the experiment restarted on an idle machine
(`~/grpc-interceptors-work/lat3/`).

### 2026-10-13: end-to-end latency, round 2 (after the first optimisations)

Same method as round 1, on the code with the compact carrier and the first fixes (not yet the
tracer cache or the frame changes below). Results in `~/grpc-interceptors-work/lat4/`.

Vanilla (mean / p99 in ms; noise = largest gap between two repetitions of the same build):

| load | wrapper | interceptor | difference | noise |
|---|---|---|---|---|
| unloaded | 2.79 / 4.21 | 2.85 / 4.24 | +0.06 / +0.03 | 0.02 / 0.21 |
| 900 req/s | 17.8 / 33.7 | 18.5 / 37.4 | +0.7 / +3.7 | 0.9 / 2.4 |
| 1050 req/s | 20.1 / 47.9 | 20.8 / 56.2 | +0.8 / +8.2 | 0.9 / 5.4 |
| throughput at 1200 req/s | 1,189 req/s | 1,187 req/s | | |

The vanilla peak-throughput gap of round 1 (1,181 vs 1,114 req/s) is gone; mean latency under load
is within the noise; unloaded +0.06 ms and p99 near saturation slightly above the noise.

SB: unloaded 3.26 -> 3.40 ms (+0.14, noise 0.08); peak throughput 963 -> 913 req/s (-5.2%; round 1
was -8.5%).

Incident (second one): when this round was first started, another copy of the experiment was
already running. It had been launched by the turn that was cut off by the connection drop (the
script `latency_experiment_final.sh`, written one second before the restart). Both shared the
machine for about 7 minutes, which showed up immediately as a wrapper build saturating at ~640
req/s instead of ~1,190. Both were stopped (all processes killed by PID), their results set aside
as `~/grpc-interceptors-work/lat3_contaminated/` and `lat/v_final_contaminated/`, and the round
rerun alone (`lat4/`). To prevent this, the measurement scripts should refuse to start while
another measurement runs (a lock); see "Open items".

### 2026-10-13: Profiling, round 2: tracer cache, stack depth, and noise

**Tracer cache.** The wrappers ask the tracer provider for the tracer on every call
(`tp.Tracer(name)`: a lock and a map lookup in the OTel SDK). The SDK returns the same tracer for
the same name, so each interceptor now caches span name and tracer per method (`methodCache` in
`interceptors.go`) and fetches the provider once. CPU per request: vanilla -0.3% vs wrappers, SB
+2.2% (single runs; see "noise" below).

**Where SB's remaining cost goes.** Profiles of SB's ComposePost (30 s, own CPU per function,
summed by area) showed: protobuf, syscalls, the bridge processor and the backend helpers got
cheaper; the Go runtime got more expensive (about +1.3 s): memory copying, allocation, and above
all **goroutine stack growth** (`runtime.newstack` -> `copystack`: 1.02 s with wrappers, 1.48 s with
interceptors). Every RPC runs on fresh goroutines whose stacks grow by copying when calls nest
deeper. With interceptors the span (and SB's bridge processor, the heaviest of the bridges) starts
*below* three more frames than with wrappers: Blueprint's client method (160 bytes), the generated
gRPC stub (136) and `ClientConn.Invoke`.

Allocation profiles (objects per request) also showed that the interceptors' **server side
allocates less** than the wrappers (UserMention: 125 -> 117), and the client side about 12-14 more
objects per outgoing call, in gRPC's metadata API (`AppendToOutgoingContext`, about 4), collecting
the trailer (about 2, plus gRPC's copy of it) and base64 for the `-bin` header (about 1). Those are
the cost of using gRPC metadata at all; they explain only a few microseconds per request.

**Smaller frames.** Frame sizes from `go build -gcflags=-S` ("locals"):

| | wrapper | interceptor, one function | interceptor now |
|---|---|---|---|
| client side, live during span start and call | 760 | 688 | 288 |
| server side, live during the whole service | 888 | 752 | 264 |

The interceptors are now closures that do little themselves: the span is started directly in the
closure (no helper frame under the SDK's work), and the work before and after the span start and
the call sits in helpers (`prepare`, `carry`, `setup`, `finish`, `invokeWithTrailer`) whose frames
are gone before the deep work starts. (A method value such as `c.intercept`, or a closure that
just calls a method, adds a trampoline frame of 64-120 bytes; hence the body is in the closure.)
With gRPC's extra frames, the client span now starts shallower than with wrappers, and the server's
service code runs about 575 bytes shallower.

**Per-request state in one context layer.** The server put the child counter, the SB end-event
list and its mutex in three `context.WithValue` layers, as the wrappers do. Readers (the client
side and the SB processor, `runtime/plugins/otelcol/sb_processor.go`) look them up by string key.
`requestState` (`server.go`) is one context layer that answers all three keys with the same values,
so nothing else changes; `TestRequestStateKeys` checks it. Saves two or three allocations per
call; measured effect within noise.

All of these were checked again: unit tests with `-race`, vanilla 50/50 identical to the wrapper
build, PB and SB 50/50 with the fixed config (PB including `_br` / `_rc` presence and size).

**Noise.** Profiling the final code gave vanilla -0.2%, PB +3.0%, SB +3.0%. But PB's +3.0% came
from the *wrapper* run being low (5.111 ms per request in one run, 4.949 in another, same build),
while the interceptor build barely moved (5.123 -> 5.096). So one run's CPU per request varies by
about +-3%, as much as the differences being measured, and the single-run numbers above (+0.2%,
+2.2%, +2.9%, +3.0%) cannot be told apart. From here on CPU is compared with repetitions
(`cpu_ab.sh`: wrapper, interceptor, interceptor, wrapper, twice).

### 2026-10-13: CPU per request with repetitions (final code so far)

`cpu_ab.sh`, 4 runs per build (wrapper, interceptor, interceptor, wrapper, twice), 60 s each at a
fixed rate below saturation (vanilla 800 req/s, PB and SB 700). Code: compact carrier, tracer
cache, one-layer request state, slim frames. Results in `~/grpc-interceptors-work/cpu/`.

| kind | wrapper (mean) | interceptor (mean) | difference | noise (largest same-build spread) |
|---|---|---|---|---|
| vanilla | 4.488 ms | 4.454 ms | -0.034 ms (-0.8%) | 0.235 ms |
| PB | 5.006 ms | 5.129 ms | +0.123 ms (+2.5%) | 0.111 ms |
| SB | 5.283 ms | 5.397 ms | +0.113 ms (+2.1%) | 0.120 ms |

(Vanilla's interceptor spread comes from one run that served 1% fewer requests and used less CPU
per request, 4.313 ms; without it the interceptor mean is 4.500 ms, +0.3%. Either way: even.)

Reading: vanilla with interceptors is as cheap as with wrappers. The bridges cost about 2% more CPU
with interceptors, at the edge of the noise but consistently (in PB every interceptor run was above
every wrapper run). The earlier single-run results (PB "+0.2%") were noise.

What the bridges have that vanilla does not: the reverse context comes back in a gRPC trailer on
every call. In PB's ComposePost (30 s profiles, wrapper -> interceptor): HTTP/2 header compression
0.90 -> 1.52 s, gRPC copying the trailer on the client 0.13 -> 0.49 s, gRPC metadata overall
0.25 -> 0.76 s, writing the trailer on the server 0.03 -> 0.16 s. The wrappers carried the reverse
context as a field of the protobuf response, which costs almost nothing extra. The trailer copy is
part of gRPC's API (`grpc.Trailer` always hands out a copy), and gRPC gives no control over HTTP/2
header indexing, so a value that differs on every call is compressed and inserted into the
connection's header table every time.

### 2026-10-13: Decision: trace data stays in gRPC metadata

Proposed and rejected: carrying the trace context inside the protobuf message bytes (as an extra,
unknown field number attached by the interceptors, which the standard codec would carry along).
It would remove the metadata and trailer costs, but it puts the tracing data back into the payload,
which is what the wrappers do; for the direction of this work that is equivalent to requiring
application changes. So the interceptors keep using gRPC metadata (request header) and trailers,
and the remaining work is making that path cheaper.

### 2026-10-13: Cheaper metadata path (microbenchmark-driven)

`bench_test.go` (see the testing section) measures one traced call in-process. First numbers
(3 runs each, stable to about 1%): no tracing 203 allocations per call, vanilla interceptors 237,
bridge interceptors with a reverse context 264.

**HTTP/2 header table.** Splitting the profiles' header-compression CPU by function showed that
Huffman coding is cheap; the cost is maintaining HTTP/2's header table (`hpack`
`headerFieldTable.evictOldest` 0.23 -> 0.64 s and `.search` 0.41 -> 0.66 s in PB's ComposePost
per 30 s). Every header value that differs per call (gRPC's own `grpc-timeout`, our carrier, the
reverse context in the trailer) is inserted into the connection's 4 KB table, and each insert into a
full table evicts the oldest entry by shifting the whole table and deleting map entries keyed on
name and value (`golang.org/x/net/http2/hpack/tables.go`). gRPC offers one knob,
`grpc.HeaderTableSize` (server option, experimental), which tells clients how big a table to use for
request headers; with 0 nothing is inserted. Measured with the benchmark it did not help (9 more
allocations per call, no faster), because every request header is then sent in full. Dropped.
gRPC has no option for the response direction and no way to mark a header as "never index".

**Changes kept** (allocation sites found with `go test -bench ... -memprofile`):

1. The carrier travels as base64 text under a normal key (`bp-trace-ctx`) instead of raw bytes
   under a `-bin` key. For `-bin` keys gRPC does the base64 itself, on both ends, with extra copies;
   now the client encodes once straight into the final string (`toBase64`, one allocation) and the
   server decodes into one buffer whose bytes the baggage strings point into. Same bytes on the wire.
2. The server no longer stores the baggage map in the context a second time: `prepare` already put
   that same map on the context and merging updates it in place, so the second store (which the
   wrappers also do) only added a context layer.
3. The client's trailer option, the variable it fills and the option slice come from a pool
   (`trailerCatcher` in `client.go`); the server's trailer map too (`trailerMDs` in `server.go`;
   `grpc.SetTrailer` copies it, checked in grpc-go's `ServerStream.SetTrailer`).

Result: vanilla 237 -> 232 allocations per call, bridge 264 -> 254 (-738 bytes per call); unit
tests (including new malformed-input cases for the text carrier) pass with `-race`; vanilla 50/50
identical to the wrapper build, PB and SB 50/50 with the fixed config.

What is left on top of plain gRPC (bridge: about 50 allocations per call) is mostly the two spans
themselves (the OTel SDK, the same with wrappers), context layers that the SDK and the processors
rely on, `metadata.AppendToOutgoingContext`, and gRPC handling one more header and one more trailer
entry. The span attribute copy in `mergeSpanBaggage` (`Attributes()` returns a copy) has to stay:
processors still write `__bag.` attributes (e.g. the priority processor, `__bag._br`).
(Later the same day this became a per-tracer switch, see "Attribute scan only where needed" below.)

### 2026-10-13: CPU per request, round 2 (after the cheaper metadata path)

`cpu_ab.sh` again, same settings as above. Results in `~/grpc-interceptors-work/cpu2/`.

| kind | wrapper (mean) | interceptor (mean) | difference | noise (largest same-build spread) |
|---|---|---|---|---|
| vanilla | 4.504 ms | 4.411 ms | -0.094 ms (-2.1%) | 0.234 ms |
| PB | 5.014 ms | 5.147 ms | +0.134 ms (+2.7%) | 0.045 ms |
| SB | 5.260 ms | 5.435 ms | +0.175 ms (+3.3%) | 0.072 ms |

Vanilla's spread again comes from one interceptor run (4.243 ms, 400 fewer requests: wrk2 sent
fewer, no errors); without it the interceptor mean is 4.467 ms, -0.8%. SB run 5 overlapped with a
compile I started by mistake; without it SB is +3.2%. So: vanilla even, the bridges about 3% more
expensive with interceptors, every interceptor run above every wrapper run. What the bridges add
over vanilla is the reverse context in the response trailer (see round 1 above), and that part
did not get cheaper.

### 2026-10-13: Further per-call work removed

The user then asked for every further saving, whether or not the wrappers could have it too, and
allowed changing how the interceptors manage baggage as long as what ends up where is unchanged.

1. **One context layer for the whole server state.** `requestState` (`server.go`) now also answers
   the baggage key and the reverse-context accumulator key itself (the backend helpers
   `backend.IsBaggageKey`, `backend.IsRetMergeKey` recognise the unexported keys; `RetMerge` is now
   an exported type). The server adds three context layers per call (remote parent, state, span)
   instead of six, and every context lookup below it (the SDK's, the processors', the client
   interceptor's) walks past three fewer.
2. **Pooled client baggage.** The client's copy of the baggage is only used to build the carrier, so
   the map comes from a pool (`copyBaggage`/`putBaggage` in `client.go`).
3. **Attribute scan only where needed.** `mergeSpanBaggage` reads the new span's attributes to
   find `__bag.` entries. The span has only just started, so only the SDK (span processors'
   `OnStart`, the sampler) can have written any. A tracer can now say whether its SDK ever does
   (`backend.AttributeBaggageReporter` in `runtime/core/backend/span_baggage.go`); the collector
   client asks its bridge processor (`startAttributeBaggage` next to each processor's `OnStart` in
   `runtime/plugins/otelcol`): vanilla, SB and the ranged PB/CGPB windows write none; legacy fixed-cpd
   PB/CGPB do (`__bag._br`, `__bag.prio`); every other processor is assumed to. When the tracer says
   none, the interceptors skip the read (a lock and a copy of the attribute list per span). Test:
   `TestStartAttributeBaggage` (a processor that writes `__bag.` at start; with the tracer reporting
   "none" its baggage is, as it must be, not looked at).

### 2026-10-13: Stack growth: about 9% of all CPU, in both builds

Looking again at the latest PB profiles (`~/grpc-interceptors-work/prof/pb_slim`, 30 s of all 14
services), this time at the whole process rather than the difference between builds:
`runtime.newstack` (a goroutine's stack is too small, so the runtime copies it to one twice the
size and adjusts every frame on it) is 8.2 s of 92.6 s with wrappers and 9.2 s of 95.8 s with
interceptors. SB the same (8.2 s / 8.6 s of ~97 s). That is the largest single item in the
profiles, and nothing to do with tracing as such.

Who pays (the function that crossed the limit, interceptor build, PB):

| where | CPU | goroutine |
|---|---|---|
| gRPC reading the request message (`recvMsg` -> `selectgo` -> `sellock`) | 3.38 s | gRPC's per-call server goroutine |
| `grpc.(*Server).processRPC` itself | 1.47 s | same |
| `onStartCheckpointWindow` (PB's span start) | 1.60 s | the SN workflow's per-call goroutines (`ComposePost.func1..7`) |
| context lookups (`context.value`, `efaceeq`) | 1.2 s | same |

Why: gRPC runs every incoming call on a new goroutine, and the SN workflow makes every downstream
call on a new goroutine. A new goroutine starts with a small stack (the runtime picks the start
size from the average stack it sees at garbage collection, and most goroutines in these processes
are small idle ones). A call that goes deeper than that is copied, maybe more than once, and the
copy cost grows with the number of frames on the stack, so it is paid deep down, where it is most
expensive. (Found with `stack_growth.py`, testing section: it sums `runtime.newstack` samples by the
frame right below it and by the goroutine's bottom frame.)

Two changes:

1. **gRPC stream workers** (server side). gRPC can run calls on a pool of reused goroutines
   (`grpc.NumStreamWorkers`, experimental; grpc-go's own comment cites exactly this problem,
   golang/go#18138). Their stacks stay grown from call to call. When all workers are busy, gRPC
   falls back to a new goroutine, so the number only limits how many calls get a reused stack,
   never how many run. The generated server (`plugins/grpc/grpccodegen/servergen.go`) now sets it
   from `GRPC_STREAM_WORKERS` at startup: unset means GOMAXPROCS (gRPC's suggestion; after the
   sweep below: 32, or GOMAXPROCS if larger), 0 turns it off. This is in the gRPC plugin, so it applies to wrapper builds too. Caveat to measure: an idle
   worker's stack is halved at every garbage collection where it uses less than a quarter of it,
   so too many workers, each idle for long, may shrink and regrow.
2. **Smaller span-start frames** (client side). `PathBridgeProcessor.OnStart` had a 1200-byte stack
   frame and `CallGraphBridgeProcessor.OnStart` 1320, although in ranged mode both just call
   `onStartCheckpointWindow` and return: Go sizes a frame for everything in the function, and the
   legacy fixed-cpd code was inline below the early return. That code now lives in `onStartFixed`;
   `OnStart` is 64 bytes. Behaviour unchanged (the same code runs in the same order). SB's `OnStart`
   (1472 bytes) is all live work; left alone for now.

To measure settings on the same binaries: `cpu_env_ab.sh` (testing section).

**Worker sweep** (PB interceptor build with both changes above and the attribute gate;
`cpu_env_ab.sh`, one A-B-B-A round per pair, 700 req/s, 60 s, profiles on in every run, results in
`~/grpc-interceptors-work/workers/`):

| GRPC_STREAM_WORKERS | CPU per request, off (0) | with workers | difference |
|---|---|---|---|
| 2 (GOMAXPROCS here) | 5.168 ms | 5.010 ms | -3.1% |
| 8 | 5.112 ms | 4.868 ms | -4.8% |
| 32 | 5.124 ms | 4.879 ms | -4.8% |

(Largest same-arm spread 0.026 ms.) The leaf services save about 9%, HomeTimeline 11%; ComposePost
and Text, whose handlers fan out on goroutines of their own, hardly change; wrk2api is the HTTP
front end and has no gRPC server. So the default became 32, or GOMAXPROCS if larger: gRPC suggests
GOMAXPROCS, but a handler that waits for its downstream calls keeps its worker, so more calls are
open at once than there are CPUs, and 32 idle workers cost nothing measurable here.

With 32 workers, stack growth on gRPC's server goroutines went from 5.4 s to 0.9 s (of ~92 s). What
remains (4.0-4.3 s, about 4.5% of all CPU) is on the SN workflow's per-call goroutines.

**The OnStart split did not reduce stack growth.** With it, `onStartCheckpointWindow` no longer
appears as the place where stacks grow, but the per-call goroutines now grow a little later, in
gRPC's client path (`addrConn.getReadyTransport` 1.5 s, `http2Client.NewStream` 1.0 s), and their
total is unchanged. Those goroutines need a bigger stack than they start with, whatever runs first.
The split is harmless and stays (OnStart is 64 bytes instead of 1200), but it is not a saving.

**3. Client call workers.** The SN workflow makes every fan-out call on a new goroutine, and
those are application code. The client interceptor can do what gRPC does for incoming calls: hand
the traced call to a worker goroutine whose stack is already grown, and wait for it
(`workers.go`). For the caller nothing changes: same arguments, same result, a panic in the call is
raised again in the caller (test `TestClientWorkersResultAndPanic`). If no worker is idle, the
caller makes the call itself (`TestClientWorkersBusyFallback`), so nothing ever waits for a worker.
`OT_CLIENT_WORKERS` sets the number for the process (all client interceptors share them), 0 turns
them off, unset is 32. The whole test suite passes with `-race` both ways.

Microbenchmark (`bench_test.go`, new variants `/Workers` and `/GoPerCall`, mean of 3 runs of
20000 calls each, CPUs 0-5, idle machine), microseconds per call:

| caller | client workers off | 32 client workers |
|---|---|---|
| Vanilla, one long-lived goroutine | 147.5 | 152.5 (+5.0) |
| Vanilla, new goroutine per call | 173.6 | 156.3 (-17.3) |
| Vanilla, new goroutine per call + server workers | 159.1 | 140.6 (-18.5) |
| Bridge, one long-lived goroutine | 160.7 | 165.5 (+4.8) |
| Bridge, new goroutine per call | 188.0 | 167.9 (-20.1) |
| Bridge, new goroutine per call + server workers | 170.3 | 153.4 (-16.9) |

Server workers alone: NoTracing 121.6 -> 99.6, Vanilla 147.5 -> 131.5, Bridge 160.7 -> 145.3.
Handing a call to a worker costs about 5 microseconds (mostly waking an idle thread), which is pure
cost when the caller's stack is already big, and saves 17-20 when the caller is a new goroutine.
Which wins in the app depends on how it makes its calls, so it is measured there (next).

**Client call workers in the app: no gain, removed.** `cpu_env_ab.sh` on the PB interceptor build
(server workers at the default), `OT_CLIENT_WORKERS=0` vs `32`, two A-B-B-A rounds, no profiling
(`~/grpc-interceptors-work/workers/pb_client_0_vs_32/`): 4.899 vs 4.914 ms per request (+0.3%,
noise 0.160 ms). Per service it is the trade the microbenchmark predicted: Text -2% and ComposePost
-0.5% (calls on fresh goroutines), HomeTimeline and wrk2api +3% each (calls from goroutines whose
stacks are already big, where the hand-off is pure cost), net zero. Part of the per-call goroutines'
stack growth also happens before the interceptor is reached (`context.WithTimeout` in Blueprint's
generated client), where no interceptor can help. So `workers.go` was deleted again and the client
interceptor is back to doing the call on the caller's goroutine (its closure frame: 288 bytes). The
benchmark variants `/Workers` and `/GoPerCall` stay. What would help the per-call goroutines is
the application not starting a goroutine per call, which is outside this work.

While the client workers were in, trace validation ran on builds with them (and with the server
workers, the gate and the OnStart split), `ab_trio.sh`, 2000 requests per run
(`~/grpc-interceptors-work/runs_r3/`): vanilla 2000/2000 identical to the wrapper build and TVD
0.000; PB (fixed config) 2000/2000, TVD 0.000; SB (fixed config) 2000/2000, largest TVD 0.250
(wrapper vs wrapper 0.143), within the range of the known start-ordinal race shift (0.26-0.44
before, see Step 2c). The final builds differ from those only in the client interceptor's
threading, which the removal returned to what was validated before.

### 2026-10-13: CPU per request, round 3 (final code)

Final code: everything above, with gRPC stream workers at the default (32) in BOTH builds (it is in
the gRPC plugin), the attribute gate and the OnStart split, no client call workers. `cpu_ab.sh`,
two A-B-B-A rounds, same rates as before. Results in `~/grpc-interceptors-work/cpu3/`.

| kind | wrapper (mean) | interceptor (mean) | difference | noise | round 2 (wrapper / interceptor) |
|---|---|---|---|---|---|
| vanilla | 4.274 ms | 4.269 ms | -0.005 ms (-0.1%) | 0.034 ms | 4.504 / 4.411 |
| PB | 4.820 ms | 4.948 ms | +0.127 ms (+2.6%) | 0.085 ms | 5.014 / 5.147 |
| SB | 5.076 ms | 5.182 ms | +0.105 ms (+2.1%) | 0.083 ms | 5.260 / 5.435 |

(SB wrapper run 4 served 350 fewer requests, the wrk2 pacing effect seen before; without it SB is
+2.5%.) Both builds are 4-5% cheaper than in round 2, the stream workers' saving. Between the
builds nothing changed: vanilla is even, and the bridges still pay about 2-3% for the reverse
context in the response trailer (HTTP/2 header compression of a value that is new on every call,
and gRPC's trailer copies; see round 1), which none of today's changes touch.

Against the build the day started with: this morning's PB wrapper build (no stream workers, the
round 2 baseline) vs the final PB interceptor build, one A-B-B-A round: 4.985 vs 4.941 ms (-0.9%,
noise 0.020 ms). So the interceptor build is now cheaper than the wrapper build was, but only
because of the stream workers, which a wrapper build gets as well.

Trace validation of these final builds (`ab_trio.sh`, 2000 requests per run,
`~/grpc-interceptors-work/runs_r4/`): vanilla 2000/2000 identical to the wrapper build, TVD 0.000;
PB (fixed config) 2000/2000, TVD 0.000; SB (fixed config) 2000/2000, largest TVD 0.150 (wrapper vs
wrapper 0.050), the known start-ordinal race shift.

### 2026-10-13: end-to-end latency, PB, with and without stream workers (final code)

`latency_ab.sh` (method as in round 1), PB, new wrapper vs new interceptor build, run twice: with
the stream workers at the default, and with `GRPC_STREAM_WORKERS=0` exported (off in both builds).
Results in `~/grpc-interceptors-work/lat5/pb_workers` and `lat5/pb_noworkers`. Mean / p99 in ms;
noise = largest gap between two repetitions of the same build.

| load | wrapper, workers | interceptor, workers | wrapper, no workers | interceptor, no workers |
|---|---|---|---|---|
| unloaded | 3.02 / 5.19 | 3.11 / 5.44 | 3.12 / 4.96 | 3.23 / 5.30 |
| 300 req/s | 19.6 / 31.3 | 20.7 / 33.1 | 21.5 / 34.1 | 22.1 / 34.4 |
| 600 req/s | 20.6 / 35.3 | 21.2 / 34.9 | 21.4 / 33.7 | 22.3 / 34.7 |
| 900 req/s | 23.1 / 68.1 | 25.5 / 76.5 | 25.7 / 67.8 | 28.3 / 71.9 |
| 1050 req/s | 71 / 221 | 550 / 1190 | 810 / 1665 | 1640 / 2700 |
| throughput at 1200 req/s | 1,054 | 1,022 | 1,002 | 969 |

(Noise of the mean: 0.00-1.2 ms below saturation with workers, 0.09-2.0 without.)

Interceptor vs wrapper, both with workers: +0.09 ms unloaded, +0.6 to +2.4 ms mean under load,
3.0% lower peak throughput; the same picture without workers (+0.11 ms, 3.4% lower peak). This is
the CPU premium of the bridges' trailer path (round 3: +2.6% CPU), showing up where the CPUs run out.

Stream workers, each build against itself: lower mean latency at every load (about 0.1 ms
unloaded, 1-3 ms under load) and 5.2-5.5% more peak throughput, which is the CPU saving. Handing
calls to workers has no visible latency cost. (The two comparisons ran one after the other, so
"workers vs no workers" compares across runs; the differences are well above the repetition noise.)

### 2026-10-14: Huffman-proof header text

Asked for any further tricks, I profiled the final PB builds again (`profile_ab.sh`, 700 req/s,
`~/grpc-interceptors-work/prof/pb_final2`) and split all CPU by area with the new `cpu_areas.py`.
Interceptor build minus wrapper build: HTTP/2 header compression +2.35 s (5.0 -> 7.4 s of ~91 s),
the only area that differs by more than the noise. By function, most of it is Huffman: x/net's
`hpack.huffmanDecode` 0.07 s with wrappers, 0.75 s with interceptors (plus 0.12 s in the
`bytes.Buffer.WriteByte` it calls per byte), `AppendHuffmanString` 0.15 s; the rest is the header
table's string-keyed maps (this CPU has no AES instructions, so Go hashes strings with its slower
fallback). HPACK Huffman-codes a value whenever that makes it shorter, and base64 text always gets
shorter, so both our values (carrier and reverse context) were Huffman-coded and decoded on every
call.

Change (`header.go`): both values are written with a base64 alphabet made of the visible ASCII
characters with the longest Huffman codes, so HPACK sends them as plain bytes. Tested on 5000
carriers shaped like PB's and 5000 reverse contexts: 3 carriers and 178 reverse contexts would
still be Huffman-coded (with RawURL base64: all of them); that only costs speed, nothing depends on
it. Price: these two headers are about 23% bigger on the wire. The reverse context is produced by
the backend as RawURL base64; the server re-encodes its bytes behind a `!` marker and the client
turns it back, exactly (canonical text only; anything else, e.g. the legacy JSON envelope, passes
through untouched; `TestRetCtxHeaderRoundTrip`). The carrier decoder now also rejects trailing
bytes after a complete record (as the wrappers' JSON parser rejects trailing data).
Microbenchmark: 1.0-1.6 microseconds less per call (about 1%) in every variant.

Other candidates looked at and left alone (each well under 1%, or not ours to change): the
interceptors' own remaining code (3.8 s: mostly allocations and maps that gRPC's metadata API and
the processors' baggage map require), the sampler's context lookup (0.3 s, but it must keep the
parent's trace state), span ID generation (0.1 s), the bridge wire side table (0.3 s, already
sharded), the OTLP export (2.8 s, mostly protobuf marshalling inside the exporter), pooling the
processors' OTLP objects (maybe 0.5%, but changes how exported messages are reused), and garbage
collection (9% of CPU, but GOGC is an experiment setting: the deploy scripts' GC modes).

**Result in the app: not measurable.** `cpu_ab.sh` with `LABEL_A=old LABEL_B=new`, PB, current
interceptor build vs the same code with header.go, two A-B-B-A rounds (`~/grpc-interceptors-work/
cpu4/pb`): 4.916 vs 4.917 ms per request (+0.0%, noise 0.062 ms). To see whether the change did
what it should, both builds were profiled (`prof/pb_hdr_old_vs_new`, `profile_ab.sh` with "wrapper"
= old, "interceptor" = new): header compression 7.45 -> 6.44 s of ~91 s (Huffman decoding 0.80 ->
0.24 s, Huffman encoding gone), the transport goroutines -0.93 s, as intended; but the client's own
code +0.35 s (turning the reverse context back into the backend's text) and syscalls +0.39 s
(consistent with the bigger headers), and areas the change does not touch moved by +-0.3-0.7 s
(export +0.72, protobuf +0.50: noise). Net somewhere between 0 and -0.5%, below what an 8-run A/B
resolves (about 1%). The SB and vanilla A/Bs were stopped after this (`cpu4/sb_aborted` holds one
unfinished run), and the header-text builds (`*_interceptor_hdr`) have not been trace-validated.

Incident: the root disk (63 GB, holding /tmp and /users) filled up during these builds: each build
is ~720 MB and about 30 had accumulated. It broke the SB build and Claude Code's own tool output.
Deleted the `bin/` directories of obsolete builds (rebuildable), stopped and restarted the
experiment; PB's build had completed before the disk filled.

### 2026-10-14: The bridges' `_br` as raw bytes instead of base64

How `_br` (the bridge processors' propagated state) travelled per RPC: the client span's processor
base64-encoded its outgoing `_br` into the baggage (`encodeBR`), our carrier wrote that text into
its binary record (so the header carried base64 of base64), the server decoded the carrier, and
the server span's processor base64-decoded `_br` again (`decodeBR`). The server span then encoded
its own `_br` for its children, and so on: per RPC four base64 passes for `_br`, plus a third more
header bytes. In the PB profiles `encodeBR` + `decodeBR` alone were 0.35 s of ~91 s.

Change: the bridges that keep their state beside the span (ranged PB/CGPB windows, SB) put the
raw bytes into the baggage as a Go string (`propBR`/`parsePropBR` in `runtime/plugins/otelcol/
pack.go`; strings may hold any bytes). A plain copy replaces each base64 pass; the allocations stay
the same. Our carrier needs no change: it already writes values as length-prefixed bytes.

What had to change elsewhere, so nothing that needs text ever sees raw bytes:

* The wrappers' JSON carrier (`runtime/core/backend/carrier_json.go`, `trace.go`): a JSON string
  cannot hold bytes that are not UTF-8 (encoding/json replaces them with U+FFFD). Such values now
  travel base64 in a separate `"baggage_bin"` object, written only when there are any, so every
  other carrier is byte for byte what it was (vanilla's, for one). Fast encoder, fast parser and the
  encoding/json fallback all handle it; the byte-equality tests against encoding/json now use a
  reference written out in the test, and the round trip is checked value by value (before, only the
  number of entries could be checked, because invalid UTF-8 came back as U+FFFD).
* Span attributes keep text (`encodeBR`): OTLP string values must be valid UTF-8. The legacy
  fixed-cpd PB/CGPB carry `_br` through their `__bag._br` attribute, so they stay text end to end.
  Nothing puts the baggage `_br` on a span.
* Tests: fixtures that built a parent's baggage `_br` now build the form the bridge uses (raw for
  SB and the ranged windows, text for the legacy mode); a test helper that copied baggage "through
  JSON, as the wrappers do" with plain encoding/json now uses the wrappers' carrier code. The
  interceptor carrier test (5000 random cases against the JSON path) now includes raw `_br` values.
  All otelcol, backend and interceptor tests pass with `-race`.

Trace validation (`ab_trio.sh`, fixed config, 2000 requests per run, `~/grpc-interceptors-work/
runs_r6/`), builds `{pb,sb}_{wrapper,interceptor}_raw`:

| comparison | traces | largest attribute TVD (same build: noise) |
|---|---|---|
| PB, new wrapper vs new interceptor | 2000/2000 identical | 0.000 (0.000) |
| PB, old wrapper vs new wrapper | 2000/2000 identical | 0.000 (0.000) |
| SB, new wrapper vs new interceptor | 2000/2000 identical | 0.234 (0.049) |
| SB, old wrapper vs new wrapper | 2000/2000 identical | 0.214 (0.061) |

PB computes exactly the same with raw `_br`, through interceptors and through the wrappers' JSON
carrier. In SB, every attribute above 0.07 is one of the three that depend on which concurrent
child reaches the parent's counter first (`_o`, and `_br` sizes and `_rc` lengths, which encode
it): the known start-ordinal race (Step 2c). The new wrapper build moved the `_br` base64 from
the span start into the JSON carrier, which changes the timing of ComposePost's fan-out a little,
so Media and Text win the race in different proportions (e.g. Media's ordinal 2: 81-85% -> 64%).
All other attributes agree within the noise.

CPU, PB, header-text build (old) vs the same plus raw `_br` (new), `cpu_ab.sh` with labels, two
A-B-B-A rounds (`~/grpc-interceptors-work/cpu5/pb`): 4.896 vs 4.922 ms per request (+0.5%, noise
0.052 ms): not measurable. Profiles of the same pair (`prof/pb_rawbr`, one run each): the bridges'
`_br` conversions 0.58 -> 0.08 s of ~91 s, all base64 0.76 -> 0.40 s, carrier decoding 0.76 ->
0.65 s; total CPU per request in that pair 4.958 -> 4.866 (-1.9%, one run, within the noise). So
it does what it should, about 0.5 s of ~91 s, which is below what an 8-run A/B resolves (about 1%).

### 2026-10-14: Throughput as the measure for small changes

The header-text and raw-`_br` changes were each below what the CPU A/B resolves (about 1% between
runs of the same build). The latency runs had recorded peak throughput too (offered 1,200 req/s at
the end of each ramp), and there two repetitions of the same build were only 0.1-0.6% apart (lat4,
lat5: e.g. PB wrapper 1,051 / 1,057, PB interceptor 1,023 / 1,021, SB interceptor 907 / 912). So
the new `tput_ab.sh` measures peak throughput directly: fresh start, warm-up at 700 req/s, then
1,600 req/s offered for 45 s (vanilla had reached 1,190 at 1,200 offered, so 1,200 was too close
to its capacity), A-B-B-A rounds. With 32 connections at overload, each connection always has one
request outstanding, so the achieved rate is how fast the services on CPUs 0-5 complete call trees.

First use: PB, yesterday's final interceptor build (`pb_interceptor`, "old") vs header text +
raw `_br` (`pb_interceptor_raw`, "new"), two rounds (`~/grpc-interceptors-work/tput/pb_tricks`):

| run | 1 old | 2 new | 3 new | 4 old | 5 old | 6 new | 7 new | 8 old |
|---|---|---|---|---|---|---|---|---|
| req/s at 1,600 offered | 992.3 | 1000.7 | 1007.1 | 998.5 | 1001.4 | 1007.5 | 1010.8 | 1002.7 |

New - old: +7.8 req/s (+0.78%); per ABBA round +8.5 (+0.85%) and +7.1 (+0.72%); every new run is
6-9 req/s above the old run next to it. The per-build spread (10 req/s, 1.0%) is mostly a steady
upward drift of the machine over the 17 minutes (old: 992, 998, 1001, 1003), which the ABBA order
cancels out of each round's difference. No socket errors in any run. The warm-up steps (CPU per
request at 700 req/s) gave -0.34% (rounds -0.22%, -0.45%; spread 1.1%): the same direction, but
not resolvable on its own. +0.8% capacity matches the profiles' estimate for the two changes
(header compression and base64 together about 1% of CPU, minus the reverse-context conversion).

Absolute rates are lower than in the latency runs (PB interceptor 999 vs 1,022 there): those
measured after a full ramp at 1,200 offered, these on fresh services at 1,600. Compare only within
one method.

### 2026-10-14: HTTP middleware (the frontend without wrappers)

Asked whether HTTP could be traced the interceptor way too, without generated wrapper code. Yes:
the standard net/http hook points are a handler wrapper on the server and a transport wrapper
(`http.RoundTripper`) on the client. The user chose response headers for the way back.

Design (mirrors Step 1 and Step 2):

* `runtime/plugins/httpmiddleware` (new): the types the generated code takes,
  `ServerMiddleware func(service, method string, next http.Handler) http.Handler` and
  `ClientMiddleware func(service string, next http.RoundTripper) http.RoundTripper`. HTTP has no
  method descriptor like gRPC's `FullMethod`, so the generated server names the method of each
  handler it wraps, and a client's middleware takes the method from the path (`/<Method>`). Plus
  `ErrorRecorder`/`RecordError`: an HTTP response only carries a status and a message, so the
  generated handler hands the service's error itself to a middleware that asks for it.
* `plugins/http`: `AddServerMiddleware` / `AddClientMiddleware` (properties on the server/client
  nodes, like the gRPC calls), `ServerMiddleware`/`ClientMiddleware` node interfaces
  (`middleware.go`). Generated server: every route's handler is wrapped
  (`handler.wrap("<Method>", ...)`); the service now gets `context.WithoutCancel(r.Context())`
  instead of `context.Background()` (middleware's values reach it; no cancellation, as before);
  on a service error it calls `httpmiddleware.RecordError(w, err)` before `http.Error`. Generated
  client: the transport is wrapped; requests are sent with `http.NewRequestWithContext(
  context.WithoutCancel(ctx), ...)` instead of `Client.Get(url)` (the caller's values reach the
  middleware; no cancellation, as before).
* `plugins/opentelemetry`: `InstrumentHTTP(spec, service, collector)` (falls back to `Instrument`
  with `OT_GRPC_MODE=wrapper`, so a wrapper build is wrappers everywhere). The SN (`docker`,
  `docker_nw`), hotel (`original`) and leaf (`docker_fanout`, `docker_tree`) specs use it for their
  HTTP services instead of `Instrument`.
* `runtime/plugins/opentelemetry/interceptors/http_server.go`, `http_client.go`: the gRPC steps,
  shared with the interceptors (the per-call helpers were split into what any transport needs and
  what gRPC delivers; the gRPC per-call frames are unchanged, 256 and 288 bytes):
  * server: carrier from the `Bp-Trace-Ctx` request header; steps 8-9 run when the handler starts
    the response (first `WriteHeader`/`Write`) or returns without writing, and the reverse context
    goes in the `Bp-Ret-Ctx` response header, only for a 2xx response without a service error
    (wrappers drop the whole response of a failed call, retCtx with it);
  * client: carrier in a copy of the request (a `RoundTripper` must not change the caller's
    request); the reverse context is read from the response headers as soon as they arrive; the
    span ends when the caller closes the body, i.e. after the generated client read and decoded
    the response, as far as the wrapper's span reached. An error status is recorded as
    `StatusCode was <n>`, the error the generated client returns and the wrapper recorded.

Known differences from the wrappers (all in error cases, none occurs in the no-work apps): a
request whose arguments do not decode gets a server span (the wrapper was never reached); the
client span records the transport's error rather than the generated client's wrapped `url.Error`,
and errors the generated client finds while decoding a 2xx response are not recorded. As with
gRPC, the server span now also covers argument decoding and response encoding.

Tests: the interceptor tests now run over both transports (`transports` in
`interceptors_test.go`: the HTTP version serves `/Check` of a service `Health` the way the
generated code does), so the HTTP middleware is held to every assertion the gRPC interceptors are
(span tree, names, scopes, kinds; baggage; `__bag.` attributes; unsampled traces; reverse context
on success and not on error; errors recorded; child ordinals and the bridges' child attributes,
SB end events). HTTP-only tests (`http_test.go`): a handler that writes nothing still returns the
reverse context; an error status without a service error returns none and records no exception;
the client span lasts until the body is closed (and only ends once); the caller's request is not
changed; a transport error is recorded. All pass with `-race`.

Codegen diff, PB, before vs after (`codegen_diff.sh pb_interceptor_raw pb_interceptor_http`): only
the two HTTP services change (`wrk2api` and the trace-pressure service): their generated HTTP
servers, their two process files, and their wrapper files are gone. No gRPC file changed.

Trace validation (`ab_trio.sh`, 2000 requests per run, `~/grpc-interceptors-work/runs_r7/`),
wrapper builds vs interceptor builds whose frontend now has the middleware: vanilla 2000/2000
identical, TVD 0.000; PB (fixed config) 2000/2000, TVD 0.000; SB (fixed config) 2000/2000, largest
TVD 0.226 (wrapper vs wrapper 0.063), only `_o` and `_br`: the start-ordinal race, as before.

### 2026-10-14: No decoding and re-encoding of unchanged data

Asked to remove every decode-and-re-encode cycle where nothing changed in between. What was left
on the paths our runs use (the `_br` baggage and the header text were already done):

* **The reverse context** was base64 text in the process (the packed envelope's RawURL base64).
  Per hop it was decoded and re-encoded with no change: on every child added to the fan-in (the
  whole accumulator each time, `mergePackedRetCtx`), at each checkpoint preparation that changed
  nothing, and twice more by the interceptors to put it in a header (RawURL -> header alphabet ->
  RawURL). Now it is the raw envelope bytes in a Go string (`reversetruss_packed.go`): merging
  appends the second envelope's records; routing parses the bytes directly and still returns the
  very same string when nothing changed; the interceptors encode it once for the header and decode
  it once on arrival (`header.go`: plain bytes to text, so the `!` marker and the pass-through for
  other forms are gone). Text is made only where text has to travel: the wrappers' response field
  (`RetCtxText` in the server templates, `RetCtxFromText` in the client templates; their wire format
  is unchanged) and the exported `_rc` attribute (unchanged format, tested:
  `TestSDKUpstreamReverseCheckpointPriority` checks it is exactly the RawURL text). The decoders
  accept both forms. A raw envelope starts with byte 0x01, which no base64 alphabet has, so the two
  forms cannot be confused. New test `TestRetCtxRawAndText`; the interceptors' header test now
  checks the header text is printable ASCII (the old code would have put raw bytes in the trailer:
  the gRPC version here let them through, which is why only the new check caught it).
* **SB's `remEndEvents`** (the end-event summary for the SB processor) was base64-encoded into a
  span attribute by the wrapper and the interceptor and decoded by the processor at the span's end,
  in the same process; the attribute is never exported. Now raw bytes.

Left as they are, because something does change in between: the forward carrier (new span ID,
`__seq`, `_br` per call), a TTL step of the reverse context (the TTL byte changes), and the copy the
bridges take of `_br` (they write into it).

### 2026-10-14: Final builds, validation

`~/grpc-interceptors-work/{v,pb,sb}_{wrapper,interceptor}_final`: everything above (header text,
raw `_br`, HTTP middleware, raw reverse context, raw `remEndEvents`); the wrapper builds are
`OT_GRPC_MODE=wrapper`, i.e. wrappers on every service, the frontend included. Trace validation
(`ab_trio.sh`, `runs_r8/`): vanilla 2000/2000 identical, TVD 0.000; SB (fixed config) 2000/2000,
largest TVD 0.219 (wrapper vs wrapper 0.019), only `_o`/`_br`: the start-ordinal race; PB (fixed
config) 1999/2000 in both comparisons, wrapper vs wrapper included, TVD 0.001.

The PB outlier is the first request of every run (all three runs): it took longer than gRPC's 1 s
deadline, the frontend's call failed with DeadlineExceeded, the retry wrappers repeated it, and it
succeeded after 2.3-2.9 s, so its trace has the retried spans (46023/46024/46051 spans instead of
46000). Every later request is normal. Looking back, the first request took ~10 ms in yesterday's
runs (`runs_r4`) but 200-600 ms in most of today's (`runs_r6`, `r7`, `r8`), wrapper builds as much
as interceptor builds, and two runs of the same build agree to 0.1 ms (2890.62 / 2890.55 ms;
207.39 / 207.36 ms): the request waits on fixed timeouts or timers, not on CPU or disk. Not caused
by the interceptor code (wrapper-only runs show it too); not investigated yet, because that needs
the app running and the performance measurements had the machine (see the next entry). The
performance measurements are not affected: every one either warms up first or drops wrk2's first
10 s.

### 2026-10-14: Performance of the final builds

Wrappers vs interceptors, final builds (previous entry), one measurement at a time with nothing else
running (`~/grpc-interceptors-work/r9_perf.sh`, results in `~/grpc-interceptors-work/final/`). Real
SN bridge config (CPD 2..6, `inverse_depth`), `GOMAXPROCS=2`, services on CPUs 0-5, wrk2 on 6-7.

**Peak throughput** (`tput_ab.sh`: fresh start, 45 s warm-up at 700 req/s, then 45 s at 1,600 req/s
offered; two A-B-B-A rounds, 4 runs per build):

| | wrapper | interceptor | difference | per ABBA round |
|---|---|---|---|---|
| vanilla | 1,233.3 req/s | 1,243.1 req/s | **+0.8%** | +0.3%, +1.3% |
| PB | 1,009.5 | 1,019.2 | **+1.0%** | +0.7%, +1.2% |
| SB | 954.2 | 967.8 | **+1.4%** | +1.3%, +1.6% |
| CGPB | 1,005.7 | 1,017.1 | **+1.1%** | +1.0%, +1.3% |

For every kind, every interceptor run is above every wrapper run (vanilla 1,241-1,246 vs
1,221-1,238; PB 1,016-1,020 vs 1,001-1,014; SB 965-969 vs 949-959; CGPB 1,013-1,020 vs
1,001-1,010). No socket errors. (CGPB was measured after the others, `~/grpc-interceptors-work/
r10_cgpb.sh`: builds `cgpb_{wrapper,interceptor}_final` from the same tree; trace validation
2000/2000 identical, 46000 spans per run, largest TVD 0.088 (wrapper vs wrapper 0.035), only the
`_br`/`_rc` lengths of ComposePost's concurrent children, which depend on their start order.)

**CPU per request** (`cpu_ab.sh`, vanilla 800 req/s, PB/SB 700 req/s, 60 s, two A-B-B-A rounds):

| | wrapper | interceptor | difference | noise (largest same-build spread) |
|---|---|---|---|---|
| vanilla | 4.224 ms | 4.215 ms | -0.2% | 0.251 ms (6%): not resolvable today |
| PB | 4.918 | 4.858 | **-1.2%** | 0.094 ms |
| SB | 5.154 | 5.101 | **-1.0%** | 0.044 ms |
| CGPB | 4.901 | 4.873 | -0.6% | 0.053 ms (1.1%) |

PB and SB: every interceptor run is below every wrapper run. The warm-up steps of the throughput
runs (700 req/s, 45 s) agree: vanilla -0.4%, PB -0.8%, SB -0.9%. CGPB: -0.6%, within the noise
(its warm-ups: +0.2%), yet +1.1% peak throughput.

Vanilla again with 4 rounds (`final/cpu_v_4rounds`, 8 runs per build): 4.267 vs 4.211 ms, -1.3%,
but that comes from two interceptor runs that were *stalled* (below). Some runs today are missing
~400 requests (47,570 instead of 47,970 at 800 req/s for 60 s; the same kind of half-second stall as
in the 600 req/s latency steps), and their CPU per request is off (vanilla: 4.05-4.09 ms against
4.22-4.30 for every other run; one PB wrapper run: 4.981 against 4.88-4.91). Without the stalled
runs (a run counts as stalled when it has 100 requests fewer than the most any run of that set had):

| | stalled runs | wrapper | interceptor | difference |
|---|---|---|---|---|
| vanilla, 2 rounds | 1 W, 1 I | 4.278 (3 runs) | 4.261 (3) | -0.4% |
| vanilla, 4 rounds | 2 I | 4.267 (8) | 4.258 (6) | -0.2% |
| PB | 1 W | 4.898 (3) | 4.858 (4) | -0.8% |
| SB | none | 5.154 | 5.101 | -1.0% |
| CGPB | none | 4.901 | 4.873 | -0.6% |

So vanilla is even on CPU (same-build spread ~0.06 ms, 1.4%), and the bridges are 0.6-1.0% cheaper
with interceptors.

**Position in the sequence.** In all four throughput sets, run 5 (the first run of round 2, always
a wrapper run in A-B-B-A order) is the lowest: vanilla 1,220.7, PB 1,001.2, SB 948.5, CGPB 1,003.8.
Its warm-up step is not stalled, so the cause is not visible. Without run 5 the interceptors still
win everywhere, and every interceptor run is still above every remaining wrapper run: vanilla +0.5%,
PB +0.7%, SB +1.2%, CGPB +1.1%. To cancel any effect of the position, the four sets were run again
with the order reversed (interceptor, wrapper, wrapper, interceptor per round; `r11_reversed.sh`,
`final/tput_rev_*`, the evening of 2026-10-14):

| | interceptor | wrapper | difference | per round | first order (above) | both orders |
|---|---|---|---|---|---|---|
| vanilla | 1,250.8 req/s | 1,244.7 | +0.5% | +0.6%, +0.4% | +0.8% | **+0.6%** |
| PB | 1,038.3 | 1,028.6 | +0.9% | +1.1%, +0.8% | +1.0% | **+1.0%** |
| SB | 988.6 | 972.8 | +1.6% | +1.1%, +2.1% | +1.4% | **+1.5%** |
| CGPB | 1,034.2 | 1,024.8 | +0.9% | +0.8%, +1.0% | +1.1% | **+1.0%** |

Run 5, now an interceptor run, was low again only for vanilla (1,235.9 against 1,253.7-1,258.4 for
the other interceptor runs); for the bridges it was not. For PB, SB and CGPB every interceptor run
is again above every wrapper run; for vanilla all but that run 5. The position in the sequence does
not explain the gap: reversed, the bridges show the same advantage (PB +0.9 vs +1.0, SB +1.6 vs
+1.4, CGPB +0.9 vs +1.1), vanilla a little less (+0.5 vs +0.8). Absolute rates were ~1-2% higher
that evening than in the afternoon (machine drift between sessions; compare within a set only).

**Latency** (`latency_ab.sh`, method as in round 1: unloaded = 1 connection at 100 req/s; ramp =
32 connections, 300 to 1,200 req/s; W-I-I-W; noise = largest gap between two repetitions of the
same build). Mean latency in ms (p99 in brackets where it matters):

| load | vanilla W / I | PB W / I | SB W / I | CGPB W / I |
|---|---|---|---|---|
| unloaded | 2.71 / 2.74 (noise 0.02) | 3.02 / 3.08 (noise 0.11) | 3.17 / 3.21 (noise 0.08) | 3.05 / 3.03 (noise 0.07) |
| 300 req/s | 16.5 / 16.3 | 20.6 / 20.3 | 21.9 / 21.5 | 20.9 / 20.5 |
| 600 req/s | 16.3 / 16.3 | 17.9 / 20.6 (noise 6.0) | 19.1 / 21.8 (noise 5.9) | 21.2 / 17.4 (noise 6.6) |
| 900 req/s | 17.0 / 17.9 (noise 1.6) | 23.6 / 23.0 | 30.7 / 27.1 (p99 95 / 70) | 24.5 / 23.0 (p99 67 / 52) |
| 1,050 req/s | 18.5 / 18.7 | 418 / 291 (p99 962 / 710) | 1,540 / 1,240 (p99 2,650 / 2,165) | 591 / 306 (p99 1,310 / 811) |
| 1,200 req/s | 34.6 / 32.1 | 3,295 / 3,000 | 4,335 / 4,055 | 3,440 / 3,185 |
| achieved at 1,200 | 1,197.5 / 1,198.2 | 1,023.9 / 1,039.9 | 968.2 / 983.8 | 1,015.5 / 1,030.4 |

Unloaded, the interceptor builds are 0.03-0.06 ms slower per ComposePost, within or near the
repetition noise. Under load, every difference is within the noise up to 900 req/s, and near
saturation the interceptor builds have clearly lower latency and 1.5-1.6% more throughput (PB, SB,
CGPB), which is the CPU saving showing up where the CPUs run out.

The 600 req/s rows are noisy for a reason outside both builds: in 4 of today's 15 ramps (PB, SB
and CGPB, wrapper and interceptor runs alike) the 600 req/s step is missing ~290 requests
(20,670-20,675 instead of ~20,965, about half a second of load) and its mean latency drops to
14-16 ms (32 connections then fire less in bunches). None of yesterday's ramps (`lat5`) shows it.
Like the slow first request, it points at short stalls of the machine that started today, possibly
the nearly full disk (86-97% used today, ~81% yesterday). Both builds are affected equally (ABBA),
and the noise column covers it. Vanilla is not saturated at 1,200 req/s any more (its
capacity is ~1,240, see the throughput table), so its ramp shows no knee.

Compared with round 3 (2026-10-13: PB +2.6%, SB +2.1% CPU, and in the latency runs 3.0% / 5.2%
lower peak throughput for interceptors): the interceptors are now cheaper than the wrappers.

**What today's changes bought the interceptors** (`tput_ab.sh`, PB, yesterday's final interceptor
build `pb_interceptor` vs today's `pb_interceptor_final`, `final/tput_pb_today`): 998.1 -> 1,017.8
req/s, **+2.0%** (rounds +2.1%, +1.9%; all four new runs above all four old ones); warm-up CPU per
request -1.4% (rounds -1.4%, -1.4%).

**The slow first request** (previous entry), looked at after the measurements: a short run of
yesterday's PB binary and of today's, one after the other on the now idle machine
(`diag_first/`): first request 6.0 ms and 6.1 ms, as yesterday. So it is intermittent and comes
from the machine's state during those validation runs (which ran right after builds, or while
tests ran or files were deleted), not from either build; it hit wrapper and interceptor builds
alike. Not chased further. Note
that the wrapper builds got today's backend changes too (raw `_br`, raw reverse context), so this
is not a comparison against yesterday's wrappers.

### 2026-10-15: Committed; one prepare and one finish step for both transports

Committed as `b943cec7` and pushed to `origin/retctx-grpc-interceptors` (asked by the user).

Cleanup asked for afterwards: the server side had `prepare` (gRPC: read the carrier from the
metadata) calling `prepareCarrier` (the work, also called by the HTTP middleware), and `finish`
(gRPC: put the reverse context in the trailer) calling `finishCall` (the work, also called by the
HTTP middleware): leftovers of splitting the steps for HTTP while keeping the tuned gRPC per-call
function untouched. Now one `prepare(ctx, carrier)` and one `finish(call, err)` that returns the
reverse context, and each transport reads and sends its values itself (commits `1e77b28a`,
`b08a2940`). The gRPC per-call frames are unchanged (server 256 bytes, client 288); all tests
pass with `-race`.

Did it buy anything? Not measurable in the app (a few calls fewer per server call; a profile samples
every 10 ms). Measured instead with the server interceptor called directly (a temporary benchmark,
no transport, PB, 8 alternating pairs per order, pinned to one CPU): before (the package as of
`b943cec7`) vs after: -151 ns per call with "before" first, -89 ns with "after" first, at ~4.4
microseconds per call; same allocations (13 per call, 1,344 bytes). Control: the same code built
under two package paths differs by -32 ns (pairs -99 to +12), so code layout alone moves results by
that much. So 0-100 ns per server call, under 0.05% of a request's CPU: a cleanup, not an
optimization.

(A first attempt checked out the old commit as a git worktree for this and filled the disk: the
repository tracks large generated build directories. Git removed the partial checkout itself; the
old package was then copied alone instead.)

### 2026-10-15: Throughput of the cleanup; disturbed runs flagged by `tput_ab.sh`

Asked for throughput tests after the prepare/finish cleanup. New PB build of the current code
(`pb_interceptor_final2`, commit `8e4d29c1`); trace validation against `pb_wrapper_final`
(`runs_r12/`): 2000/2000 identical, TVD 0.000, 46000 spans per run. Peak throughput against the
previous final interceptor build (`b943cec7`), both orders (`r12_cleanup.sh`,
`final/tput_pb_cleanup`, `final/tput_pb_cleanup_rev`):

| order | old | new | difference | per round |
|---|---|---|---|---|
| old first | 1,024.1 req/s | 1,027.5 (3 runs) | +0.3% | -0.4%, +1.0% |
| new first | 1,022.0 | 1,024.3 | +0.2% | +0.3%, +0.2% |

No measurable change, as the microbenchmark predicted (under 0.05%). Warm-up CPU per request: -0.4%
and +0.1%.

One run (old-first set, run 6, new build) served 888.9 req/s, 13% below all others, with a normal
warm-up. In its overload step the services got 177.7 s of CPU against ~195.5 s in the runs around
it: at saturation they use all the CPU they get, so something outside the app took ~9% of CPUs 0-5
for that step. The earlier throughput sets never had this (service CPU 193-201 s in every run;
yesterday's "run 5" dips had normal CPU). `tput_ab.sh` now prints each run's service CPU in the
overload step and flags:

* `STARVED`: service CPU over 3% below the set's median; left out of the throughput means and
  differences (the new-build mean above is of its 3 other runs);
* `stalled warm-up`: the warm-up step served over 100 requests fewer than the set's best (the
  ~0.5 s stalls seen since 2026-10-14); left out of the warm-up CPU figures only.

The summary lists what it left out. Recomputed for the earlier sets, nothing changes except CGPB's
warm-up CPU (its run 6 had a stalled warm-up): +0.2% becomes -0.0%.

### 2026-10-16: Where tracing costs CPU, and fixes in our own code (branch `perf-own-code`)

**Profiles** (`~/grpc-interceptors-work/r14_ineff.sh`, `prof/ineff/`): no tracing (a new
`docker_nt_es_nw` build), vanilla, PB and SB, 700 req/s, 30 s CPU and allocation profiles, two runs
each in mirrored order. CPU per request: none 3.612 ms, vanilla 4.280 (+18.5%), PB 4.843, SB 4.994
(the no-tracing binaries have no profiling endpoint; that comparison is CPU only).

* Vanilla's 0.67 ms per request: the OpenTelemetry SDK starting and ending spans (~4% of CPU), the
  interceptors' own code (3.2%), the processor's conversion of each ended span to its protobuf form
  (1.3%) and the export (1.7%, mostly protobuf's reflective marshalling), plus indirect costs (GC
  of tracing's allocations, header compression). Every span is copied twice at its end: the SDK's
  snapshot, then our `buildSpanProto`.
* What the bridges add (per 30 s profile, PB / SB): processor `OnStart` +1.4 / +5.6 s, reverse path
  +2.0 / +1.8 s, interceptors' own code +1.9 / +2.3 s, `OnEnd` +1.2 / +1.5 s, header compression
  +1.4 s, allocation and GC +3-4 s.
* SB's `OnStart`: its stack frame was 1,472 bytes (PB's 64): 2.8 of the 3.0 s spent in
  `GetBaggageFromContext` was goroutine stack growth triggered under it. Each span decoded its
  parent's whole tail (Lehmer codes included) and encoded it again, twice. Every Lehmer group
  allocated (`mixedRadixChunks` 45 objects per request, `lehmerDigits` 29).
* Reverse path (`PrepareCheckpoint`, ~2 s): routing 30%, reading the span's attributes back (a
  locked copy through a map) 10%, writing `_rc` 8%, `RT_SAMPLE` read from the environment on every
  checkpoint 4%; every decode built a hex string of all span IDs that routing never used.
* The processors' diagnostic counters: ~0.05% of CPU (measured per instruction, both at 2 and 4
  cores per service); no measurable lock contention. `GOMAXPROCS=4` instead of 2 costs 15-17% more
  CPU per request (runtime: scheduler, futex, GC on 56 logical processors for 6 cores).

**Changes** (only our own code; the OpenTelemetry SDK stays; branch `perf-own-code`, worktree
`/users/tomislav/blueprint-perf`):

* `c85c2ded`: `RT_SAMPLE` read once per process; reverse-context fingerprints derived only where
  read (`trussData.fingerprint`).
* `c869767c`: SB `OnStart` in two steps (context lookups in a small frame: lookups now at 584 bytes
  of depth instead of 1,472); the inherited tail is validated and copied as bytes
  (`scanStructuralBR`, `appendChildTail`), built once for both payloads; no TTL-wrap copy, no copy of
  the parent's `_br`, no Bloom allocation for a checkpoint's new window; Lehmer groups without
  allocations for bounds up to 64. Also a fix: `unpackEndGroup` could be made to panic by a corrupt
  `_br` (a huge count reached `make`). Tests (`structural_raw_test.go`): byte-identical to the old
  code in every case (Lehmer 20,000 groups; scan vs decode 20,000 payloads, half corrupted; child
  tails 20,000; the whole new `OnStart` against the old algorithm in fixed, 2-6 and 3-3 CPD modes,
  9,000 spans).
* `0c2a8a67`: `backend.PrepareServerCheckpoint`: the server side (interceptors and wrapper server
  templates) passes the child count instead of the SDK reading it back off the span; for bridge
  spans with wire state, `_rc` and a leaf's rejection are kept in the per-span state and exported
  from there. SB still reads attributes in `PrepareCheckpoint`: `force_lp` may be set at any time
  (`TestReverseTTLRespectsSyntheticForceLP`).

All otelcol, backend and interceptor tests pass with `-race`; gRPC per-call frames unchanged.
`TestGeneratedReverseCheckpointWrappers` (plugins) compiles the changed wrapper templates and fails
exactly as before (same five failure messages, same counts) at this branch, at `8d343578`,
`b943cec7`, `7abfa16e` and the original base `d0f40602`. (The 2026-10-13 note on that test quoted
only the first of its five failure messages.)

Not done: writing the OTLP export bytes directly. The batches go out through OpenTelemetry's
`otlptracegrpc` exporter client, which takes protobuf objects; bypassing it is a decision for the
user.

**Validation** (`r15_perf_own.sh`, `ab_trio.sh`, fixed config, 2000 requests per run, `runs_r15/`):
each new build (`{pb,sb,cgpb}_interceptor_perf`) against the current one, since none of the
changes may alter an exported trace. PB: 2000/2000 identical, TVD 0.000. SB: 2000/2000
structurally identical, beyond the noise only `_o` (the start-ordinal race). CGPB: 2000/2000
identical; `_br`/`_rc` lengths of ComposePost's concurrent children differ by TVD up to 0.32
(same build twice: 0.088): still only the two race outcomes (30 / 48 bytes), and the split
between them drifts between sessions on its own: on 2026-10-14 (`runs_r8`) the current build
itself had Text at 30 bytes in 71-83% of traces, the new build has 80% here.

**Peak throughput**, current build ("old") vs `perf-own-code` ("new"), both orders (`perf/`):

| | old first | new first | warm-up CPU per request |
|---|---|---|---|
| SB | **+2.3%** (rounds +2.6, +1.6; one starved new run left out; with it +0.9%) | **+2.9%** (rounds +2.9, +2.8; every new run above every old run) | -0.8% / **-2.0%** |
| PB | **+1.1%** (rounds +0.9, +1.3) | **+0.6%** (rounds +0.6, +0.6; every new run above every old run) | -1.25% / -0.9% |

So SB gains about 2.6% peak throughput and 1-2% CPU per request, PB about 0.8% and 1%, in line
with the profile estimates (CGPB shares PB's reverse-path changes and was not measured).

### Open items

* (Done: the measurement lock, see the testing section.)
* The measured differences from the plan ("Known differences"): bytes on the wire, span durations,
  the SB start-ordinal race shift. CPU and latency: measured (rounds 1-3 above).
* What is left of the bridges' extra cost with interceptors (2-3% CPU) is the reverse context in
  the response trailer: HTTP/2 header compression of a value that is new on every call, and gRPC's
  trailer copies. gRPC offers no control over either; the alternative (payload) was ruled out.
* Stack growth on the application's per-call goroutines (about 4.5% of CPU in SN) remains, in both
  builds. Client call workers did not pay off (see "Stack growth"); it would take the application
  not starting a goroutine per call.
* Smaller: PB's export path starts a goroutine per batch (`PathBridgeProcessor.exportBatch`, 0.3 s
  of stack growth per 30 s of all services); SB's `OnStart` frame is 1472 bytes. Both are processor
  code shared by both builds; not touched.
* The stream workers are on by default for every Blueprint gRPC server built from this branch
  (wrapper builds too); `GRPC_STREAM_WORKERS=0` restores gRPC's goroutine per call.
* The interceptors are only tested end to end on the no-work SN app. Real-work SN and
  HotelReservation only compile-checked.
* Bridge kind still comes from `OT_BRIDGE` at build time; could become a wiring argument.
* Nothing is committed yet.

### 2026-10-19: The OTLP export bytes written directly (item 8, option B; branch `perf-own-code`)

The user's condition for option B: fine if it stays inside a processor, not if it replaces an
OpenTelemetry component elsewhere. It does stay inside: the exporter client (`otlptracegrpc`) was
only ever called by our processors (`client.UploadTraces(ctx, []*tracepb.ResourceSpans)`), the
collector speaks standard OTLP/gRPC, and the SDK's `SpanProcessor` interface is unchanged. So the
live processors (vanilla, PB, CGPB, SB) now do both steps themselves (`runtime/plugins/otelcol/
otlp_wire.go`, commit `c68a01fd`):

* `encodeSpan` writes a span's OTLP `Span` message straight from the SDK's snapshot at `OnEnd`
  (one allocation per span) instead of building a `*tracepb.Span` object tree (`buildSpanProto`,
  the `convert*` functions: about 64 allocations per request, then protobuf's reflective
  marshalling at flush). The buffers hold bytes (`wireSpan`); `buildBatch` concatenates a chunk
  into one `ExportTraceServiceRequest` (lengths computed, no marshalling at flush); `otlpConn`
  sends it over a plain `grpc.ClientConn` with a pass-through codec, with the same
  `bridges-priority` metadata and the same gRPC status errors, so the census (`recordRefusedIDs`)
  and the retry gate (`scheduleBytes`) see what they saw. The per-processor `convert*` code is
  gone (about 250 lines per processor); the earlier processors (eb, rc, priority, realtime) keep
  the exporter client.
* The bytes are the ones `proto.Marshal` produced for the old object trees, byte for byte:
  `otlp_wire_test.go` keeps the old builders as the reference and checks 20,000 random spans over
  all four strip policies (every special key, every value type, events, links, statuses, parents,
  wire entries), the request envelope over resources, scopes and chunk sizes (0 to 600), and a
  round trip against a real gRPC server for every processor (span names, the priority header, a
  refused batch counted and recorded). Two things the old trees did are kept on purpose because
  the bytes must match: an empty `Status` message is always present, and the status code is the
  SDK's `codes.Code` copied without mapping, which swaps Ok and Error on the wire (found here;
  `docs/tracing/BUG_REPORT.md` F-15 on `code-review`; the fix is one line in `encodeSpan` once
  the old bytes no longer have to be matched). One deliberate difference: a string attribute that
  is not valid UTF-8 is written as it is, where the old path failed to marshal and dropped the
  batch (no producer makes such a value). `OTLP_RETRY` has no counterpart on this path (every
  campaign runs with it off, and with the 1 s export deadline the exporter's 5 s first retry never
  ran anyway); a non-off value is warned about at startup.

Per-request micro-benchmark (`BenchmarkRequest*`, the instrumentation + processor + export
encode of one request, `-count 3`, old = `code-review` tree, new = this commit):

| | old | new |
|---|---|---|
| vanilla | 23.3 us, 69 allocs | 20.6 us, 54 allocs |
| PB | 43.4 us, 120 allocs | 35.3 us, 97 allocs |
| SB with reverse | 60.2 us, 151 allocs | 47.6 us, 109 allocs |

(The old benchmark's export step marshalled the object trees; the new one concatenates the
encoded spans, as the flush now does.) All otelcol, backend and interceptor tests pass with
`-race`. Builds, trace validation and throughput: `~/grpc-interceptors-work/r16_direct_otlp.sh`,
results below.

**Validation** (`r16_direct_otlp.sh`, `ab_trio.sh`, fixed config, 2000 requests per run,
`runs_r16/`), each direct-export build against the build it replaces (PB, SB, CGPB: the items 1-7
builds `*_interceptor_perf`; vanilla: `v_interceptor_final`, since items 1-7 did not touch its code
path):

| kind | trace by trace | largest attribute TVD vs old (same build twice) |
|---|---|---|
| PB | 2000/2000 identical, 46,000 spans | 0.000 (0.000) |
| CGPB | 2000/2000 identical | 0.012 (0.048) |
| SB | 2000/2000 structurally identical | 0.124 (0.036): only the `_br` sizes of ComposePost's concurrent children, the start-ordinal race |
| vanilla, first run | 1999/2000 | 0.000 (0.000) |
| vanilla, rerun on the idle machine (`vE`) | 2000/2000 identical, 46,000 spans | 0.000 (0.000) |

The one vanilla mismatch of the first run was trace 0, the first request, with 49 spans instead of
23: it hit the 1 s gRPC deadline, the retry wrappers repeated it, and composepost's log has the two
"context canceled" calls at that moment. That is the slow-first-request stall of 2026-10-14, which
hits whichever build runs right after a build (this run started seconds after the four builds), not
the export path; two runs of the old build in the same set were identical, and the rerun was exact.

**Peak throughput** (`tput_ab.sh`: fresh start, 45 s warm-up at 700 req/s, then 45 s at 1,600
req/s offered; two A-B-B-A rounds per order; `direct/`), new (direct export) minus old:

| | old first | new first | warm-up CPU per request, old first / new first |
|---|---|---|---|
| SB | **+1.16%** (rounds +1.17, +1.15) | **+1.12%** (rounds +0.87, +1.35) | -1.10% / -1.61% |
| PB | **+1.13%** (rounds +0.86, +1.40) | **+1.08%** (rounds +1.32, +0.81) | -1.61% / -0.79% |
| vanilla | +0.14% (noise 0.75%) | +0.00% (noise 0.84%) | +0.11% / -0.73% (noise 1.1-3.1%) |

For SB and PB every new run is above every old run in both orders (SB, old first: new
1,017.7-1,023.4 vs old 1,002.5-1,010.6 req/s; PB: 1,045.4-1,050.0 vs 1,024.0-1,040.1). No starved
runs; three stalled warm-ups (SB new-first run 3, PB old-first run 7, vanilla old-first run 4) are
left out of the CPU figures only. Absolute rates: SB old 1,008.5 today against 984.4 on 2026-10-16
(machine drift between sessions; compare within a set only).

So the bridges gain about 1.1% peak throughput and 1-1.6% CPU per request from writing the export
bytes directly; vanilla gains nothing this method can resolve (the profiles put its export at
about 3% of CPU, and the object trees it no longer builds are smaller than the bridges'). Chained
with items 1-7, the `perf-own-code` branch is about +3.7% (SB) and +1.9% (PB) peak throughput over
the 2026-10-15 interceptor builds, with the usual caveat that the two sessions' sets were measured
on different days.

`retctx-grpc-interceptors` is fast-forwarded to this branch (items 1-8). Item 10 (our other large
stack frames) remains deferred.
