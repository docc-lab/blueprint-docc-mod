# Tomislav-RetCtx: session change map

This records the edits developed on `retctx-reject-impl`, including the earlier
work and the latest reverse-routing revisions. `Tomislav-RetCtx:` comments identify
the relevant implementation points; generated build directories are artifacts,
not additional service implementations.

| Area | What changed and where |
| --- | --- |
| SDK checkpoint refusal | [reverse_checkpoint.go](../../runtime/plugins/otelcol/reverse_checkpoint.go) moves refusal and receipt decisions into the SDK before span end. Refused spans still export with ordinary metadata; original span IDs, absolute depths, and truss bytes return upstream. |
| Per-truss reverse TTL | Original forward checkpoints are now protected. Only unscheduled server leaves may reject, drawing a fresh reverse TTL from collector CPD settings. Each upstream span emits expired trusses and forwards the rest; original checkpoints absorb all returns. This replaces the provider-wide `RT_POLICY`/`RT_DEPTH` bundle policy. See [routing and encoding](reverse_truss.md). |
| Probabilistic reverse routing (September 13) | [reverse_policy.go](../../runtime/plugins/otelcol/reverse_policy.go) adds fixed p, 1/origin depth, and normalized linear depth bias. Each TTL-free truss gets its own receiver trial; explicit TTLs retain their countdown. Collector discovery and both deployment scripts validate/select policies. Existing origin metadata supplies depth, so the probability carrier adds no fields. See [formulas and configuration](reverse_checkpoint_probability.md). |
| Return transport | [backend helpers](../../runtime/core/backend/reversetruss.go) preserve typed trusses, varint depths, and individual TTLs. [Client](../../plugins/opentelemetry/ir_ot_client.go) and [server](../../plugins/opentelemetry/ir_ot_server.go) wrappers hand returns to recording spans, read the SDK decision, and merge only pending segments. The per-request mutex supports concurrent fan-in; private carriers never become forward baggage or exported attributes. |
| Random forward CPD | [checkpoint_distance.go](../../runtime/plugins/otelcol/checkpoint_distance.go) validates collector `cpd_min`/`cpd_max` and schedules checkpoints using a one-byte countdown. Roots and scheduled checkpoints draw once for outgoing descendants; sibling paths use independent copies. Legacy `cpd` keeps its existing forward format. |
| Bloom sizing | [checkpoint_window.go](../../runtime/plugins/otelcol/checkpoint_window.go) replaces the initial experimental maximum-sized Bloom with a filter sized for each selected window. An immutable distance byte identifies its geometry and CGPB HA boundary after the forward TTL changes. Incoming trusses retain their geometry when a checkpoint starts a new window. |
| Collector configuration | The sibling contrib project's `receiver/configdiscoveryreceiver/config.go` validates paired integer bounds in 1–256. Its existing logs/config pipeline serves them to SDKs at startup. These bounds also supply reverse draws; the collector does not route reverse trusses. |
| Configurable synthetic applications | [FanoutNode/PatternNode and wiring guide](../../examples/leaf/FANOUT.md) support arbitrary child counts, per-request concurrency limits, nested sequence/parallel/repeat/sleep/timeout patterns, and JSON/YAML rooted DAGs. Shared services receive separate calls per incoming edge. [deep8.yaml](../../examples/leaf/wiring/specs/deep8.yaml) provides eight RPC hops, 15 services, and fanouts at three depths. |
| Generator support | Variadic constructor binding in [gocode](../../plugins/golang/gocode/service.go), workflow wiring, and namespace generation supports reusable fanout nodes. IR literals use Go quoting, and the source parser excludes test-only constructors. [DeployWithTimeout](../../plugins/grpc/wiring.go) makes transport deadlines configurable while ordinary `Deploy` retains 1s. |
| Hotel integration | [Hotel wiring and guide](../../examples/dsb_hotel/README.md) route SDK traces through otelcol/config discovery to Jaeger, optionally Elasticsearch, with matching bridge variants and local endpoints. [The one-shot deployer](../../utils/build_deploy_hotel.py) adds image builds/pushes, namespaces, placement, probes, sampling/rejection options, and Social Network collector/Jaeger tuning. |
| Deployment CPD options | [Shared validation](../../utils/checkpoint_distance.py) and both one-shot scripts accept fixed CPD or paired range bounds, removing stale alternate keys when switching formats. The Hotel shell entry point sources the profile and virtual environment. |
| Collector load generator (September 13) | [spanload](../../utils/spanload/README.md) rebuilds the missing lightweight OTLP generator. It constructs protobuf directly, supports zero/custom/example attribute profiles and binary bridge size proxies, applies aggregate rate ramps, and records rejection/failure/queue/drain counters plus optional collector metrics. The build script and collector fixture make the setup durable; unresolved paper inputs stay explicit. |
| Variable generator payloads (September 13) | [Distribution inputs](../../utils/spanload/README.md#payload-size-distributions) add weighted, empirical, and inclusive uniform byte lengths, including per-CPD table entries. Each checkpoint gets a reproducible draw by seed/sequence position; its exact byte prefix is sent without per-checkpoint allocation. Manifests retain the distribution and results report attempted-payload byte totals and bounded histograms. Fixed sizes and checkpoint cadence remain supported. |
| Simulator histogram import (September 14) | [generate_spanload_distributions.py](../../utils/generate_spanload_distributions.py) converts the supplied PB0/CGP0/SB3 percentage file into count-weighted generator profiles, checks histogram integrity, and subtracts its declared 4-byte key/type overhead. All tail bins and simulation provenance survive; [Uber day 1 random-CPD 2–8 profiles](../../utils/spanload/profiles/uber-day1-random2-8/manifest.json) are included. |
| Collector saturation ramps (September 14) | [run_spanload_ramp.py](../../utils/run_spanload_ramp.py) measures vanilla/PB/CGPB/SB export curves with zero or ten example semconv attributes, a one-CPU/4-GiB collector, and separate generator cores. `--profile` selects and records the baseline. Seeded `--checkpoint-fraction` sampling matches the imported corpus's 52.51% checkpoint share. Steady-window export/CPU samples exclude startup/drain; raw data, resource limits, and plots are retained. |
| Protobuf correction and sender checks (September 14) | The initial file fixture used JSON; the intended experiment uses protobuf to `/dev/null`. The fixture and `--export-format` now make this explicit. Repeated `--generator-cpus` launches independent senders sharing the aggregate rate, measures their common active window, and weights payload means by count. [check_spanload_capacity.py](../../utils/check_spanload_capacity.py) checks one/two/four senders against one fixed collector; separate optional cases profile CPU and change collector batch size. |
| Complete repeated ramps (September 14) | [run_spanload_suite.py](../../utils/run_spanload_suite.py) runs shared 16/20-point grids through all variants, with three repetitions and matched limiter controls. `--complete-grid` separates saturation detection from early stopping. A separate collector fixture adds `memory_limiter`, `GOMEMLIMIT=2400MiB`, and 8,192-span batches. Atomic progress records and per-stage archives support interrupted sessions. [The analyzer](../../utils/analyze_spanload_suite.py) independently checks raw counts/windows and plots means with sample SD in a combined figure. |

Tomislav-RetCtx: two earlier Hotel repairs were made during the initial
application check: `SearchHandler` propagates availability errors, and
reservation cache counters use atomic increments. Their regression tests are in
`examples/dsb_hotel/tests`. The later instruction to preserve service
implementations remains in force: the reverse-TTL work changes SDK/infrastructure
code, and Social Network workflow implementations have not been modified.

## Experiment and deployment records

Tomislav-RetCtx: the initial maximum-sized-Bloom experiment was superseded by
[the corrected fixed-2/fixed-4/random-2–6 comparison](/users/tomislav/deployments/dsb-sn/cgpb-es-window-cpd-20260910/RESULTS.md).
The [fixed-6 follow-up](/users/tomislav/deployments/dsb-sn/cgpb-es-fixed6-20260910/RESULTS.md)
measured 230 decoded bridge bytes/request, versus 243.62 for random 2–6 on this
finite-depth graph. These runs used the actual Social Network Lua request format
and had rejection disabled; they do not measure the new reverse-TTL policy.

The [ramp review](/users/tomislav/deployments/dsb-sn/ramp-review-20260911/REVIEW.md)
and [allocation evidence](/users/tomislav/deployments/dsb-sn/ramp-review-20260911/APP_ALLOCATIONS.md)
separate the saved controlled manifests from the current uncapped application
deployment. No 2k–5k ramp or deployment change accompanies this revision.

Tomislav-RetCtx: the September 13 [collector load-generator recovery](collector_load_generator.md)
compares section 5.5 of the supplied paper with the preserved telemetrygen
patches, records the missing experiment inputs, and distinguishes the draft's
zero/ten-attribute benchmark from the older R5 measurements. The preserved
generator builds; this review did not run collector load or alter deployment.

The subsequent standalone rebuild passed race tests and a small local collector
check: 240 spans were accepted/exported across all eight profile/bridge
combinations and an HTTP ramp. Binary and local container builds passed; the
[recovery record](collector_load_generator.md) links the retained verification
artifacts. This work did not modify application services or Kubernetes.

Tomislav-RetCtx: the variable-payload revision passed race tests and a further
local gRPC/HTTP collector check with 3,600 exported spans and 1,200 checkpoints.
All three distributions preserved exact sampled wire lengths and size accounting;
seeded sizes were stable across transports, worker counts, and batch boundaries.
The [verification record](collector_load_generator.md#local-verification) links
the retained artifacts and rebuilt local container image.

Tomislav-RetCtx: the September 14 histogram import passed four converter tests
and native dry-run validation of all three supplied distributions. Every bin
weight and adjusted byte length survived, including SB's rare tail. The
[verification record](collector_load_generator.md#local-verification) retains
the supplied input and generated profiles; no collector traffic was sent.

Tomislav-RetCtx: the initial **JSON-export** zero-semconv saturation run completed 37 rate steps.
Vanilla/PB/CGPB/SB plateaued near 386.7k/256.2k/254.6k/251.5k exported spans/s
with a one-CPU, 4-GiB collector. Fractional checkpoint sampling passed Go race
tests; steady-window calculations passed Python tests and all recorded counters
reconciled. The [report](/users/tomislav/deployments/collector-load/spanload-zero-ramp-20260914T144131Z/RESULTS.md)
includes curves, configuration, and limitations. Temporary collectors were
removed; application implementations and deployments were unchanged.

Tomislav-RetCtx: the matching **JSON-export** ten-attribute comparison completed 22 rate steps
with the same binaries and limits. Vanilla/PB/CGPB/SB reached approximately
75.2k/70.0k/69.9k/69.2k spans/s; bridge throughput penalties were 6.8%/7.0%/8.0%
relative to vanilla with the same profile. All counters reconciled and temporary
collectors were removed. The [comparison report](/users/tomislav/deployments/collector-load/spanload-semconv10-ramp-20260914T153113Z/RESULTS.md)
retains curves, data, and the exact example semconv attributes.

Tomislav-RetCtx: the corrected **protobuf-export** ramps use four independent
open-loop senders sharing one collector and an aggregate offered rate.
Vanilla/PB/CGPB/SB reached approximately 962.5k/533.5k/532.5k/531.1k spans/s with
zero attributes and 113.2k/105.9k/106.5k/105.2k with ten. Sender-count controls
and CPU profiles support a collector CPU limit; larger collector batches raised
zero-attribute vanilla to 996.1k/s but did not improve the ten-attribute profile.
All 37 ramp points and 16 controls reconciled, and temporary collectors were
removed. The [corrected report](/users/tomislav/deployments/collector-load/spanload-proto-ramps-20260914T155957Z/RESULTS.md)
supersedes the initial JSON results for this experiment and preserves both sets.

## Verification

Tomislav-RetCtx: backend and SDK tests cover original checkpoint protection,
independent reverse draws, exact payload preservation, both span kinds, TTL
0/255, partial bundle emission, idempotence, sampling, and nested concurrent
fanout. Generated-wrapper tests compile and execute PB/CGPB/SB/vanilla templates.
Existing topology, generator, deployment-option, and collector validation tests
cover their respective earlier additions.

On September 11, 2026, `go test -race` passed for `runtime/core/backend`,
`runtime/plugins/otelcol`, and `plugins/opentelemetry`, including a 79-span
concurrent tree for each bridge variant. All seven checkpoint-distance/Hotel
deployment-option tests and shell syntax checks also passed.

The reverse-TTL revision requires rebuilt application images and consistently
updated SDKs along the call path. Local verification is distinct from the earlier
Kubernetes builds and request measurements linked above.

Tomislav-RetCtx: the September 13 probability revision passed SDK/backend/wrapper
and collector-receiver race tests, all 13 deployment-option tests, and shell
syntax checks. The concurrent-tree coverage
now includes every probability policy as well as TTLs. No service implementation
or running deployment is changed by this revision.
