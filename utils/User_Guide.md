# DSB-SN Build & Deploy — User Guide

`utils/build_deploy_dsb.sh` is a turnkey driver that takes a DeathStarBench
Social Network (DSB-SN) bridge variant from Blueprint source all the way to a
**seeded, load-ready Kubernetes deployment** in a single command.

It reconstructs the *entire* pipeline and automatically re-applies the manual
fixes that Blueprint wiring / d2k8s / kompose do **not** produce and that get
wiped on every regeneration — so a fresh build "just works" instead of
crash-looping or silently producing an invalid run.

---

## First-time setup (fresh node)

On a brand-new CloudLab head node, run the one-shot bootstrap first — it installs
every dependency the build/deploy pipeline needs. Full built-in reference:
`utils/setup_environment.sh --help`.

```bash
cd /users/tomislav/blueprint-docc-mod
utils/setup_environment.sh
```

It runs 8 idempotent stages (safe to re-run):

1. **Base apt** — git, curl, ca-certificates, python3-venv, python3-pip
2. **Clone sibling repos** (next to `blueprint-docc-mod`) — docc-lab's
   `opentelemetry-collector-contrib` and `DeathStarBench` (`--recurse-submodules`).
   Skipped per-repo if already present — a restored copy with local changes is
   never touched.
3. **Build wrk2** — DeathStarBench's load generator: apt `build-essential libssl-dev
   libz-dev luarocks`, `luarocks install luasocket`, `make` in `DeathStarBench/wrk2`,
   install the `wrk` binary to `/usr/local/bin`
4. **Toolchain** (delegates to `install_blueprint_deps.sh`) — Go (latest, from
   go.dev, **not** apt; PATH wired into `~/.bashrc` + `~/.profile`), protobuf
   compiler + lib (`protobuf-compiler libprotobuf-dev`), the two Go protoc plugins
   (`protoc-gen-go`, `protoc-gen-go-grpc`), and kompose (latest release)
5. **Python venv** at `blueprint-docc-mod/.venv` — `pyyaml` (d2k8s + helpers) + `aiohttp` (seeder)
6. **Docker group** — adds you to `docker` (re-login to take effect)
7. **In-cluster registry** — creates the `registry` namespace + applies pvc/deployment/service (NodePort 30000)
8. **CPU pin** — pins **every** k8s node's CPUs to a fixed clock (default 2.2 GHz).
   Nodes are **auto-detected from `kubectl get nodes`** — no assumption about
   cluster size; works identically on a 10- or 15-node experiment.

**Invocation examples:**
```bash
utils/setup_environment.sh                        # standard fresh-cluster bootstrap
utils/setup_environment.sh --help                 # complete built-in reference
CPU_GHZ=2.0 utils/setup_environment.sh            # pin nodes to 2.0 GHz instead of 2.2
DSB_REPO=https://github.com/docc-lab/DeathStarBench.git utils/setup_environment.sh
COLLECTOR_REPO=<url> utils/setup_environment.sh   # use a different collector fork
utils/setup_environment.sh                        # re-run after cluster rebuild: clones/toolchain
                                                  # fast-skip; registry + pin get re-applied
```

> **Restoring instead of cloning:** if you have existing working copies (e.g. from a
> previous experiment) with uncommitted changes, copy them into place *before*
> running setup — stage 2 will detect and keep them.

**After it finishes:**
1. Open a new shell (or `source ~/.bashrc`) to pick up the Go PATH.
2. `newgrp docker` (or log out/in) for docker-without-sudo.
3. **Activate the venv** before any build — `build_deploy_dsb.sh` and the seeder
   call `python3` directly, so they need the venv's `pyyaml` + `aiohttp`:
   ```bash
   source .venv/bin/activate
   ```

> The **Prerequisites** table (§2) is the per-item breakdown of what this installs —
> consult it only if you're setting up by hand instead of via the script.

### Setting the CPU clock independently

CPU-frequency pinning lives in `utils/pin-cpu-22ghz.sh`, and the target clock is
**configurable** (default 2.2 GHz, the c220g5 base clock):

```bash
utils/pin-cpu-22ghz.sh pin              # 2.2 GHz on this host
utils/pin-cpu-22ghz.sh pin 2.0 --all    # 2.0 GHz on every k8s node
utils/pin-cpu-22ghz.sh pin --ghz 1.8    # 1.8 GHz (flag form)
utils/pin-cpu-22ghz.sh check --all      # read-only: show each node's clock
utils/pin-cpu-22ghz.sh unpin --all      # revert to powersave
```

The `--all` pin is **not** reboot-persistent — re-run `pin <ghz> --all` after node
reboots, or use `persist <ghz>` per node to install a systemd unit that survives.

---

## 1. Quick start

```bash
cd /users/tomislav/blueprint-docc-mod

