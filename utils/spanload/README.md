# spanload

Tomislav-RetCtx: a standalone collector load generator rebuilt from the experiment
in section 5.5 of `bridges.pdf`. It constructs OTLP protobuf messages directly,
without a tracing SDK. It supports OTLP/gRPC and OTLP/HTTP protobuf, bounded
concurrency, rate ramps, and optional collector-metrics snapshots.

The paper's exact lightweight generator, ten-attribute profile, `P0`-`P3`
definitions, and complete payload-size table were not recovered. This tool makes
those inputs explicit; it does not claim to reproduce the published numbers.
See the [recovery record](../../docs/dev/collector_load_generator.md).

## Build

From the Blueprint repository root:

```bash
utils/build_spanload.sh
utils/spanload/bin/spanload --help
```

The script sources `~/.profile` and `.venv`, isolates this module from the root
Go workspace, and defaults to Go 1.24.13. Optional container build/push:

```bash
utils/build_spanload.sh --image 10.10.1.1:30000/spanload:my-run
utils/build_spanload.sh --image 10.10.1.1:30000/spanload:my-run --push
```

The image contains the static binary and CA certificates and runs as UID 65532.
For containerized local testing, `--network host` lets the generator use the
collector fixture's loopback endpoints. Artifact mounts must be writable by that
UID. The build script does not deploy anything.

## Profiles and payloads

| Option | Baseline span attributes |
| --- | --- |
| `--profile zero` (default) | Exactly zero. |
| `--profile semconv10-example` | The ten typed attributes in [the example file](profiles/semconv10-example.json); these are illustrative, not the recovered paper profile. |
| `--profile custom --attributes-file FILE` | Exactly the attributes in a typed JSON array. |

No resource attributes, scope metadata, events, links, or operation name are
added by default. Use `--resource-attributes-file` or `--span-name` explicitly
when those belong in the workload. Every span has a unique trace/span ID pair,
a synthetic parent ID, and timestamps. These are independent synthetic records,
not an application call graph.

Custom attributes use this format (types: `string`, `int`, `double`, `bool`,
`bytes`; byte values in the input JSON use base64):

```json
[
  {"key": "http.request.method", "type": "string", "value": "GET"},
  {"key": "http.response.status_code", "type": "int", "value": 200}
]
```

`--bridge pb|cgpb|sb` adds one metadata attribute per span. Every CPD-th span
position carries `_br`; other positions carry `_d` for PB/CGPB or `_o` for SB.
`--cpd` accepts 1..256. This cadence is over the run's span sequence, continuing
across warmup and measurement phases; queue drops leave holes in that sequence.

Tomislav-RetCtx: `--checkpoint-fraction P` is an alternative to `--cpd`. It
independently selects each span as a checkpoint with probability P (0..1), using
the payload seed and global span position. Membership and payload size use
separate random streams. This matches a measured checkpoint share when importing
a conditional size histogram; it does not simulate forward TTLs or topology.
The manifest records the fraction; its default `cpd` field is inactive in this
mode. Fractional mode draws sizes by global span position, while fixed cadence
continues to use checkpoint position.

`_br` is an OTLP **bytes_value** with a fixed `--bridge-bytes N` length or a
length sampled from a distribution. Its deterministic opaque data is controlled
by `--payload-seed`. `_d` is a depth
uvarint; `_o` is an ordinal uvarint followed by a depth uvarint. Defaults depth=6
and ordinal=1 give 1-byte `_d` and 2-byte `_o`. Bridge values are additional to
the baseline attribute count. `_br`, `_d`, and `_o` are reserved profile keys.

Checkpoint bytes are size proxies, not reconstructable trusses. The generator
does not run Bloom filters, random forward CPDs, or reverse-return policies.
Supply sizes measured for the experiment being evaluated; there is no implicit
maximum-CPD sizing or hard-coded historical size table.

Instead of `--bridge-bytes`, pass `--sizes-file FILE`. The file must include a
source description and the selected bridge/CPD entry. For example, this records
the P-Bridge CPD=6 mean shown in the supplied Figure 14(c):

```json
{"source":"bridges.pdf Figure 14(c), P-Bridge CPD=6 mean",
 "sizes":{"pb":{"6":25}}}
```

Inspect a profile's protobuf JSON without contacting a collector:

