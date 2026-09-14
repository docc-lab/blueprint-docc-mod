# Collector load-generator recovery

Tomislav-RetCtx: reviewed September 13, 2026 against the supplied
`bridges.pdf`, section 5.5 (page 13), Figure 13 (page 14), and the surviving
generator source. This records the recovered experiment specification; it does
not report a new collector measurement.

## Experiment in the draft

The workload sends synthetic spans directly to the collector and ramps the
offered span rate to measure the cost of carrying bridge metadata. It does not
exercise application services or SDK checkpoint/reverse-routing decisions.

| Dimension | Specified in the draft |
| --- | --- |
| Collector budget | One CPU core and 4 GB of memory. |
| Export destination | File exporter writing to `/dev/null`, retaining export serialization cost. |
| Baseline profiles | Zero span attributes, or ten semconv span attributes. Bridge metadata is additional to the baseline. |
| Variants | Vanilla, P-Bridge, CG-Bridge, and S-Bridge. Figure 13 shows CPD 2 and 8 for each bridge. |
| Bridge payload | Average checkpoint payload size for each bridge/distance, with depth/ordinal data on non-checkpoint spans. Section 4 describes raw byte-valued bridge attributes. |
| Measurement | Collector span-export throughput as offered ingest rate increases. The draft requests replacing the current bars with ramps. |
| Draft results | Approximately 25% less throughput with bridge metadata for the zero-attribute profile, and 6.5% less with ten semconv attributes. These are reported results, not results reproduced on this node. |

The draft does not define Figure 13's `P0`-`P3` groups or its throughput axis
units, the ten attributes and values, the exact payload-size table used by this
experiment, or the full collector pipeline and file-export format. Collector
count, batch sizes, rate steps, warmup, measurement windows, and repetitions also
need to be recovered or explicitly chosen for a new run. The written `4 GB`
budget does not establish whether the original manifest used `4G` or `4Gi`.

## What survived

The sibling `opentelemetry-collector-contrib` repository contains patched
`cmd/telemetrygen/pkg/traces/{config,traces,worker}.go`. Commit `9a32648c9a`
(July 16) added `--bridge`, `--cpd`, and `--br-size`; `1da150cf96` (July 5)
added `--event-name`. The bridge generator gives every CPD-th span `_br`, and
other spans `_d` (PB/CGPB) or `_o` (SB).

This source builds, but does not directly reproduce the draft's profiles:

- It always adds `network.peer.address` and `peer.service` to every span, so
  plain mode still has two span attributes. It also adds resource `service.name`.
- Bridge values are fixed-length strings, rather than OTLP `bytes_value`.
  Equal value lengths do not establish equal decoding/export serialization cost.
- `--rate` is per worker. The older
  [ramp script](../../utils/ramp_telemetrygen_at_fixed_app.sh) divides its nominal
  total by collector count but launches four workers per process; its configured
  aggregate rate is therefore four times that label, before generator/export
  limits. It also suppresses generator output and ignores failures.
- The SDK batch processor sits between span generation and export. Generated
  counts and configured rates alone do not establish collector input throughput.

The [older R5 notes](../../examples/dsb_sn/notes/r5_overloaded_collector_throughput.md)
describe nine collector pods, a `tgenload` DaemonSet, a batch/file pipeline, and
earlier payload and attribute-count sweeps. These notes are useful evidence, but
their results and two-default-attribute generator are not the zero/ten-attribute
experiment in the current draft. In particular, the old metric is accepted spans
per second, while Figure 13 specifies exported spans per second.

The documented `/tmp/tgenimg` entrypoint, `/tmp/*_measure.sh` scripts,
`/tmp/telemetrygen` binary, and `~/runs` results are absent at those locations.
The draft's separate lightweight generator has not been found in the inspected
local repositories or their available history.

## Rebuilt generator

Tomislav-RetCtx: [utils/spanload](../../utils/spanload/README.md) now provides
the standalone replacement, built by [build_spanload.sh](../../utils/build_spanload.sh).
It constructs OTLP protobuf directly over gRPC or HTTP, adds no implicit span
attributes, uses binary bridge values, and accepts explicit per-CPD payload sizes.
The included `semconv10-example` profile is illustrative; custom typed JSON
profiles supply the exact experiment attributes when available.