# Build + deploy + seed a Path-Bridge variant at checkpoint-distance 2:
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_estest --cpd 2 --gc natural --apply --seed
```

That one command will: compile the variant, fix generated code, normalize the
collector config, build & push all images, generate k8s manifests, inject the
performance env, generate + apply node-pinning, **tear down whatever DSB-SN
variant is currently running**, deploy the new one, wait for it to be Running,
and seed the social graph. When it finishes it prints the wrk2api NodePort to
drive load against.

Build with **no deploy** (just produce manifests under `build_<name>/k8s`):

```bash
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_estest --cpd 2        # no --apply
```

---

## 2. Prerequisites (one-time per cluster)

The script assumes a working CloudLab kubespray cluster and these already in place:

| Requirement | Detail |
|---|---|
| **kubectl** | Working admin context (the script talks to the live cluster for apply/seed). |
| **Local registry** | `10.10.1.1:30000` reachable and accepting pushes. |
| **Go toolchain** | For `go run wiring/main.go`. `goimports` is **auto-installed** if missing. |
| **python3 + PyYAML** | `pip install pyyaml` — used by the config/pinning/perf-env helpers. |
| **kompose** | On `PATH` (used inside d2k8s). |
| **docker** | Working without sudo in the shell that runs the script (image builds). |
| **Collector source** | `/users/tomislav/opentelemetry-collector-contrib` with `build-and-push.sh` (only needed with `--build-collector`). |
| **Seed deps** | `pip install aiohttp`; DSB seed dir at `/users/tomislav/DeathStarBench/socialNetwork` (only needed with `--seed`). |

> See `feedback_new_cluster_bringup` for full first-time cluster setup (CPU
> pinning, otelcontribcol base image, aiohttp seed dep, etc.).

---

## 3. Choosing a variant: `-s <spec>` and `-n <name>`

- **`-s <spec>`** picks the bridge kind via the Blueprint wiring spec:
  - `docker_pb_es`  → Path Bridge (PB)
  - `docker_cgpb_es` → Call-Graph Path Bridge (CGPB)
  - `docker_sb_es`  → Sequence Bridge (SB)
  - `docker_v_es`   → vanilla (no bridge)
  - `docker_rc_es`  → control variant
- **`-n <name>`** is the build directory name → `examples/dsb_sn/build_<name>/`.
- **`--extra <str>`** appends to the kompose suffix (e.g. `--extra test2`
  produces variant `pb-estest2`). The script auto-detects the *authoritative*
  service suffix from the generated compose file, so service/daemonset names
  always match even if the `-o` name and the spec-derived suffix differ.

---

## 4. Flag reference

| Flag | Meaning | Default |
|---|---|---|
| `-s <spec>` | Wiring spec (selects bridge kind). **Required.** | — |
| `-n <name>` | Build dir name → `build_<name>/`. **Required.** | — |
| `--cpd <N>` | Checkpoint distance (baked into the otelcol config). | unchanged |
| `--gc <mode>` | App-pod GC mode: `natural` (GOGC=100, interval off) or `forced` (GOGC=off, GC 10×/s). | unset |
| `--extra <str>` | Append to the wiring suffix. | — |
| `--soft <pct>` | Collector controller soft (refuse-all) threshold %. | 50 |
| `--hard <pct>` | Collector controller hard (force-GC) threshold %. | 70 |
| `--cp-safety <f>` | CP-vs-LP shedding dial (higher = protect CP harder). | 1 |
| `--no-pin-requests` | Node-pin places services on nodes (nodeSelector) but sets **no** CPU requests — avoids over-reserving cores. | off |
| `--anti-affinity` | Placement: **no two traced services with a call edge share a node** (static 9-node layout) — un-masks per-hop bridge overhead. Use with `--wrk2api-deploy`. | off |
| `--one-per-node` | Placement: **every named service on its own node**; nodes **auto-detected** from `kubectl get nodes` (tainted CP excluded, needs ≥14 schedulable). Caches/DBs ride along; near-idle userid+tracepressure share the CP node. Use with `--wrk2api-deploy`. | off |
| `--wrk2api-deploy` | wrk2api as a single pinnable **Deployment** instead of a per-node DaemonSet — required for the isolation placements to cover the frontend hop. | off (DaemonSet) |
| `--collector <mode>` | otelcol pipeline mode. `passthrough` = strip priority **and** memory_limiter → `receivers→batch→exporters` (use for **overhead** experiments so the collector imposes no shedding/limiting). Unset = wiring default (priority for bridges; for shedding studies). | unset |
| `--build-collector` | Rebuild + push the `otelcontribcol` base image first. | off |
| `--skip-build` | Skip d2k8s image builds (reuse existing images — only valid if images for this exact suffix are already in the registry). | off |
| `--apply` | Deploy: evict the live variant, apply, wait for Running. | off |
| `--seed` | After apply, NodePort-patch wrk2api and seed the social graph. | off |
| `-h`, `--help` | Complete built-in reference (works everywhere — before the docker guard). | — |

> `force_gc`, `gc_soft_interval`, `gc_ultrasoft_interval` for the collector
> controller are script variables near the top of the file (defaults
> `true` / `1s` / `0s` = the rev2-proven config). Edit there if you need them.

---

## 5. What it does (the 8 stages)

| Stage | Action | Auto-fix it applies |
|---|---|---|
| **[0]** | (`--build-collector`) build+push `otelcontribcol`. | — |
| **[1]** | Export `OT_BRIDGE` (derived from the spec: `docker_<kind>_es` → `<kind>`), then `go run wiring/main.go -w <spec> -o build_<name>`. | **selects the per-variant OT client/server wrapper template** at codegen time (vanilla gets a true no-bridge wrapper; pb/cgpb/sb get theirs) — without it every variant silently got the path-bridge wrapper |
| **[1b]** | Detect the real kompose suffix from the generated compose. | prevents `--daemon-services` silently no-op'ing |
| **[1c]** | `goimports -w` over generated Go. | **strips dead-import codegen** (unused `encoding/base64`/`binary`) that breaks `go build` |
| **[2]** | Normalize the otelcol `priority` processor config + set `cpd`. | **rewrites the STALE `high/mid/low_percentage` schema** the wiring emits to the current `soft/hard/cp_safety_factor/...` the collector image expects (otherwise CrashLoopBackOff) |
| **[3]** | Copy `.local.env` → `docker/.env` (address map for compose). | — |
| **[4]** | d2k8s: build/push images, emit k8s; otelcol as DaemonSet(+Local); wrk2api as DaemonSet **without** `InternalTrafficPolicy: Local` — or as a single pinnable **Deployment** with `--wrk2api-deploy`. | DaemonSet + traffic-policy wiring |
| **[5]** | `inject_perf_env.py`. | **re-adds** Jaeger ES-bulk tuning (7 vars) + otelcol GOMEMLIMIT/resources + optional app GC mode |
| **[6]** | Node pinning: **auto-generate** `node-pinning-<name>.yaml` if absent (`gen_node_pinning.py`), then `pin_nodes.py`. | **re-applies pinning** d2k8s strips |
| **[7]** | **Auto-evict** the currently-live variant (detected from `otelcol-<v>-ctr` pods), then `kubectl apply` + wait for Running. | only one variant on the cluster at a time |
| **[8]** | (`--seed`) NodePort-patch wrk2api + `init_social_graph.py`. | — |

---

## 6. Node pinning — three placement modes

Stage [6] auto-generates `node-pinning-<name>.yaml` (if absent) via
`utils/gen_node_pinning.py`, in one of three modes:

| Mode | Flag | Design | Use for |
|---|---|---|---|
| **Canonical hot-triple** | (default) | `composepost`+`hometimeline`(+cache) co-located on one hot node so its otelcol absorbs the pressure; everything else spread across 9 nodes. | shedding/controller studies (asymmetric pressure) |
| **Anti-affinity** | `--anti-affinity` | No two traced services with a **call edge** share a node (static 9-node layout); every traced hop crosses the network. | overhead studies (un-masks per-hop propagation cost) |
| **One-per-node** | `--one-per-node` | Every named service on its **own node**. Nodes **auto-detected from `kubectl get nodes`** (schedulable only — tainted CP excluded); needs ≥14. Caches/DBs ride with their service; near-idle `userid`+`tracepressure` share the untainted CP node; wrk2api and jaeger+ES each get a node. | maximal isolation on ≥15-node clusters |

Both isolation modes should be combined with **`--wrk2api-deploy`** so the
frontend hop is isolated too (a DaemonSet can't be anti-affined).

- **Custom pinning**: an existing `node-pinning-<name>.yaml` is **respected** —
  hand-edit it (or generate then tweak) and it won't be overwritten.
- **Placement-only (`--no-pin-requests`)**: places services (nodeSelector) but
  emits/applies **no CPU requests**, so nothing over-reserves cores (the canonical
  requests reserve up to 25 cores for social-db). Applies to both the generated
  file *and* the apply step, so even an existing file's `requests_cpu` is ignored.
- **Generate manually** to inspect/edit before deploying:
  ```bash
  utils/gen_node_pinning.py pb-estest2 examples/dsb_sn/node-pinning-pb_estest2.yaml            # canonical
  utils/gen_node_pinning.py v-esrev2   examples/dsb_sn/node-pinning-v_esrev2.yaml --anti-affinity --no-requests
  utils/gen_node_pinning.py sb-es15    examples/dsb_sn/node-pinning-sb_es15.yaml  --one-per-node --no-requests
  ```

### ⚠ Cluster topology
Node **names/counts are experiment-specific** — always check `kubectl get nodes`.
The kubespray profiles used here produce:
- `node-0` — **tainted** control-plane (off-limits to app pods; auto-excluded by `--one-per-node`)
- `node-1` — **untainted** control-plane (schedulable, but runs an etcd member)
- `node-2`…`node-N` — pure workers

(The original testbed had 10 nodes; the current `bridges-tb-04` has 15 → 14
schedulable, which is exactly what `--one-per-node` needs.)

The canonical hot-triple mode pins its hot node to `node-1`, i.e. ~19 cores of app
load (incl. the stressed `composepost`) on a control-plane/etcd node. That's fine
for a test deploy + light seed, but **under a heavy sweep it can contend with
etcd** (the failure mode in `cloudlab_etcd_stall_rbac_playbook`) — for a real
sweep, move `node-1`'s services to a pure worker in the generated file. The
isolation modes already keep real load off the CP node (anti-affinity puts only
composepost+userid there; one-per-node only userid+tracepressure).

---

## 7. Auto-eviction behavior

With `--apply`, stage [7] detects the live DSB-SN variant from the running
`otelcol-<variant>-ctr` pods and tears it down **before** applying the new one —
so you never accidentally run two variants competing for node resources. It
removes each active variant via its own `build_*/k8s` manifests (falling back to
name-matched deletion if the build dir is gone). Non-DSB workloads
(metallb, registry, kube-system) are never touched.

---

## 8. After deploy: driving load

The seed step prints the wrk2api NodePort, e.g. `wrk2api NodePort: 35540`. Drive
load against `http://10.10.1.1:<NodePort>` (wrk2api listens on port 2000 inside
the cluster), or hit the per-node wrk2api DaemonSet pods directly. Runs should go
to `~/runs`, never inside the repo.