```bash
utils/spanload/bin/spanload --dry-run --profile zero \
  --bridge pb --cpd 6 --bridge-bytes 25
```

## Payload-size distributions

Tomislav-RetCtx: `--bridge-distribution FILE` draws a byte length for each
checkpoint. It accepts these JSON forms; include a `source` description for
experiment provenance:

```json
{"source":"Illustrative mixture", "type":"discrete", "values":[
  {"bytes":16, "weight":75},
  {"bytes":32, "weight":20},
  {"bytes":128, "weight":5}
]}
```

```json
{"source":"Observed checkpoint sizes", "type":"empirical",
 "samples":[16,16,16,32,128]}
```

```json
{"source":"Uniform sensitivity sweep", "type":"uniform", "min":16, "max":128}
```

Discrete weights are relative: the first example draws 16 bytes with 75%
probability, 32 with 20%, and 128 with 5%, for an expected mean of 24.8 bytes.
Weights must be finite and nonnegative, with at least one positive; duplicates
are combined and zero weights ignored. Empirical samples are sampled with
replacement using their observed frequencies; their input order is discarded.
Uniform bounds are inclusive integers. All sizes must be in 0..1,048,576 bytes;
JSON files are limited to 4 MiB. These distributions model size variation and
large-payload tails beyond a single mean, while checkpoint frequency still comes
from `--cpd` and ordinary `_d`/`_o` lengths remain unchanged.

Run the [included mixture](profiles/payload-distribution-example.json):

```bash
utils/spanload/bin/spanload --endpoint 127.0.0.1:14317 --insecure \
  --bridge pb --cpd 6 \
  --bridge-distribution utils/spanload/profiles/payload-distribution-example.json \
  --payload-seed 42 --rates 10000 --duration 30s
```

Per-CPD tables may mix fixed integers and distribution objects. A distribution
inherits the table's `source` unless it supplies its own:

```json
{"source":"Measured workload, run identifier", "sizes":{"pb":{
  "2":16,
  "6":{"type":"empirical", "samples":[16,16,32,128]}
}}}
```

Use exactly one of `--bridge-bytes`, `--sizes-file`, or
`--bridge-distribution`. Size draws use a versioned counter-based sampler keyed
by `--payload-seed` and checkpoint position in the run. Worker count, completion
order, and batch boundaries do not change a position's size; dropped or unsent
positions can change which samples reach the collector. The sequence continues
through warmup and rate steps. This samples the marginal size distribution; it
does not preserve correlations between checkpoints or simulate a call graph.

The generator allocates one immutable byte reservoir up to the largest supported
size in the chosen distribution and samples prefix lengths without per-checkpoint
allocations. Each `_br` on the wire contains exactly the sampled length. It does
not pad smaller samples to the maximum; bytes beyond that prefix are not sent.

### Simulator histogram import

Tomislav-RetCtx: [generate_spanload_distributions.py](../generate_spanload_distributions.py)
converts `bridges.payload_size_percentages.v1` files such as the supplied
`payload_percentages.json` into `pb.json`, `cgpb.json`, `sb.json`, and a provenance
manifest in a new directory:

```bash
python utils/generate_spanload_distributions.py \
  --input /users/tomislav/payload_percentages.json \
  --out utils/spanload/runs/imported-distributions
```

The supplied file's sizes include 3 bytes for `_br` and 1 byte for its type.
The converter subtracts those **4 bytes** to obtain the OTLP byte-value length.
Actual protobuf framing is added by the transport. It checks the declared byte
accounting, exact bin counts, percentages, cumulative percentages, and size
summaries before writing. It preserves every bin, including single-occurrence
tail sizes, using integer counts as relative weights; it never expands counts
into a sample array or rounds percentages into counts.

Ready-to-use [Uber day 1 profiles](profiles/uber-day1-random2-8/manifest.json)
come from 521,305 traces and 475,488,643 spans, with 249,659,504 emitted payloads
per bridge. The source settings are random CPD 2–8, checkpoint seed 42, no-prime,
and SB3 with Lehmer encoding, service-instance queues, and single-pop enabled.

