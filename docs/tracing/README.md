# Tracing in this fork: project structure

A map of every part of this repository that has to do with tracing and OpenTelemetry: what each
directory and file does, where it came from, what is used and what is not, and an order to review it
in. Written 2026-10-15 at commit `8d343578` (branch `retctx-grpc-interceptors`); line counts are
from that commit.

**Origin** in the tables is relative to the fork point, upstream Blueprint commit `1e5478a0`
(Vaastav Anand, 2025-03-24):

* *upstream*: the file is upstream Blueprint's and unchanged (some upstream files had only their
  file mode changed, to executable, in this fork; that is not counted as a change);
* *upstream, changed +a/-d*: an upstream file this fork changed, by that many added/removed lines;
* *new*: added in this fork.

Authors in this fork, as git records them: `tome215`, `tomislav` and `Tomislav Zabcic-Matic` (all
Tomislav); `dhuye` (a shared CloudLab account; its 7 commits, October 2025, are marked "Darby:" or
"Tomislav:"); `Emilio L. Aleman` (the reverse truss, August 2026).

Contents:

1. [How a request is traced](#1-how-a-request-is-traced)
2. [Names used everywhere](#2-names-used-everywhere)
3. [Run time: the tracing SDK and the bridges](#3-run-time-the-tracing-sdk-and-the-bridges)
4. [Run time: instrumentation (interceptors, middleware)](#4-run-time-instrumentation-interceptors-middleware)
5. [Build time: plugins and code generation](#5-build-time-plugins-and-code-generation)
6. [Applications: wiring specs](#6-applications-wiring-specs)
7. [Tools: tests, measurements, experiments](#7-tools-tests-measurements-experiments)
8. [Outside this repository](#8-outside-this-repository)
9. [Environment variables](#9-environment-variables)
10. [Unused code and other things a reviewer should know](#10-unused-code-and-other-things-a-reviewer-should-know)
11. [A review order](#11-a-review-order)

---

## 1. How a request is traced

Blueprint works in two stages, and tracing has a part in each.

**Build time.** A *wiring spec* (Go code under `examples/<app>/wiring/specs/`) says which services
exist and how they are deployed. For tracing it calls the OpenTelemetry plugin
(`plugins/opentelemetry`) for each service, and creates a collector (`plugins/otelcol`). When
Blueprint compiles the spec, the plugins generate Go code (servers, clients, and in wrapper mode
tracing wrapper services) into a build directory; the hand-written runtime code under `runtime/`
is copied in next to it.

```
wiring spec ──► plugins/opentelemetry   Instrument (wrappers) | InstrumentGRPC | InstrumentHTTP
            ──► plugins/grpc, plugins/http   generated servers/clients, with the interceptor /
                                             middleware hooks
            ──► plugins/otelcol         the collector container and each process's collector client
                 │
                 ▼  Blueprint compiles the spec
            build directory: generated code + a copy of runtime/
```

**Run time.** Each service process creates one *collector client*
(`runtime/plugins/otelcol/trace.go`, `NewOTCollectorTracer`): an OpenTelemetry SDK tracer provider
with one *span processor*, picked by the `BRIDGE_KIND` environment variable (vanilla, or one of the
bridges). Then, for one call from service A to service B (interceptor mode, the default):

```
A, client side (runtime/plugins/opentelemetry/interceptors/client.go, http_client.go)
  1. take the next child ordinal from A's per-request state ("childCount" -> "seqNum", "__seq")
  2. start the client span; the span processor's OnStart computes the bridge state (`_br`)
  3. encode the carrier: span context + baggage (incl. `_br`, `__seq`)  -> header bp-trace-ctx
B, server side (server.go, http_server.go)
  4. decode the carrier; remote parent, baggage, "seqNum" on the context
  5. start the server span (processor OnStart reads the parent's `_br`)
  6. run the service; its own calls repeat 1-11 one level down
  7. set the child attributes (hasChildren / childCount / SB's remEndEvents)
  8. reverse context: merge the children's, let the SDK decide on checkpoints
     (PrepareCheckpoint)                                  -> trailer / header bp-ret-ctx
  9. end the server span; processor OnEnd classifies it (checkpoint or not) and queues it
A, client side again
 10. PrepareCheckpoint on the client span with B's reverse context; add the rest to A's merge
 11. end the client span
Every process: the processor exports its queued spans to the collector agent over OTLP/gRPC, high
priority (checkpoints) and low priority in separate batches, marked with the "bridges-priority"
gRPC metadata; the collector's priority processor (other repository, section 8) can shed the low
priority ones under load.
```

In **wrapper mode** (`opentelemetry.Instrument`, or any build with `OT_GRPC_MODE=wrapper`) the same
steps run in generated wrapper services instead (templates in `plugins/opentelemetry/ir_ot_*.go`):
every method gets an extra `traceCtx string` argument (the carrier, as JSON) and an extra
`retCtx string` return value (the reverse context, as text). The interceptor mode was built to
produce identical traces; `utils/grpc_interceptors/README.md` is the log of how, with every
validation and measurement.

## 2. Names used everywhere

| name | meaning |
|---|---|
| carrier | what travels with a request so the callee can continue the trace: span context (trace ID, span ID, flags, trace state) plus baggage. JSON in wrapper mode (`runtime/core/backend/carrier_json.go`), a compact binary record in interceptor mode (`interceptors/carrier.go`) |
| baggage | key/value pairs that travel with the carrier; the bridges use `_br`, `__seq`, `__rt_depth` |
| `_br` | a bridge's propagated state (raw packed bytes in the process; `runtime/plugins/otelcol/pack.go`) |
| `__seq`, `seqNum` | the caller's child ordinal (1, 2, ...), in the baggage and on the context |
| `__bag.<key>` | span attributes that a processor sets for the instrumentation to turn into baggage; never exported |
| checkpoint, CPD | a span the bridge exports with high priority, carrying enough to reconstruct its window of the call path; checkpoint distance (CPD) = spacing of checkpoints, from the collector's config (`cpd_min`, `cpd_max`) |
| HP / LP | high / low priority export classes |
| truss | the data a checkpoint carries about the spans around it |
| reverse context, retCtx | data travelling back up the call path with each response: checkpoints a child could not keep (`runtime/core/backend/reversetruss*.go`); raw bytes in the process, text only where a carrier needs text |
| `_rc` | exported span attribute: a reverse checkpoint a span took over |
| PB, CGPB, SB, EB | path bridge, call-graph + path bridge, structural bridge, exact bridge (span processors) |
| vanilla, `v` | plain OpenTelemetry tracing with the same export path, no bridge |

## 3. Run time: the tracing SDK and the bridges

### 3.1 `runtime/core/backend` (tracing parts)

The interfaces Blueprint's generated code and runtime plugins share. Only the tracing files are
listed; the rest of the directory (cache, databases, queues, metrics, logging) is unrelated.

| file | lines | origin | what it does |
|---|---|---|---|
| `trace.go` | 129 | upstream, changed +85/-14 | `Tracer` interface; the JSON carrier's decoding (`GetSpanContext`) and `AddBaggageToTraceContext`; baggage on the context (`SetBaggageInContext`, `GetBaggageFromContext`, `IsBaggageKey`) |
| `carrier_json.go` | 327 | new | reflection-free encoder and fast parser of the wrappers' JSON carrier, byte-identical to `encoding/json`; non-UTF-8 baggage values (raw `_br`) travel base64 in `"baggage_bin"` |
| `span_baggage.go` | 43 | new | `SpanBaggageSource`: baggage the SDK keeps beside a span, added to each outgoing carrier |
| `checkpoint.go` | 60 | new | `CheckpointPreparer` / `PrepareCheckpoint`: the hook through which instrumentation asks the SDK to decide on checkpoints before a span ends; reverse-path attribute keys |
| `reversetruss.go` | 493 | new (Emilio L. Aleman) | the reverse context: segments, encode/decode, routing (`RouteRetCtx*`: which checkpoints a span takes over, TTL steps), merging (`MergeRetCtx`, the per-request `RetMerge` accumulator: `WithRetMerge`, `AddToMerge`, `MergedChildren`), toggles (`REVERSE_TRUSS`, `RT_*`), counters and sampled logging |
| `reversetruss_packed.go` | 231 | new | the packed binary envelope of the reverse context (version byte + records), its merge by concatenation, and `RetCtxText` / `RetCtxFromText` for carriers that need text |
| tests: `carrier_json_test.go` 197, `checkpoint_test.go` 222, `retctx_cost_test.go` 80, `retctx_text_test.go` 66, `reversetruss_test.go` 63 | | new | |

### 3.2 `runtime/plugins/otelcol`: the span processors (the bridges)

The core of the research code: 10,230 lines plus 3,713 lines of tests, all new in this fork.
Every processor implements the OpenTelemetry SDK's `SpanProcessor` (`OnStart`, `OnEnd`) and does
its own export to the collector.

**Entry point and shared machinery**

| file | lines | what it does |
|---|---|---|
| `trace.go` | 172 | `NewOTCollectorTracer`: picks the processor by `BRIDGE_KIND`, wraps it in the reverse-checkpoint processor, sets the head sampler (`OTEL_SAMPLE_RATIO`) |
| `defs.go` | 75 | baggage keys, ancestry modes, Bloom filter defaults, the collector's config response type |
| `pack.go` | 402 | the packed `_br` formats of the bridges; `encodeBR`/`decodeBR` (text, for attributes) and `propBR`/`parsePropBR` (raw, for baggage) |
| `checkpoint_distance.go` | 101 | `checkpointRange`: the CPD range discovered from the collector; the forward TTL byte |
| `checkpoint_window.go` | 164 | the ranged PB/CGPB window: per-distance Bloom geometry, packing and unpacking of the window |
| `wire_state.go` | 139 | `bridgeWires`: per-span bridge state kept beside the span instead of in its attributes (emit payload, depth, outgoing `_br`) |
| `vanilla_processor.go` | 718 | the vanilla processor; also defines export settings other processors use (deadline `OTLP_DEADLINE_MS`, `OTLP_RETRY`, span padding `SPAN_PADDING_BYTES`) |

**The bridges used in the experiments**

| file | lines | what it does |
|---|---|---|
| `pb_processor.go` | 1,087 | path bridge (PB): HP/LP buffers and export, OnStart builds the path window, OnEnd classifies and converts; legacy fixed-CPD mode (text `_br` in `__bag._br` attributes) when no CPD range is configured |
| `cgpb_processor.go` | 1,090 | call-graph + path bridge (CGPB): PB plus a window-local hash array of fan-out witnesses |
| `sb_processor.go` | 1,305 | structural bridge (SB): CGPB's core plus vertical ordinals and orthogonal end-event trusses; reads `remEndEvents` at OnEnd |
| `structural_truss.go` | 378 | SB's truss format (wire layout in its header comment) |
| `lehmer_on.go`, `lehmer_off.go` | 9, 6 | build tag: Lehmer-coded end-event groups (default) or the plain-list ablation (`-tags sb_nolehmer`) |

**The reverse path**

| file | lines | what it does |
|---|---|---|
| `reverse_checkpoint.go` | 338 | `reverseCheckpointProcessor`, wrapped around every processor when `REVERSE_TRUSS=on`: per-span depth and scheduling at OnStart; `PrepareCheckpoint` (takes over / forwards returned trusses, leaf rejection `RT_LEAF_REJECT`) |
| `reverse_policy.go` | 138 | reverse routing policies (TTL, probabilistic) from the collector's config |
| `reverse_state.go` | 77 | the sharded per-span state table the reverse path uses |

**Operations**

| file | lines | what it does |
|---|---|---|
| `refused_ids.go` | 247 | census of trace IDs whose spans the collector refused, served over HTTP (`RETCTX_REFUSED_*`) for the campaign runner |
| `sdk_retry.go` | 171 | one-shot retry of refused export batches (`BRIDGES_RETRY`) |
| `pprof.go` | 54 | opt-in pprof endpoint (`BRIDGES_PPROF`) |

**Processors not used by the current experiment scripts** (still selectable with `BRIDGE_KIND`;
the scripts only use `v`, `pb`, `cgpb`, `sb`)

| file | lines | origin | what it is |
|---|---|---|---|
| `priority_processor.go` | 1,171 | new (dhuye) | the first high/low-priority processor (October 2025); ancestry by Bloom filter or hash array (`ANCESTRY_MODE`) |
| `eb_processor.go` | 904 | new | exact bridge (`eb`) |
| `random_processor.go` | 793 | new | control: vanilla plus a random 1/CPD of spans marked high priority (`rc`) |
| `realtime_processor.go` | 691 | new | exports partial spans as they start and end (`realtime`) |

**Tests** (3,713 lines): `backpressure_test.go`, `checkpoint_distance_test.go`,
`checkpoint_window_test.go`, `checkpoint_window_encode_test.go`, `export_cost_test.go`,
`microbench_test.go`, `received_checkpoint_test.go`, `refused_ids_test.go`, `request_cost_test.go`,
`reverse_checkpoint_test.go`, `reverse_passthrough_test.go`, `reverse_policy_test.go`,
`reverse_sdk_cost_test.go`, `reverse_state_bench_test.go`, `reverse_ttl_test.go`,
`sdk_retry_test.go`, `structural_truss_test.go`. (`README.md` here is generated by gomarkdoc and out
of date: it shows an older two-argument `NewOTCollectorTracer`.)

### 3.3 `runtime/plugins/bloom`, `runtime/plugins/critpath`

| file | lines | origin | what it does |
|---|---|---|---|
| `bloom/bloom.go` | 359 | new | the Bloom filter the bridges use (+ `bloom_prehashed_test.go`, 102) |
| `critpath/critical_path.go` | 95 | new | critical path of a set of child spans; used only by the unused wrapper templates (section 10) |

### 3.4 Other tracers (upstream, unchanged unless noted)

`runtime/plugins/jaeger/trace.go` (35; upstream, changed +3/-3), `runtime/plugins/zipkin`,
`runtime/plugins/xtrace`, `runtime/plugins/govector`: collector clients of other tracing systems,
from upstream Blueprint. `runtime/plugins/opentelemetry/trace.go`, `log.go`, `metric.go` (upstream):
a stdout tracer, a logger that turns log lines into span events, a stdout metric collector.
`runtime/plugins/tracecoordinator/coordinator.go` (65, new, 2025): only used by the unused
`plugins/tracecoordinator`.

## 4. Run time: instrumentation (interceptors, middleware)

### 4.1 `runtime/plugins/opentelemetry/interceptors` (all new, 2026-10)

gRPC interceptors and HTTP middleware that do what the generated wrapper services do (section 1,
steps 1-11), with the same span names, attributes and per-request state. 1,208 lines plus 1,100 of
tests.

| file | lines | what it does |
|---|---|---|
| `interceptors.go` | 238 | constructors of the gRPC interceptors; the per-bridge-kind `variant` table; span and tracer names per method (`methodCache`, `newMethodInfo`); `mergeSpanBaggage` |
| `carrier.go` | 155 | the compact binary carrier (`encodeCarrier`, `decodeCarrier`, the unsampled form) |
| `header.go` | 57 | how the carrier and the reverse context become header text: base64 with an alphabet HPACK does not Huffman-code |
| `server.go` | 280 | the server side: the gRPC server interceptor; `prepare` (steps 1-3: carrier to remote parent, baggage, ordinal), `setup`, `finish` (steps 8-9); `requestState`, one context value answering every per-request key |
| `client.go` | 233 | the client side: the gRPC client interceptor; `prepare` (ordinal), `carrierFor`, the trailer catcher, `finish` (SB end events) |
| `http_server.go` | 125 | the HTTP server middleware: carrier from the `Bp-Trace-Ctx` request header; steps 8-9 when the handler starts its response; reverse context in the `Bp-Ret-Ctx` response header |
| `http_client.go` | 120 | the HTTP client middleware (a `RoundTripper`): carrier in a copy of the request; reverse context from the response header; the span ends when the body is closed |
| tests | | `interceptors_test.go` (579: every behaviour over both gRPC and HTTP), `http_test.go` (150), `carrier_test.go` (144: 5000 random cases against the JSON carrier), `header_test.go` (89), `bench_test.go` (138: per-call microbenchmarks) |

### 4.2 `runtime/plugins/httpmiddleware` (new)

`httpmiddleware.go` (43): the `ServerMiddleware` / `ClientMiddleware` types the generated HTTP code
takes, and `RecordError` (the generated handler hands the service's error to a middleware that
wants it).

## 5. Build time: plugins and code generation

### 5.1 `plugins/opentelemetry`

| file | lines | origin | what it does |
|---|---|---|---|
| `wiring.go` | 227 | upstream, changed +89/-0 | `Instrument` (wrappers), `InstrumentGRPC` and `InstrumentHTTP` (new; fall back to `Instrument` with `OT_GRPC_MODE=wrapper`), `Logger` |
| `ir_ot_interceptors.go` | 119 | new | IR nodes for the interceptors and the HTTP middleware: they generate no code, only the call that creates them |
| `ir_ot_server.go` | 1,093 | upstream, changed +809/-4 | the server wrapper node and its templates: one per bridge kind (`serverTemplateVanilla`, `Path`, `CGPB`, `SBridge`), chosen by `OT_BRIDGE`; plus the unused original `serverTemplate` (section 10) |
| `ir_ot_client.go` | 885 | upstream, changed +683/-4 | the client wrapper node and its templates, same structure (`clientSideTemplate*`) |
| `ir_collector_client.go` 13, `ir_ot_logger.go` 69 | | upstream | interface of collector clients; the span-event logger node |
| `reverse_checkpoint_test.go` | 123 | new | compiles the real wrapper templates and runs them against the SDK (reverse checkpoints across calls) |

Note that the wrapper templates are Go source inside Go string literals: the code a reviewer reads
there is what gets generated per service.

### 5.2 Hooks in the gRPC, HTTP and Go plugins (all small changes)

| file | lines | origin | change |
|---|---|---|---|
| `plugins/grpc/interceptors.go` | 62 | new | `ServerInterceptor` / `ClientInterceptor` node interfaces |
| `plugins/grpc/wiring.go` | 184 | upstream, +64/-2 | `AddServerInterceptor`, `AddClientInterceptor`; `DeployWithTimeout` |
| `plugins/grpc/ir_grpc_server.go`, `ir_grpc_client.go` | 135, 147 | upstream, +17/-3, +26/-3 | pass the interceptor nodes to the generated constructors |
| `plugins/grpc/grpccodegen/servergen.go`, `clientgen.go` | 134, 115 | upstream, +32/-3, +15/-3 | generated server/client install the interceptors; stream workers (`GRPC_STREAM_WORKERS`) |
| `plugins/http/middleware.go` | 105 | new | `ServerMiddleware` / `ClientMiddleware` node interfaces, `AddServerMiddleware`, `AddClientMiddleware` |
| `plugins/http/wiring.go`, `ir_http_server.go`, `ir_http_client.go` | 89, 122, 117 | upstream, +11/-2, +16/-3, +16/-4 | pass the middleware nodes to the generated constructors |
| `plugins/http/httpcodegen/servergen.go`, `clientgen.go` | 139, 137 | upstream, +19/-4, +15/-3 | generated server wraps each method's handler and passes the request's context (without cancellation) to the service; generated client wraps its transport and sends the caller's context |
| `plugins/golang/ir.go`, `gogen/namespacebuilder.go` | 368, 296 | upstream, +11/-0, +8/-2 | `TypedInstance`: a non-service constructor argument (an interceptor) gets its real Go type in the generated code |

### 5.3 Collectors and other tracers

| path | lines | origin | what it is |
|---|---|---|---|
| `plugins/otelcol/` (`wiring.go` 198, `ir_collector.go` 461, `ir_collector_client.go` 78) | 737 | new (2025) | the OpenTelemetry collector container in a wiring spec (config file, image, config-discovery port) and each process's collector client node |
| `plugins/jaeger/` (`wiring.go`, `ir_collector.go`, `ir_elasticsearch.go`) | 454 | upstream, changed (+102, +47; `ir_elasticsearch.go` new) | Jaeger, with an Elasticsearch storage option |
| `plugins/zipkin`, `plugins/xtrace`, `plugins/govector` | | upstream | other tracing systems (only the greeter and leaf examples use them) |
| `plugins/otel_with_interm/` | 716 | new (2025) | a copy of the OpenTelemetry plugin (same package name) for an intermediary; not imported anywhere |
| `plugins/tracecoordinator/` | 124 | new (2025) | a coordinator between tracers; not imported anywhere |

## 6. Applications: wiring specs

Only the parts that set up tracing; the services themselves (`examples/<app>/workflow/`) are the
applications' code and are not changed for tracing.

| file | lines | origin | tracing in it |
|---|---|---|---|
| `examples/dsb_sn/wiring/specs/docker_nw.go` | 183 | new | DSB SocialNetwork "no-work" variant (the one the measurements use): `InstrumentGRPC` for gRPC services, `InstrumentHTTP` for `wrk2api` and the trace-pressure service, the collector with its config |
| `examples/dsb_sn/wiring/specs/docker.go` | 275 | upstream, changed +251/-126 | DSB SocialNetwork with backends, same instrumentation |
| `examples/dsb_sn/wiring/specs/docker_nt.go` | 301 | new | the same without tracing (no-tracing baseline) |
| `examples/dsb_hotel/wiring/specs/original.go` | 154 | upstream, changed +84/-47 | HotelReservation, same instrumentation; `collector-bridges.yaml`, `collector-vanilla.yaml` are its collector configs |
| `examples/dsb_hotel/wiring/specs/docker.go`, `docker_nw.go` | 102, 93 | new | other HotelReservation variants |
| `examples/leaf/wiring/specs/docker_fanout.go`, `docker_tree.go` | 254, 89 | new | synthetic topologies (fan-out, trees) |
| `examples/dsb_sn/workflow/{snnw,socialnetwork}/TracePressureService.go` | 65 each | new | synthetic span-pressure service: each request emits N child spans forced to low priority |

## 7. Tools: tests, measurements, experiments

| path | what it is |
|---|---|
| `utils/grpc_interceptors/` | the interceptor project's harness: build (`build_sn.sh`, `build_binaries.sh`), local runs in a private network namespace with a fake collector (`run_local.sh`, `fakecollector/`), trace comparison (`ab_trio.sh`, `compare_traces.py`, `attr_stats.py`), measurements (`tput_ab.sh`, `cpu_ab.sh`, `latency_ab.sh`, `profile_ab.sh`, `cpu_areas.py`, ...), and `README.md`: the plan, the test methods and the dated log of every step |
| `utils/prepare_*.py`, `build_*`, `run_*`, `analyze_*`, `derive_*`, `dsb_apps.py`, `rt_env.sh`, `retctx_wire.py`, `reverse_policy.py`, `checkpoint_distance.py` | the cluster experiment pipeline: build and deploy the apps, set each service's tracing environment, run campaigns, analyse span loss and checkpoints (`test_*.py` are their tests) |
| `utils/plot_*.py`, `ramp_*`, `*spanload*`, `burst_*`, `throughput_analysis.py` | load ramps, span-load experiments, figures |
| `utils/analysis/` | campaign results, figures and their scripts (1,220 files) |
| `utils/setup_environment.sh`, `install_blueprint_deps.sh`, `pin_nodes.py`, `pin-cpu-22ghz.sh`, `registry-*.yaml`, `k8s-configuration.md` | cluster setup |

Running the Go tests of the tracing code (from `runtime/`):

    go test ./core/backend/ ./plugins/otelcol/ ./plugins/opentelemetry/interceptors/

and from `plugins/`: `go test ./opentelemetry/` (its `TestGeneratedReverseCheckpointWrappers`
fails, and failed the same way at the base commit before this work: `utils/grpc_interceptors/
README.md`, Step 2d).

## 8. Outside this repository

* **The collector**: `~/opentelemetry-collector-contrib` (branch `retctx-reject-impl`),
  `processor/priorityprocessor`: admits or refuses export batches by the `bridges-priority`
  metadata, discovers and serves the bridge configuration (CPD range, reverse policy).
* **The bridges simulator**: comments in the processors refer to files such as
  `bridges/bridge/pcrb.go` and `cgprb.go` of a separate bridges repository, which the Go processors
  mirror.

## 9. Environment variables

**Build time** (read by the plugins while Blueprint compiles a spec)

| variable | effect |
|---|---|
| `OT_BRIDGE` | bridge kind the generated code is for: `v`, `pb` (default), `cgpb`, `sb`; picks the wrapper templates and the interceptors' variant |
| `OT_GRPC_MODE` | `wrapper`: `InstrumentGRPC` / `InstrumentHTTP` fall back to wrappers; unset: interceptors and middleware |

**Run time** (read by each service process)

| variable | read in | effect |
|---|---|---|
| `BRIDGE_KIND` | `otelcol/trace.go` | span processor: `v`, `pb` (default), `cgpb`, `sb`, `eb`, `rc`, `priority`, `realtime`; must match `OT_BRIDGE` |
| `OTEL_SAMPLE_RATIO` | `otelcol/trace.go` | head sampling ratio (default: everything) |
| `REVERSE_TRUSS` | `backend/reversetruss.go` | `on`: the reverse path (checkpoint handover on the way back) |
| `RT_ROOT`, `RT_LEAF_REJECT`, `RT_PARENTID`, `RT_SAMPLE`, `RT_DUMP_SEC`, `RT_BLOOM_CAP`, `RT_BLOOM_FPR` | `backend/reversetruss.go`, `otelcol/reverse_checkpoint.go` | reverse path: root behaviour, leaf rejection rate, parent IDs, 1-in-N checkpoint logging, counter dumps, standalone Bloom sizing |
| `OTLP_RETRY`, `OTLP_DEADLINE_MS` | `otelcol/vanilla_processor.go` | export retries (`off` in all measurements) and per-export deadline |
| `BRIDGES_RETRY`, `BRIDGES_RETRY_DELAY_MS` | `otelcol/sdk_retry.go` | one-shot retry of refused batches |
| `RETCTX_REFUSED_CENSUS`, `RETCTX_REFUSED_PORT`, `RETCTX_REFUSED_RECORDS`, `RETCTX_REFUSED_RECORDS_FILE` | `otelcol/refused_ids.go` | refused-trace census |
| `BRIDGES_PPROF` | `otelcol/pprof.go` | pprof endpoint |
| `SPAN_PADDING_BYTES` | `otelcol/vanilla_processor.go` | pads every span (span-pressure experiments) |
| `ANCESTRY_MODE` | `eb`, `priority`, `sb` processors | ancestry encoding |
| `GRPC_STREAM_WORKERS` | generated gRPC servers | reused goroutines for incoming calls (default 32; 0 = off) |
| `POD_IP`, `HOST_IP` | `otelcol/realtime_processor.go` | how the real-time processor finds its own IP |

The bridge configuration itself (CPD range, reverse policy) is not an environment variable: the
processors fetch it from the collector's config endpoint at startup.

## 10. Unused code and other things a reviewer should know

* **Unused wrapper templates**: `serverTemplate` (`ir_ot_server.go`) and `clientSideTemplate`
  (`ir_ot_client.go`), the original templates, are never selected (every `OT_BRIDGE` value picks
  one of the four bridge variants). They are the only users of `runtime/plugins/critpath`.
* **Unused plugins**: `plugins/otel_with_interm` and `plugins/tracecoordinator` (with
  `runtime/plugins/tracecoordinator`) are not imported anywhere.
* **Processors no current script selects**: `eb`, `rc`, `priority`, `realtime` (section 3.2).
* **Legacy fixed-CPD mode** in PB/CGPB (`_br` as text in `__bag._br` attributes): only when the
  collector configures no CPD range; the experiments always configure one.
* **`BuildRetCtx`** (`backend/reversetruss.go`) has no callers; it is the only producer of the
  JSON form of the reverse context and of parent fingerprints.
* **Generated build output is tracked in git**: 148,990 of the repository's 152,077 tracked files
  are under `examples/*/build_*/` (generated code plus copies of `runtime/`). Searching the
  repository finds runtime files many times over; review the originals under `runtime/` and
  `plugins/`.
* **`bridges`** (repository root, 8.9 MB) is a compiled executable tracked in git.
* **gomarkdoc READMEs** (`plugins/*/README.md`, `runtime/plugins/*/README.md`) were generated at
  some point and not regenerated since; some are out of date.
* **Comment convention**: changes in this fork are marked `Tomislav-RetCtx:` in comments.

## 11. A review order

From the data structures outwards; hand-written lines (tests separately):

| # | part | lines | tests |
|---|---|---|---|
| 1 | `runtime/core/backend`: `trace.go`, `span_baggage.go`, `checkpoint.go`, `carrier_json.go`, `reversetruss.go`, `reversetruss_packed.go` | 1,283 | 628 |
| 2 | `runtime/plugins/otelcol` shared machinery: `trace.go`, `defs.go`, `pack.go`, `checkpoint_distance.go`, `checkpoint_window.go`, `wire_state.go` | 1,053 | |
| 3 | the processors in use: `vanilla_processor.go`, `pb_processor.go`, `cgpb_processor.go`, `sb_processor.go`, `structural_truss.go`, `lehmer_*.go` | 4,593 | |
| 4 | the reverse path: `reverse_checkpoint.go`, `reverse_policy.go`, `reverse_state.go` | 553 | |
| 5 | operations: `refused_ids.go`, `sdk_retry.go`, `pprof.go`; `runtime/plugins/bloom` | 831 | 3,713 for all of `otelcol`, 102 for `bloom` |
| 6 | instrumentation: `runtime/plugins/opentelemetry/interceptors`, `runtime/plugins/httpmiddleware` | 1,251 | 1,100 |
| 7 | build time: `plugins/opentelemetry` (`wiring.go`, `ir_ot_interceptors.go`, the wrapper templates in `ir_ot_server.go` / `ir_ot_client.go`) | 2,324 | 123 |
| 8 | the hooks: section 5.2, the two new files and the changed lines (`git diff 1e5478a0 -- plugins/grpc plugins/http plugins/golang`) | ~420 | |
| 9 | collectors: `plugins/otelcol`, `plugins/jaeger/ir_elasticsearch.go` and the jaeger changes | ~960 | |
| 10 | the specs and the trace-pressure services in section 6 | ~1,580 | |
| - | not in use (optional): the four other processors, `otel_with_interm`, `tracecoordinator`, `critpath`, the unused templates (~280 of the lines in #7) | ~4,800 | |

About 14,800 hand-written lines in use, plus about 5,700 lines of tests.
