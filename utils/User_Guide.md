# DSB-SN Build & Deploy — User Guide

`utils/build_deploy_dsb.sh` is a turnkey driver that takes a DeathStarBench
Social Network (DSB-SN) bridge variant from Blueprint source all the way to a
**seeded, load-ready Kubernetes deployment** in a single command.

It reconstructs the *entire* pipeline and automatically re-applies the manual
fixes that Blueprint wiring / d2k8s / kompose do **not** produce and that get
wiped on every regeneration — so a fresh build "just works" instead of
crash-looping or silently producing an invalid run.

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
| `--build-collector` | Rebuild + push the `otelcontribcol` base image first. | off |
| `--skip-build` | Skip d2k8s image builds (reuse existing images). | off |
| `--apply` | Deploy: evict the live variant, apply, wait for Running. | off |
| `--seed` | After apply, NodePort-patch wrk2api and seed the social graph. | off |
| `-h`, `--help` | Print usage. | — |

> `force_gc`, `gc_soft_interval`, `gc_ultrasoft_interval` for the collector
> controller are script variables near the top of the file (defaults
> `true` / `1s` / `0s` = the rev2-proven config). Edit there if you need them.

---

## 5. What it does (the 8 stages)

| Stage | Action | Auto-fix it applies |
|---|---|---|
| **[0]** | (`--build-collector`) build+push `otelcontribcol`. | — |
| **[1]** | `go run wiring/main.go -w <spec> -o build_<name>`. | — |
| **[1b]** | Detect the real kompose suffix from the generated compose. | prevents `--daemon-services` silently no-op'ing |
| **[1c]** | `goimports -w` over generated Go. | **strips dead-import codegen** (unused `encoding/base64`/`binary`) that breaks `go build` |
| **[2]** | Normalize the otelcol `priority` processor config + set `cpd`. | **rewrites the STALE `high/mid/low_percentage` schema** the wiring emits to the current `soft/hard/cp_safety_factor/...` the collector image expects (otherwise CrashLoopBackOff) |
| **[3]** | Copy `.local.env` → `docker/.env` (address map for compose). | — |
| **[4]** | d2k8s: build/push images, emit k8s; otelcol+wrk2api as DaemonSets, wrk2api **without** `InternalTrafficPolicy: Local`. | DaemonSet + traffic-policy wiring |
| **[5]** | `inject_perf_env.py`. | **re-adds** Jaeger ES-bulk tuning (7 vars) + otelcol GOMEMLIMIT/resources + optional app GC mode |
| **[6]** | Node pinning: **auto-generate** `node-pinning-<name>.yaml` if absent (`gen_node_pinning.py`), then `pin_nodes.py`. | **re-applies pinning** d2k8s strips |
| **[7]** | **Auto-evict** the currently-live variant (detected from `otelcol-<v>-ctr` pods), then `kubectl apply` + wait for Running. | only one variant on the cluster at a time |
| **[8]** | (`--seed`) NodePort-patch wrk2api + `init_social_graph.py`. | — |

---

## 6. Node pinning

The asymmetric-pressure experiment design requires the **hot triple**
(`composepost` + `hometimeline` + `hometimeline-cache`) co-located on a single
node so that node's otelcol absorbs the pressure; everything else is spread.

- **Auto-generation**: if no `node-pinning-<name>.yaml` exists, stage [6]
  generates the canonical rev2 placement for the variant via
  `utils/gen_node_pinning.py` (24 services across `node-1`…`node-9`, hot node =
  `node-1`).
- **Custom pinning**: an existing `node-pinning-<name>.yaml` is **respected** —
  hand-edit it (or generate then tweak) and it won't be overwritten.
- **Generate manually** to inspect/edit before deploying:
  ```bash
  utils/gen_node_pinning.py pb-estest2 examples/dsb_sn/node-pinning-pb_estest2.yaml
  ```

### ⚠ Cluster topology caveat
This cluster has 10 nodes, `node-0`…`node-9`:
- `node-0` — tainted control-plane (off-limits to app pods)
- `node-1` — **untainted** control-plane (also runs an etcd member)
- `node-2`…`node-9` — pure workers

The canonical hot node is `node-1`, so the default places ~19 cores of app load
(incl. the stressed `composepost`) on a control-plane/etcd node. That's fine for
a test deploy + light seed, but **under a heavy load sweep it can contend with
etcd** (the failure mode in `cloudlab_etcd_stall_rbac_playbook`). For a real
sweep, edit the generated pinning file to move `node-1`'s services to a pure
worker (e.g. `node-2`).

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

```bash
# CGPB at cpd 6, forced GC, custom controller thresholds, deploy only (no seed):
utils/build_deploy_dsb.sh -s docker_cgpb_es -n cgpb_run --cpd 6 --gc forced \
  --soft 50 --hard 70 --cp-safety 1 --apply

# Rebuild the collector base image too (after editing a bridge processor):
utils/build_deploy_dsb.sh -s docker_pb_es -n pb_run --cpd 2 --gc natural \
  --build-collector --apply --seed

# Vanilla baseline:
utils/build_deploy_dsb.sh -s docker_v_es -n v_run --gc natural --apply --seed
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