| Input → profile | Distinct sizes | Byte-value range | Mean byte-value size |
| --- | ---: | ---: | ---: |
| PB0 → [pb.json](profiles/uber-day1-random2-8/pb.json) | 7 | 12–26 | 20.09182 |
| CGP0 → [cgpb.json](profiles/uber-day1-random2-8/cgpb.json) | 12 | 12–35 | 21.16148 |
| SB3 → [sb.json](profiles/uber-day1-random2-8/sb.json) | 1,675 | 14–7,522 | 29.42263 |

Inspect a generated checkpoint using the existing binary:

```bash
utils/spanload/bin/spanload --dry-run --bridge sb --cpd 1 --payload-seed 42 \
  --bridge-distribution utils/spanload/profiles/uber-day1-random2-8/sb.json
```

These are distributions **conditional on an emitted bridge payload**. Set
`--cpd` or `--checkpoint-fraction` separately; the dry-run above uses
1 to inspect checkpoint values only. Importing random-CPD simulation results
does not automatically change spanload's cadence or ordinary `_d`/`_o` values. The input
excludes separate depth metadata and does not provide its size distribution.
Manifests retain the input/configuration and file hashes; the supplied simulator
histogram hashes are recorded as provenance, since those original histogram
files were not supplied. No simulator checkout is needed for this conversion.

The supplied corpus has a payload on `249659504 / 475488643` spans, giving
`--checkpoint-fraction 0.5250588161787073`. Using an average CPD to choose the
fraction would miss the corpus's root/leaf checkpoint contribution.

### Collector saturation ramp

Tomislav-RetCtx: [run_spanload_ramp.py](../run_spanload_ramp.py) runs vanilla,
PB, CGPB, and SB in order with `--profile zero` by default. Pass
`--profile semconv10-example` to repeat the ramp with ten baseline attributes;
bridge metadata is additional. It derives the checkpoint share
from the imported manifest and uses its conditional size distributions. Each
variant gets a fresh Docker collector with one CPU, 4 GiB, `GOMAXPROCS=1`, and
the batch/protobuf-file-to-`/dev/null` fixture. `--export-format proto` is the
default; an explicit `--export-format json` is recorded as a separate experiment.
The collector is pinned to CPU 2. Each generator has `GOMAXPROCS=4`, 8 workers,
and at most 512 spans per request. One generator on CPUs 4–7 is the default;
repeat `--generator-cpus` for independent processes. The offered rate is split
across those processes, and steady rates use their common active interval.
Select nonoverlapping physical cores for other hosts.

```bash
source ~/.profile
source .venv/bin/activate
python utils/run_spanload_ramp.py --out /path/to/new-results-directory
python utils/run_spanload_ramp.py --profile semconv10-example --out /path/to/new-semconv-results
# Four independent senders, sharing one collector (the corrected experiment):
python utils/run_spanload_ramp.py --export-format proto --profile zero \
  --generator-cpus 4-7 --generator-cpus 14-17 \
  --generator-cpus 8-11 --generator-cpus 0,1,18,19 --out /path/to/new-proto-results
```

The command requires Docker, Python `psutil`/`matplotlib`, the built generator,
and a collector image named `spanload-collector:ramp-20260914` (override with
`--collector-image`; its entry point must accept `--config`). Rates begin at
10,000 spans/s and rise through 25k, 50k, 75k, 100k, 150k, 200k, and higher as
needed. Each step runs 25 seconds. One-second raw Prometheus/process samples
measure a steady window after the first 5 seconds and before the last second;
drain is excluded. Generator counts still retain every phase's losses and
unissued work. `--rates`, `--seconds`, and CPU options allow explicit overrides.
Each step starts a fresh generator with seed 42; the collector persists for
the whole variant. The initial discard allows connections and buffers to settle.

The ramp stops after two overloaded points with at least 94% collector CPU,
exported throughput below 93% of target, and less than 12% throughput change.
Generator CPU/headroom is checked separately. JSON/CSV points, PNG/PDF/SVG
curves, raw metrics, process statistics, generator manifests, and Docker resource
settings are retained. The selected profile is recorded in each row and the
figure title; the example attribute JSON is copied into semconv runs. This is
one ramp per variant; the 25-second points are not independent repeated
experiments. Temporary collectors are stopped and
removed after each variant.

