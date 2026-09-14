# Utils

Utility scripts for blueprint-docc-mod.

Tomislav-RetCtx: the [session change map](../docs/dev/tomislav_retctx_changes.md)
records the SDK, generator, topology, Hotel deployment, and experiment changes.

Both `build_deploy_dsb.sh` and `build_deploy_hotel.sh` accept
`--cpd-min N --cpd-max N` for inclusive randomized checkpoint distances in PB,
CGPB, and SB. Use them instead of `--cpd`; valid distances are 1–256. The bounds
are served to the SDK by the collector config pipeline. See the
[TTL protocol and rollout details](../docs/dev/randomized_checkpoint_distance.md).

Tomislav-RetCtx: both scripts also accept `--reverse-policy ttl|probability|inverse_depth|depth_linear`.
Fixed probability requires `--reverse-probability P` in `[0,1]`. These select
SDK return routing through collector discovery; enable leaf rejection separately.
See [probability formulas and examples](../docs/dev/reverse_checkpoint_probability.md).

## Collector span load generator

Tomislav-RetCtx: [spanload](spanload/README.md) generates OTLP protobuf directly
for collector-only tests. It supports exact zero/custom attribute profiles, an
explicit ten-attribute example, binary PB/CGPB/SB size proxies, aggregate-rate
ramps, and generator/collector metric artifacts. Build with
`utils/build_spanload.sh`; use `--dry-run` to inspect a profile without exporting.
Tomislav-RetCtx: checkpoint sizes can be fixed or sampled from weighted,
empirical, or uniform JSON distributions with `--bridge-distribution FILE`,
including per-CPD entries in `--sizes-file`. Seeded draws and observed size
histograms make variable-payload experiments reproducible and inspectable.
The original paper's missing parameters remain explicit inputs.

Tomislav-RetCtx: [generate_spanload_distributions.py](generate_spanload_distributions.py)
imports simulator `payload_percentages.json` histograms. It preserves exact
count weights and the full tail, subtracts the declared 4-byte `_br` key/type
overhead, and saves PB/CGPB/SB profiles plus provenance. The
[import guide](spanload/README.md#simulator-histogram-import) includes the supplied
Uber day 1 random-CPD 2–8 profiles and usage examples.

Tomislav-RetCtx: [run_spanload_ramp.py](run_spanload_ramp.py) compares vanilla/PB/CGPB/SB
workloads with zero attributes or `--profile semconv10-example` using a one-CPU,
4-GiB test collector, rising offered span rates, and steady-window export/CPU
measurements. It derives the measured
checkpoint share from the profile manifest via `--checkpoint-fraction`, records
generator headroom/losses, and saves raw samples plus ramp plots. See the
[ramp guide](spanload/README.md#collector-saturation-ramp) for setup and controls.

Tomislav-RetCtx: the corrected collector fixture uses protobuf file export to
`/dev/null`; the initial JSON measurements are labeled separately. The ramp
records `--export-format` and supports repeated `--generator-cpus` for independent
senders with a shared aggregate offered rate. [check_spanload_capacity.py](check_spanload_capacity.py)
holds a single protobuf collector at one CPU and 4 GiB while increasing sender processes
from one to two to four, retaining CPU, RPC latency, and reconciled span counters.
Separate cases profile collector CPU and compare collector batches of 512 and
8,192 spans. The [corrected experiment report](/users/tomislav/deployments/collector-load/spanload-proto-ramps-20260914T155957Z/RESULTS.md)
retains both attribute-profile ramps, all capacity controls, and reproducible plots.

Tomislav-RetCtx: [run_spanload_suite.py](run_spanload_suite.py) completes shared
16/20-point grids for all variants with three repetitions, a memory limiter,
explicit Go memory budget, and 8,192-span collector batches. Separate controls
isolate the limiter. [analyze_spanload_suite.py](analyze_spanload_suite.py)
audits raw counters and generates the combined figure with mean/SD across runs.
See the [suite configuration](spanload/README.md#run-and-ramp).

## Hotel Reservation

`build_deploy_hotel.sh` generates, builds/pushes, and optionally deploys Hotel
Reservation using the same custom collector, Jaeger tuning, and Kubernetes
conversion as social network. It sources the profile and activates `.venv`.

```bash
utils/build_deploy_hotel.sh --help
utils/build_deploy_hotel.sh -s docker_cgpb_es -n cgpb_es_run1 \
  --cpd 2 --gc natural --collector passthrough \
  --frontend-deploy --nodeport --apply
```

Omit `--apply` to build without deploying; add `--skip-build` to generate
manifests only. The script supports bridge variants, collector rebuilds, image
tags, namespaces, node placement, sampling, and reverse leaf rejection. Reverse
TTLs use the collector CPD configuration; the alternative probability policies
use `--reverse-policy`. `--rt-policy`/`--rt-depth` remain deprecated.
Hotel constructors seed their own data. See the
[hotel deployment guide](../examples/dsb_hotel/README.md) for options and API checks.

## pin_nodes.py

Pins Kubernetes deployments to specific nodes and sets CPU resource requests/limits using a node-pinning YAML file.

**Requirements:** PyYAML (`pip install pyyaml`)

**Usage:**

```bash
python pin_nodes.py <pinning.yaml> <k8s-dir> [--dry-run]
```

- **pinning.yaml** – Path to a node-pinning config (e.g. `examples/dsb_sn/node-pinning-sb.yaml`). Format: top-level keys are node names (`kubernetes.io/hostname`); each value is a list of `service-name: { requests_cpu: N, limits_cpu?: N }` (CPU in millicores).
- **k8s-dir** – Directory containing `*-deployment.yaml` files (e.g. `examples/dsb_sn/build_sb/k8s`).
- **--dry-run** – Print planned changes without modifying files.

**Example:**

```bash
cd blueprint-docc-mod
python utils/pin_nodes.py examples/dsb_sn/node-pinning-sb.yaml examples/dsb_sn/build_sb/k8s
```

Then apply (or re-apply) the manifests:

```bash
kubectl apply -f examples/dsb_sn/build_sb/k8s/
```