Re-fetch the NodePort later:
```bash
kubectl get svc wrk2api-service-<variant>-ctr -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}'
```

---

## 9. Common recipes

`utils/build_deploy_dsb.sh --help` has the full flag reference; these are the
proven configurations.

**Overhead experiments** (passthrough collectors, isolation placement, no CPU
reservations — the corrected-baseline fourway):
```bash
# vanilla baseline, anti-affinity, cpd=2 (suffix rev2 -> images *-v-esrev2-ctr):
utils/build_deploy_dsb.sh -s docker_v_es -n v_esrev2 --extra rev2 --cpd 2 --gc natural \
  --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests --apply --seed

# the three bridges, same config (build once each; the sweep driver re-applies them):
for V in pb cgpb sb; do
  utils/build_deploy_dsb.sh -s docker_${V}_es -n ${V}_esrev2 --extra rev2 --cpd 2 --gc natural \
    --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests
done

# same fourway at cpd=6 under a DISTINCT suffix (cpd is baked into the otelcol image,
# so different cpd = different suffix = separate images; cpd2 data stays untouched):
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_esrev2c6 --extra rev2c6 --cpd 6 --gc natural \
  --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests

# one-per-node variant on a >=15-node cluster (nodes auto-detected):
utils/build_deploy_dsb.sh -s docker_v_es -n v_es15 --extra 15 --cpd 2 --gc natural \
  --collector passthrough --one-per-node --wrk2api-deploy --no-pin-requests --apply --seed
```