Tomislav-RetCtx: the corrected September 14 **protobuf-export** ramps used four
independent senders and one collector. Vanilla/PB/CGPB/SB reached approximately
962.5k/533.5k/532.5k/531.1k spans/s without semconv and
113.2k/105.9k/106.5k/105.2k with the ten-attribute example. Collector CPU was
99–100%; doubling senders from two to four added only 0.6–2.0% in separate
vanilla/SB controls. All 37 ramp points and 16 diagnostic controls reconciled.
The [corrected report, curves, and raw data](/users/tomislav/deployments/collector-load/spanload-proto-ramps-20260914T155957Z/RESULTS.md)
also retain CPU profiles and a collector-batch sensitivity check: 8,192 spans
raised zero-attribute vanilla to 996.1k/s but lowered the ten-attribute result.

Tomislav-RetCtx: [check_spanload_capacity.py](../check_spanload_capacity.py) runs
the sender-count, separate CPU-profile, and batch-size controls. It uses the
same local collector image, CPU assignments, and one-CPU/4-GiB protobuf fixture;
these host-specific settings are recorded in the script. `--seconds` defaults
to 25 (minimum 20), and `--cases` selects comma-separated exact case names:

```bash
python utils/check_spanload_capacity.py --out /path/to/new-capacity-results
python utils/check_spanload_capacity.py \
  --cases zero-none-proto-1cpu-4gen-pprof,zero-none-proto-1cpu-4gen-batch8192 \
  --out /path/to/new-batch-profile-results
```

Tomislav-RetCtx: the initial September 14 **JSON-export** zero-semconv run
completed 37 steps and reached
plateaus of approximately 386.7k spans/s for vanilla, 256.2k for PB, 254.6k for
CGPB, and 251.5k for SB, with about 99% collector CPU. The
[report and curves](/users/tomislav/deployments/collector-load/spanload-zero-ramp-20260914T144131Z/RESULTS.md)
retain configuration, raw counters, and the startup/timing qualifications.

Tomislav-RetCtx: repeating **JSON export** with `--profile semconv10-example` and the same
binaries/settings completed 22 steps. Vanilla/PB/CGPB/SB plateaued near
75.2k/70.0k/69.9k/69.2k spans/s; bridge throughput penalties fell to
6.8%/7.0%/8.0% relative to the ten-attribute vanilla baseline. All 26,648,096
acknowledged spans matched collector exports, with no RPC or collector errors.
The [comparison report and curves](/users/tomislav/deployments/collector-load/spanload-semconv10-ramp-20260914T153113Z/RESULTS.md)
retain both profiles, counters, and a reproducible analysis script. These remain
single ramps using illustrative semconv values. Both initial comparisons used
the wrong exporter format for the intended protobuf experiment; retain them as
JSON measurements only. The corrected fixture defaults to protobuf.

## Run and ramp

Tomislav-RetCtx: [run_spanload_suite.py](../run_spanload_suite.py) runs the agreed
denser comparison with [collector-realistic.yaml](collector-realistic.yaml):
`otlp -> memory_limiter -> batch -> file`, protobuf to `/dev/null`, one CPU,
4 GiB/no swap, and `GOMEMLIMIT=2400MiB`. The limiter checks every 100ms, with a
3,072 MiB hard threshold and 512 MiB spike allowance; the collector batch target
and maximum are 8,192 spans with a 200ms timeout. Generator requests remain 512.

```bash
source ~/.profile
source .venv/bin/activate
python utils/run_spanload_suite.py --out /path/to/new-dense-results
python utils/analyze_spanload_suite.py --out /path/to/new-dense-results
```

The suite completes all 16 rates from 100k to 1.6M for zero attributes and all
20 rates from 10k to 200k for ten attributes, for every bridge and vanilla. It
runs three repetitions, rotating variant order and alternating profile order.
Each point offers load for 45s, discards the first 10s, and requires at least
30s of common steady measurement. Four independent senders share the aggregate
target. Eight separate controls toggle only the memory-limiter processor while
keeping batching and `GOMEMLIMIT` identical. The old fixture remains available
for comparisons.

`--complete-grid` on the lower-level ramp records saturation without stopping
early. `--collector-config`, `--collector-gomemlimit`, and `--min-steady-seconds`
make these settings explicit. Each point reconciles full collector acceptance
and export counters with acknowledgements. Raw losses, metrics, manifests,
configuration, and clean container exits are retained. The suite writes atomic
status/results after completed stages; `--resume` skips successfully verified
stages and requires unchanged generator/ramp source. Incomplete stage directories
are retained for inspection and must be moved aside before retrying that stage.