The shared scheduler applies one aggregate rate across all endpoints/workers.
Bounded queues, partial OTLP rejection, failed/unconfirmed RPCs, cancellation,
and drain are counted. Each ramp has separate warmup/measurement records,
resolved configuration, profile/payload provenance, and optional raw collector
metrics snapshots. The collector fixture now uses batch/protobuf-file settings
as clarified by the user. Its initial JSON setting was incorrect for the intended
experiment; the earlier JSON measurements remain separately labeled.

Tomislav-RetCtx: checkpoint payload sizes can also vary per emitted `_br` using
a weighted discrete distribution, empirical sample frequencies, or an inclusive
integer uniform range. `--bridge-distribution FILE` selects a JSON distribution;
`--sizes-file` also accepts distributions in bridge/CPD entries. Fixed sizes
remain supported. A seed and checkpoint sequence position determine each draw
independently of worker scheduling. The resolved distribution and actual
attempted-payload byte totals/histograms are retained with the run.

Only sampled prefixes of a preallocated byte reservoir are serialized, keeping
generation free of per-checkpoint allocations and avoiding maximum-size padding.
This models the marginal distribution of checkpoint value lengths, including
large-payload tails; it does not model topology, inter-checkpoint correlations,
or random checkpoint frequency. Size histograms include attempts from failed
RPCs because OTLP acknowledgements cannot identify accepted sizes individually.

Tomislav-RetCtx: on September 14, `payload_percentages.json` supplied complete
PB0/CGP0/SB3 size histograms for Uber day 1 with random CPD 2–8 and seed 42.
[The converter](../../utils/generate_spanload_distributions.py) produces
[load-generator profiles](../../utils/spanload/profiles/uber-day1-random2-8/manifest.json)
using exact counts, retaining all 1,675 SB sizes and the single-occurrence tail.
Input sizes include the `_br` key and type; subtracting those 4 bytes gives mean
OTLP value lengths of 20.09182/21.16148/29.42263 bytes for PB/CGPB/SB. The source
and output hashes, corpus, no-prime mode, and SB3 encoding/queue settings are
recorded in the profile manifest. This supplies payload-size distributions;
checkpoint cadence and other metadata still require separate configuration.

## Experiment requirements

Record offered, receiver-accepted, and exported rates
separately, together with collector CPU, memory, failures, and restarts. Confirm
that generators can keep collectors saturated before treating a plateau as a
collector limit.

Keep configuration, image/source versions, profile definitions, ramp steps,
raw metrics, and plotting code in durable experiment artifacts. Historical
payload sizes and payloads from the current random-CPD/reverse-truss SDK should
be separately identified; the existing fixed-cadence payload proxy does not
simulate those new policies.

## Local verification

Sourced `~/.profile` and the Blueprint virtual environment, then successfully
built the preserved source with `GOTOOLCHAIN=go1.24.13 go build` and checked
`telemetrygen traces --help`. The binary and PDF extraction/renderings are under
`/tmp/bridges-paper-review/`. This was a build/help check only; no load was sent
and no deployment was changed.

The replacement's local gRPC/HTTP tests pass with the race detector, and the
existing contrib collector binary validates `utils/spanload/collector.yaml`.
See the generator README for the verification command and coverage.

The rebuilt static executable and local image `spanload:retctx-20260913` also
passed a real-collector loopback check: eight gRPC baseline/bridge combinations
plus an HTTP warmup/ramp produced 240 unique spans. Receiver-accepted and
exporter-sent counters both reached 240. Exported JSON confirmed zero or ten
baseline attributes, additional byte-valued bridge metadata, checkpoint cadence,
and exact payload lengths. [The artifact summary](/users/tomislav/deployments/collector-load/spanload-smoke-20260913-wvmod5o2/SUMMARY.json)
has the results; its directory retains configuration, binary hashes, raw spans,
metrics, and per-run manifests. This was a small correctness check, not a
saturation benchmark. The temporary collector was stopped; Kubernetes was not
modified and the generator image was not pushed.

Tomislav-RetCtx: the variable-size follow-up passed `go test -race ./... -count=1`
and native/container builds (`spanload:distributions-20260913`). A local collector
accepted and exported 3,600 spans, including 1,200 checkpoint payloads, across
weighted, empirical, and uniform inputs over gRPC and HTTP. Decoded lengths
matched independent seeded draws and reported histograms/byte totals. Different
worker counts and batch boundaries yielded the same per-position sizes. Empty
checkpoint values retained their binary type. The
[distribution verification artifacts](/users/tomislav/deployments/collector-load/spanload-distributions-20260913-huo8w7ta/SUMMARY.json)
include a replayable verification script, inputs, manifests, raw exported spans,
metrics, and binary/image hashes. The temporary collector was stopped after this
correctness check.