**Shedding / controller studies** (priority processor active, hot-triple placement):
```bash
# CGPB at cpd 6, forced GC, custom controller thresholds, deploy only (no seed):
utils/build_deploy_dsb.sh -s docker_cgpb_es -n cgpb_run --cpd 6 --gc forced \
  --soft 50 --hard 70 --cp-safety 1 --apply
```

**Rebuild / iterate:**
```bash
# rebuild the collector base image too (after editing a bridge processor in contrib):
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_esrev2 --extra rev2 --cpd 2 --gc natural \
  --build-collector --apply --seed

# regenerate manifests only, reusing existing registry images (fast, no docker builds;
# ONLY valid if images for this exact suffix were pushed before):
utils/build_deploy_dsb.sh -s docker_v_es -n v_esrev2 --extra rev2 --skip-build \
  --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests

# no-deploy build: just produce build_<name>/k8s to inspect:
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_estest --cpd 2
```

---

## 10. Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| otelcol pods CrashLoopBackOff with `invalid keys: high_percentage...` | Stale config schema. Stage [2] fixes this on a fresh build; if you hand-edited config on an already-deployed build, you must `docker build`+`push` the otelcol image and `kubectl rollout restart daemonset/otelcol-<v>-ctr` (config is baked into the image, not a ConfigMap). |
| `go build` fails: `"encoding/base64" imported and not used` | Dead-import codegen. Stage [1c] (`goimports`) fixes it; ensure `goimports` installed (`go install golang.org/x/tools/cmd/goimports@latest`). |
| Pods stuck `Pending` after deploy | Pinning targets a node that doesn't exist / is full. Check `node-pinning-<name>.yaml` against `kubectl get nodes`. |
| App "collapses" (non-2xx flood) right after seed | Missing `aiohttp` (seed silently under-seeds) — `pip install aiohttp`. See `feedback_new_cluster_bringup`. |
| `kubectl` suddenly `Forbidden` for admin | Transient etcd stall, **not** this script. See `cloudlab_etcd_stall_rbac_playbook` — self-recovers; `super-admin.conf` is the break-glass. |
| Diluted/invalid run (no shedding) | Pods scattered — confirm pinning landed: `kubectl get pods -o wide | grep composepost` should show `node-1` (or your chosen hot node). |

---

## 11. Related files

- `utils/build_deploy_dsb.sh` — the driver (this guide).
- `utils/inject_perf_env.py` — Jaeger/otelcol perf env injector.
- `utils/gen_node_pinning.py` — canonical node-pinning generator.
- `utils/pin_nodes.py` — applies a pinning YAML to k8s deployments.
- `examples/dsb_sn/build_<name>/` — generated build output (docker/, k8s/, golang/).
- `examples/dsb_sn/node-pinning-<name>.yaml` — per-variant pinning.