The independent analyzer audits the complete grid and counters, then produces
the combined 4.4 × 2.1-inch figure with one shared y-axis label. Curves show means
over repetitions and shading shows one sample standard deviation. All plotted
points are measured; plateaus use the final three rates of each repetition.

```bash
utils/spanload/bin/spanload --endpoint 127.0.0.1:14317 --insecure \
  --profile zero --bridge pb --cpd 6 --bridge-bytes 25 \
  --rates 10000,50000,100000 --duration 30s --warmup 5s \
  --workers 8 --batch-size 512 \
  --metrics-url http://127.0.0.1:18888/metrics --settle 1s \
  --out utils/spanload/runs/pb6-zero-run1
```

For vanilla, omit bridge options. For HTTP use
`--protocol http --endpoint http://127.0.0.1:14318/v1/traces` and omit
`--insecure`; TLS is selected by the URL scheme. gRPC defaults to TLS and
supports `--ca-file` for an additional CA.

**Rates are aggregate spans/second across all workers and endpoints in one
process.** Repeat `--endpoint` to distribute paced batches round-robin.
`--workers` controls concurrency **per endpoint** and does not multiply the rate.
Separate generator processes each have their own aggregate rate.

Tomislav-RetCtx: every paced generator process has its own wall-clock open-loop
scheduler. Its RPC workers wait for responses, but response completion does not
control scheduled arrivals. The multi-process ramp divides its target among
these schedulers; queue drops explain the gap between offered and attempted load.

`--rates 0` sends as fast as exports permit. `--spans N` optionally limits each
phase to N scheduled positions, also bounded by its duration. Warmup, if enabled,
runs before every measured rate and has separate counters. Connections are reused
between phases. Do not combine warmup counts with measurement counts.

In paced mode the quota is `floor(rate * duration)`, capped by `--spans` when
specified. A shared scheduler dispatches batches at their due times. Batch size
is bounded by both `--batch-size` and `max(1, ceil(rate / 10))`, avoiding very
large bursts at low rates. A phase may end with a partial batch. Batch pacing
is approximate; late scheduling can bunch batches, and batches already scheduled
may finish during drain. Unpaced mode uses the configured maximum batch size.

The per-endpoint queue is bounded by `--queue`. A full queue drops scheduled
work visibly rather than turning the ramp into a slower closed-loop workload.
Each RPC has `--timeout`; phase end or SIGINT/SIGTERM stops generation and allows
up to `--drain-timeout` for pending work. There are no application-level retries
or compression. An RPC failure does not prove the collector received no spans.

## Records and interpretation

JSONL is written to stdout. `--out` creates a **new** directory with
`manifest.json`, `results.jsonl`, and optional raw `.prom` snapshots. Existing
directories are rejected. The manifest contains the source revision/build label,
Go/runtime settings, command/config, resolved attributes, payload size source,
payload hash, and run ID. Keep the binary/image digest and collector manifest
with the results as well.

Tomislav-RetCtx: variable profiles record `checkpoint_distribution` with the
normalized probabilities or uniform bounds, expected mean, source, and sampler
version. `checkpoint_bytes: -1` marks variable size; the
`checkpoint_payload_buffer_sha256` hashes the shared byte reservoir. Fixed
profiles retain their exact size and `checkpoint_sha256`.

Phase/progress records include `attempted_checkpoint_payload`: count, total
value bytes, observed min/max/mean, and a histogram, both per endpoint and in
aggregate. Discrete/empirical supports of at most 128 sizes get exact buckets;
larger supports and uniform ranges use at most 128 inclusive integer bins.
These count attempted checkpoint values, including failed RPCs. OTLP partial
success does not identify which individual payload sizes were rejected.

Progress records are approximate concurrent snapshots. Final phase records obey:

```text
offered = attempted + queue_dropped + unsent
attempted = acknowledged + rejected + failed
```

`scheduler_unissued_spans` records paced quota that the scheduler did not reach
before its deadline/cancellation. `rejected` comes from OTLP partial-success
responses. `failed` means export RPCs with no successful acknowledgement.
`attempted_protobuf_bytes` counts uncompressed request protobuf bytes, excluding
HTTP/gRPC/TCP/TLS framing. Error counts count RPCs, not spans.