Tomislav-RetCtx: the September 14 histogram converter passed four tests with
invalid-input subcases, including byte-accounting and single-occurrence tail
preservation. Native `spanload --dry-run` loaded all three real profiles and
preserved every normalized bin weight, range, and mean; sample byte lengths
matched independent seeded draws. The
[import verification record](/users/tomislav/deployments/collector-load/spanload-histogram-import-20260914-tx90zsw9/SUMMARY.json)
retains source/profiles and raw dry runs. This conversion required no binary
change or collector traffic.

Tomislav-RetCtx: the initial September 14 **JSON-export** zero-semconv saturation comparison
ran vanilla, PB, CGPB, and SB through separate rising-rate ramps with a one-CPU,
4-GiB collector and the batch/JSON-file-to-`/dev/null` pipeline. A new seeded
`--checkpoint-fraction` option matched the corpus's 52.51% checkpoint share;
conditional sizes came from the supplied histograms. Across 37 rate steps, the
final export plateaus were about 386.7k/256.2k/254.6k/251.5k spans/s respectively,
at approximately 99% collector CPU. Generator CPU stayed below 0.81 of its four
available cores. The
[full report and ramp figures](/users/tomislav/deployments/collector-load/spanload-zero-ramp-20260914T144131Z/RESULTS.md)
retain steady-window calculations, raw counters, process/resource metadata,
source/binary hashes, and the initial connection-refusal qualification. All
phase counters reconciled with collector acceptance/export deltas; temporary
collectors were removed after completion. These are single ramps per variant.

Tomislav-RetCtx: the **JSON-export** ten-attribute follow-up used `--profile semconv10-example`
with identical generator/collector binaries, CPU/memory limits, histograms,
checkpoint share, and pacing settings. Across 22 steps, vanilla/PB/CGPB/SB
plateaued at 75,160/70,025/69,911/69,178 spans/s, around 98% collector CPU.
The bridge throughput penalty relative to the same-profile vanilla baseline
was 6.83%/6.98%/7.96%, versus 33.75%/34.17%/34.96% without semconv. Generator
CPU stayed below 1.20 cores; all 26,648,096 acknowledgements matched exports,
with no RPC failures, collector refusals, or exporter failures. Temporary
collectors were removed. The
[comparison report](/users/tomislav/deployments/collector-load/spanload-semconv10-ramp-20260914T153113Z/RESULTS.md)
includes both ramp curves, raw data, and an analysis script. This is one ramp
per variant using the illustrative ten-attribute profile, not a recovery of
the paper's exact attribute values.

Tomislav-RetCtx: the corrected September 14 runs use the requested protobuf file
exporter to `/dev/null`, with unchanged generator/collector binaries and the same
one-CPU/4-GiB collector budget. Four independent open-loop generators share the
aggregate offered rate. Across 37 steps, vanilla/PB/CGPB/SB reached
962,489/533,526/532,476/531,108 spans/s with zero attributes and
113,221/105,889/106,481/105,207 with the ten-attribute example. Bridge throughput
penalties were 44.57–44.82% and 5.95–7.08%, respectively. These protobuf results
supersede the JSON measurements for the intended experiment.

Twelve separate one/two/four-sender controls kept the collector fixed. Moving
from two to four senders added only 0.63–2.00%, with collector CPU at 99–100% and
sender headroom remaining. Separate CPU profiles locate substantial work in
protobuf decoding, allocation, and garbage collection. Two more controls changed
collector batches from 512 to 8,192 spans: zero-attribute vanilla rose to 996,110/s,
while the ten-attribute result fell to 108,037/s. Batching is not a uniform gain.
All 37 ramp points and 16 controls passed independent counter/window audits;
238,826,524 ramp acknowledgements matched collector exports with no RPC failures,
receiver refusals, or exporter failures. Bounded sender queue drops remain visible.
The [corrected report and figures](/users/tomislav/deployments/collector-load/spanload-proto-ramps-20260914T155957Z/RESULTS.md)
retain full raw data, CPU profiles, and a reproducible analysis script. Temporary
collectors were removed; application services and Kubernetes were unchanged.

PDF provenance: `/users/tomislav/bridges.pdf`, SHA-256
`5561af7d07c0b5eb362dfb53d2c2ecec1d90e348dd855676ce96798a1b8daa71`.