Acknowledged spans are not a measure of downstream export success. Collector
metrics are required to measure receiver-accepted and exporter-sent spans.
Each metrics snapshot records its URL, endpoint, start/finish timestamps, file
hash, and any scrape error. Before/after deltas include drain, optional settle,
and scrape timing; use those actual timestamps, not just `--duration`.
Snapshots can include traffic from other clients using the same collector.

By default the process exits 1 for RPC failures/rejections, queue drops, unsent
or scheduler-unissued spans, or scrape failures, after retaining all phase
results. `--allow-errors` keeps these records but permits exit 0 for intentional
overload experiments. Invalid inputs exit 2; interruption exits 130.

Confirm collector CPU saturation and generator headroom before interpreting a
throughput plateau as the collector's ceiling. Store offered, accepted, and
exported throughput separately. Pacing and acknowledgement counters alone do
not establish that ceiling.

## Collector fixture

[collector.yaml](collector.yaml) uses isolated loopback ports 14317/14318 and
18888, `batch -> file`, protobuf export to `/dev/null`, and explicit batch/flush
settings. Tomislav-RetCtx: the user clarified protobuf serialization on September
14; the initial JSON setting was wrong for this experiment. Writing to
`/dev/null` retains protobuf serialization and removes storage/backend work.

For a local container experiment, set `SPANLOAD_COLLECTOR_IMAGE` to the desired
collector image/tag, then from the repository root:

```bash
docker run -d --rm --name spanload-collector --network host \
  --cpus 1 --memory 4g --memory-swap 4g \
  -v "$PWD/utils/spanload/collector.yaml:/etc/otelcol/config.yaml:ro" \
  "$SPANLOAD_COLLECTOR_IMAGE" --config /etc/otelcol/config.yaml
```

This example enforces 4 GiB; the draft's `4 GB` notation does not establish its
original units. The YAML alone does not enforce CPU/RAM limits. Run generators
with sufficient resources and record their placement separately from collectors.
Stop this test collector with `docker stop spanload-collector` when finished.

## Verification

```bash
source ~/.profile
source .venv/bin/activate
python -m unittest discover -s utils -p test_generate_spanload_distributions.py -v
cd utils/spanload
GOWORK=off GOTOOLCHAIN=go1.24.13 go test -race ./... -count=1
```

Tests cover protobuf decoding for every baseline/bridge combination, exact
typed/custom profiles, payload-table validation, unique IDs and partial batches,
aggregate pacing across two gRPC endpoints and multiple workers, partial
rejection, RPC/HTTP failures, bounded queues/drain, cancellation, HTTP export,
warmup/ramp artifacts, metrics snapshots, and overwrite protection.
Distribution coverage checks frequencies, reproducible concurrent sampling,
fixed-table compatibility, empty payloads, actual protobuf lengths, bounded
histograms, exact attempted-byte accounting, and allocation-free batch refill.

Tomislav-RetCtx: the September 13 local collector check also passed for 240
unique spans across all eight baseline/bridge combinations and an HTTP ramp.
Receiver and exporter counters matched, and exported JSON preserved the exact
attribute counts and binary metadata sizes. The
[verification record](../../docs/dev/collector_load_generator.md#local-verification)
links the retained artifacts. This check established correctness, not maximum
collector throughput.

Tomislav-RetCtx: the distribution follow-up passed race tests and exported 3,600
spans with 1,200 checkpoint payloads through the local collector. Weighted,
empirical, and uniform inputs passed over both transports; decoded value lengths
and histograms matched the seeded draws, including zero-byte checkpoints.
Changing worker counts and batch sizes preserved the assigned sizes. The static
binary and local `spanload:distributions-20260913` image also passed validation.
See the [retained summary](/users/tomislav/deployments/collector-load/spanload-distributions-20260913-huo8w7ta/SUMMARY.json).

Tomislav-RetCtx: the September 14 histogram import passed converter tests for
count/percentage consistency, key/type subtraction, rare tails, malformed input,
and output preservation. The existing native binary loaded all three generated
profiles; every resolved size/probability matched the source counts, and dry-run
payload lengths matched independent seeded draws. The
[import verification](/users/tomislav/deployments/collector-load/spanload-histogram-import-20260914-tx90zsw9/SUMMARY.json)
retains the input, profiles, binary hash, and dry-run results.
