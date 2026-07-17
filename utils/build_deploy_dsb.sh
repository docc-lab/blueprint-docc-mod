#!/bin/bash
# Turnkey compile (+optional deploy +seed) for a DSB-SN variant.
#
#   build_deploy_dsb.sh -s <wiring_spec> -n <build_name> [options]
#
# Reconstructs the full pipeline: (collector image) -> Blueprint wiring into a
# custom build_<name>/ -> goimports (strip dead-import codegen) -> normalize the
# otelcol priority-processor config to the CURRENT schema -> env-into-compose ->
# d2k8s (otelcol DaemonSet+Local, wrk2api DaemonSet WITHOUT Local) -> inject
# Jaeger/otelcol perf env -> node-pin -> (apply) -> (seed). The goimports, config-
# normalize, perf-env, and pinning steps re-apply fixes that wiring/d2k8s/kompose
# do NOT produce and that get wiped on every regeneration. --apply first EVICTS
# whatever DSB-SN variant is currently live (auto-detected from running pods), so
# only one variant ever occupies the shared-pressure cluster.
#
# Controller knobs (priorityprocessor): --soft/--hard/--cp-safety (defaults
# 50/70/1 = rev2-proven); force_gc + gc intervals are script vars near the top.
# --no-pin-requests: node-pin places services on nodes but sets NO CPU requests
#   (just nodeSelector co-location) — avoids over-reserving cores. Applies to both
#   the generated pinning file and the apply step (existing files' requests ignored).
# --collector <mode>: otelcol pipeline mode. Currently only `passthrough` = strip
#   the priority processor AND memory_limiter, leaving receivers -> batch -> exporters.
#   USE FOR OVERHEAD EXPERIMENTS so the collector adds no shedding/limiting of its own
#   (otherwise bridge=priority vs vanilla=memory_limiter confounds the comparison, and
#   priority-refused batches + SDK retry can cause a backpressure runaway). Default
#   (unset) keeps the wiring's processor (priority for bridges) for shedding studies.
#
# Examples:
#   build_deploy_dsb.sh -s docker_pb_es -n pb_esrev2 --cpd 2 --gc natural --apply --seed
#   build_deploy_dsb.sh -s docker_pb_es -n pb_sweep  --cpd 6 --soft 50 --hard 70 --cp-safety 1 --apply
#   build_deploy_dsb.sh -s docker_v_es  -n v_esrev0  --build-collector --apply
set -uo pipefail

REPO=/users/tomislav/blueprint-docc-mod
DSB=$REPO/examples/dsb_sn
UTILS=$REPO/utils
REGISTRY=10.10.1.1:30000
COLLECTOR_SRC=/users/tomislav/opentelemetry-collector-contrib
SEED_DIR=/users/tomislav/DeathStarBench/socialNetwork
SEED_PY=$DSB/scripts/init_social_graph.py

SPEC=""; NAME=""; CPD=""; GC=""; EXTRA=""; COLLECTOR=""
BUILD_COLLECTOR=0; DO_APPLY=0; DO_SEED=0; DO_NOSEED=0; SKIP_BUILD=0; NOPIN_REQ=0; WRK_DAEMON=1; ANTI=0; ONEPER=0
# priorityprocessor controller config (rev2-proven defaults; the wiring emits a
# STALE legacy schema -> step [2] rewrites the block to these current-schema keys).
SOFT_PCT=50; HARD_PCT=70; CP_SAFETY=1; FORCE_GC=true; GC_SOFT=1s; GC_ULTRA=0s

die(){ echo "ERROR: $*" >&2; exit 1; }
usage(){
cat <<'EOHELP'
build_deploy_dsb.sh — turnkey compile (+optional deploy +seed) of a DSB-SN bridge variant

USAGE
  build_deploy_dsb.sh -s <wiring_spec> -n <build_name> [options]

REQUIRED
  -s <spec>            Wiring spec = bridge kind. One of:
                         docker_pb_es    Path Bridge (PB)
                         docker_cgpb_es  Call-Graph Path Bridge (CGPB)
                         docker_sb_es    Sequence Bridge (SB)
                         docker_v_es     vanilla (true no-bridge baseline)
                         docker_rc_es    random-checkpoint control
                       Also selects the per-variant OT wrapper template at codegen
                       time (exported as OT_BRIDGE) and the runtime BRIDGE_KIND.
  -n <name>            Build dir name -> examples/dsb_sn/build_<name>/

BUILD OPTIONS
  --cpd <N>            Checkpoint distance, baked into the otelcol image config.
  --gc natural|forced  App-pod GC: natural = GOGC=100 + interval off;
                       forced = GOGC=off + forced GC 10x/s (deterministic cadence).
  --extra <str>        Append to the kompose suffix (e.g. --extra rev2 => v-esrev2).
                       Distinct suffix = distinct image names in the registry.
  --build-collector    Rebuild + push the otelcontribcol base image first.
  --skip-build         Skip d2k8s image builds; reuse existing registry images
                       (valid only if images for this exact suffix already exist).

COLLECTOR PIPELINE
  --collector passthrough
                       Strip priority processor AND memory_limiter ->
                       receivers -> batch -> exporters. USE FOR OVERHEAD RUNS
                       (identical, non-interfering collectors for all variants).
                       Unset = wiring default (priority processor; for shedding studies).
  --soft <pct>         Priority controller soft threshold %        (default 50)
  --hard <pct>         Priority controller hard (force-GC) %       (default 70)
  --cp-safety <f>      CP-vs-LP shedding dial (higher=protect CP)  (default 1)

PLACEMENT (stage [6]; generated only if node-pinning-<name>.yaml is absent)
  (default)            Canonical rev2 hot-triple: composepost+hometimeline(+cache)
                       co-located on the hot node (asymmetric-pressure design).
  --anti-affinity      No two traced services with a call edge share a node
                       (static 9-node layout). Un-masks per-hop bridge overhead.
  --one-per-node       Every named service on its OWN node. Nodes AUTO-DETECTED
                       from `kubectl get nodes` (tainted control-plane excluded);
                       needs >= 14 schedulable nodes. Caches/DBs ride with their
                       service; near-idle userid+tracepressure share the CP node.
  --no-pin-requests    Placement-only: nodeSelector but NO CPU requests/limits
                       (don't over-reserve cores). Applies to generated AND
                       pre-existing pinning files.
  --wrk2api-deploy     wrk2api as a single pinnable Deployment instead of the
                       per-node DaemonSet. REQUIRED for --anti-affinity /
                       --one-per-node to also isolate the frontend hop.

DEPLOY
  --apply              Evict whatever DSB-SN variant is live (auto-detected from
                       otelcol-<v>-ctr pods), apply this build, wait for Running.
  --seed               After apply: NodePort-patch wrk2api + seed the social graph
  --noseed             After apply: NodePort-patch wrk2api only (skip DB seed; for zero-work variants)
                       (needs DeathStarBench/socialNetwork + venv with aiohttp).

MISC
  -h, --help           This help.

EXAMPLES
  # overhead-experiment fourway builds (passthrough, anti-affinity, no requests):
  build_deploy_dsb.sh -s docker_v_es  -n v_esrev2  --extra rev2 --cpd 2 --gc natural \
      --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests --apply --seed
  # same at cpd=6 under a DISTINCT suffix (separate images, cpd baked in):
  build_deploy_dsb.sh -s docker_pb_es -n pb_esrev2c6 --extra rev2c6 --cpd 6 --gc natural \
      --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests
  # one-per-node isolation on a >=15-node cluster:
  build_deploy_dsb.sh -s docker_sb_es -n sb_es15 --extra 15 --cpd 2 --gc natural \
      --collector passthrough --one-per-node --wrk2api-deploy --no-pin-requests --apply --seed
  # shedding study (priority processor active, custom thresholds), hot-triple placement:
  build_deploy_dsb.sh -s docker_cgpb_es -n cgpb_shed --cpd 6 --gc forced \
      --soft 50 --hard 70 --cp-safety 1 --apply
  # regenerate manifests only, reuse existing images (fast; no docker builds):
  build_deploy_dsb.sh -s docker_v_es -n v_esrev2 --extra rev2 --skip-build \
      --collector passthrough --anti-affinity --wrk2api-deploy --no-pin-requests
  # rebuild the collector base image too (after editing a processor in contrib):
  build_deploy_dsb.sh -s docker_pb_es -n pb_esrev2 --extra rev2 --cpd 2 --build-collector --apply
EOHELP
exit "${1:-1}"
}

# Help must work before ANY environment checks (docker guard below re-execs).
for a in "$@"; do case "$a" in -h|--help) usage 0;; esac; done

# Docker access guard: d2k8s + image builds need the docker socket (root:docker).
# On CloudLab a session can predate `usermod -aG docker` (group not active yet),
# so plain `docker` is denied. If the user IS a docker group member, re-exec the
# whole script under `sg docker` (activates the group without re-login). Idempotent:
# after re-exec `docker info` succeeds, so it runs once.
if ! docker info >/dev/null 2>&1; then
  ME=$(id -un)
  if getent group docker | grep -qw "$ME"; then
    echo "=== [docker] not active in this shell -> re-exec under 'sg docker' ==="
    exec sg docker -c "$(printf '%q ' "$0" "$@")"
  else
    die "docker not accessible and '$ME' is not in the 'docker' group. Fix: sudo usermod -aG docker $ME (then re-login), or run utils/setup_environment.sh"
  fi
fi

while [ $# -gt 0 ]; do
  case "$1" in
    -s) SPEC=$2; shift 2;;
    -n) NAME=$2; shift 2;;
    --cpd) CPD=$2; shift 2;;
    --gc) GC=$2; shift 2;;
    --extra) EXTRA=$2; shift 2;;
    --collector) COLLECTOR=$2; shift 2;;
    --build-collector) BUILD_COLLECTOR=1; shift;;
    --soft) SOFT_PCT=$2; shift 2;;
    --hard) HARD_PCT=$2; shift 2;;
    --cp-safety) CP_SAFETY=$2; shift 2;;
    --no-pin-requests) NOPIN_REQ=1; shift;;
    --wrk2api-deploy) WRK_DAEMON=0; shift;;       # wrk2api as a Deployment (pinnable), NOT a per-node DaemonSet
    --anti-affinity) ANTI=1; shift;;              # placement: no co-located traced call edges (implies pin wrk2api)
    --one-per-node) ONEPER=1; shift;;             # placement: every named service on its own auto-detected node
    --skip-build) SKIP_BUILD=1; shift;;
    --apply) DO_APPLY=1; shift;;
    --seed) DO_SEED=1; shift;;
    --noseed) DO_NOSEED=1; shift;;
    -h|--help) usage 0;;
    *) die "unknown arg: $1 (try -h)";;
  esac
done
[ -n "$SPEC" ] && [ -n "$NAME" ] || usage
[ -n "$GC" ] && [ "$GC" != natural ] && [ "$GC" != forced ] && die "--gc must be natural|forced"
[ -n "$COLLECTOR" ] && [ "$COLLECTOR" != passthrough ] && die "--collector must be: passthrough (more modes TBD)"

VARIANT=${NAME//_/-}                       # provisional; re-derived from generated compose below
BUILD=$DSB/build_$NAME
K8S=$BUILD/k8s
echo "=== build_deploy_dsb: spec=$SPEC name=$NAME (provisional variant=$VARIANT) cpd=${CPD:-default} gc=${GC:-default} ==="

# 0. collector base image (bridge processors live here) -------------------------
if [ "$BUILD_COLLECTOR" = 1 ]; then
  echo "=== [0] build+push otelcontribcol ==="
  ( cd "$COLLECTOR_SRC" && ./build-and-push.sh "$REGISTRY" ) || die "collector image build failed"
fi

# 1. Blueprint wiring -> build_<name>/ ------------------------------------------
echo "=== [1] wiring: go run wiring/main.go -w $SPEC -o build_$NAME ==="
cd "$DSB" || die "no $DSB"
rm -rf "$BUILD"
EXTRA_ARG=""; [ -n "$EXTRA" ] && EXTRA_ARG="-extra $EXTRA"
# Select the OT client/server wrapper template per bridge kind (read by the
# opentelemetry plugin's codegen). Derived from the spec: docker_<kind>_es -> <kind>.
# vanilla => plain spans, no bridge data; pb/cgpb/sb => their bridge wrappers.
OT_BRIDGE=$(echo "$SPEC" | sed -E 's/^docker_//; s/_nw$//; s/_es$//'); export OT_BRIDGE
echo "   OT_BRIDGE=$OT_BRIDGE (per-variant OT wrapper template)"
go run wiring/main.go -w "$SPEC" $EXTRA_ARG -o "build_$NAME" || die "wiring failed (watch for dead-import codegen errors in generated golang/)"
[ -d "$BUILD/docker" ] || die "wiring produced no build_$NAME/docker"
echo "   NOTE: confirm runtime/plugins/otelcol/trace.go / BRIDGE_KIND matches the intended bridge variant (compile-time selection site)."

# 1b. resolve the AUTHORITATIVE kompose suffix from the generated compose --------
#     (the real service suffix comes from the wiring spec/-extra, not the -o name;
#      a mismatch would make --daemon-services silently no-op.)
COMPOSE=$BUILD/docker/docker-compose.yml
[ -f "$COMPOSE" ] || die "no $COMPOSE"
SUF=$(grep -oE 'otelcol_[a-z0-9_]+_ctr' "$COMPOSE" | head -1 | sed -E 's/^otelcol_//; s/_ctr$//')
[ -n "$SUF" ] || die "could not detect otelcol service suffix from $COMPOSE"
DVAR=${SUF//_/-}
if [ "$DVAR" != "$VARIANT" ]; then
  echo "   NOTE: wiring produced suffix '$DVAR' (build name implied '$VARIANT') -> using '$DVAR' for all service names."
  VARIANT=$DVAR
fi
OTELCOL_SVC=otelcol-${VARIANT}-ctr
WRK_SVC=wrk2api-service-${VARIANT}-ctr
grep -qE "wrk2api_service_${SUF}_ctr|wrk2api-service-${VARIANT}-ctr" "$COMPOSE" || \
  echo "   WARN: wrk2api service not found by expected name in compose; --daemon-services may no-op for it."

# 1c. fix dead-import codegen ---------------------------------------------------
#     The OT wrapper generator emits unused imports (e.g. encoding/base64,
#     encoding/binary) for variants that don't use them (PB/vanilla) -> `go build`
#     fails on every service. goimports strips genuinely-unused imports per file.
echo "=== [1c] goimports -w (strip dead-import codegen) ==="
GOIMPORTS="$(go env GOPATH)/bin/goimports"
[ -x "$GOIMPORTS" ] || { echo "   installing goimports..."; go install golang.org/x/tools/cmd/goimports@latest || die "goimports install failed"; }
"$GOIMPORTS" -w "$BUILD/docker" || die "goimports failed"

# 2. otelcol config.yaml: configure the collector pipeline + set cpd -------------
#    DEFAULT: normalize the priority processor to the CURRENT schema (the wiring
#    emits a stale legacy schema the freshly-built image rejects -> CrashLoopBackOff).
#    `--collector passthrough`: strip BOTH priority and memory_limiter, leaving a
#    clean passthrough pipeline (receivers -> batch -> exporters). Use this for
#    OVERHEAD experiments where the collector must impose NO shedding/limiting of its
#    own (otherwise bridge=priority vs vanilla=memory_limiter confounds the result
#    AND priority-refused batches + SDK retry can cause a backpressure runaway).
#    cpd lives under receivers.configdiscovery.config_map (set in both modes).
OTELCFG=$BUILD/docker/otelcol_${SUF}_ctr/config.yaml
[ -f "$OTELCFG" ] || die "otelcol config.yaml not found at $OTELCFG"
echo "=== [2] collector=${COLLECTOR:-default} (priority-normalize) + cpd=${CPD:-unchanged} ==="
python3 - "$OTELCFG" "${CPD:-}" "$SOFT_PCT" "$HARD_PCT" "$CP_SAFETY" "$FORCE_GC" "$GC_SOFT" "$GC_ULTRA" "${COLLECTOR:-}" <<'PY' || die "collector config failed"
import sys, yaml
path, cpd, soft, hard, safety, force_gc, gc_soft, gc_ultra, collector = sys.argv[1:10]
with open(path) as f: cfg = yaml.safe_load(f)
procs = cfg.setdefault('processors', {})
if collector == 'passthrough':
    procs.pop('priority', None)
    procs.pop('memory_limiter', None)
    if not isinstance(procs.get('batch'), dict): procs['batch'] = {}
    tr = ((cfg.get('service') or {}).get('pipelines') or {}).get('traces')
    if isinstance(tr, dict): tr['processors'] = ['batch']
    print("  collector=passthrough -> traces processors:[batch]; priority + memory_limiter removed")
else:
    pr = procs.get('priority')
    if isinstance(pr, dict):
        procs['priority'] = {
            'check_interval': pr.get('check_interval', '100ms'),
            'soft_percentage': int(soft),
            'hard_percentage': int(hard),
            'cp_safety_factor': float(safety) if '.' in safety else int(safety),
            'force_gc': force_gc.lower() == 'true',
            'gc_soft_interval': gc_soft,
            'gc_ultrasoft_interval': gc_ultra,
        }
        print("  priority ->", procs['priority'])
    else:
        print("  WARN: no 'priority' processor in config; left untouched")
if cpd:
    cm = ((cfg.get('receivers') or {}).get('configdiscovery') or {}).get('config_map')
    if isinstance(cm, dict) and 'cpd' in cm:
        cm['cpd'] = int(cpd); print("  cpd ->", cm['cpd'])
    else:
        print("  note: --cpd given but no receivers.configdiscovery.config_map.cpd "
              "in this config (e.g. vanilla / no-bridge variant) — skipping cpd set")
with open(path, 'w') as f: yaml.safe_dump(cfg, f, default_flow_style=False, sort_keys=False)
PY

# 3. env map into compose (.local.env -> docker/.env) ---------------------------
echo "=== [3] env into compose ==="
cp "$BUILD/.local.env" "$BUILD/docker/.env" || die "no .local.env"
set -a; . "$BUILD/docker/.env"; set +a      # export the address map for compose interpolation

# 4. d2k8s: build/push images + emit k8s; otelcol=DaemonSet+Local, wrk2api=DaemonSet (no Local)
SKIPF=""; [ "$SKIP_BUILD" = 1 ] && SKIPF="--skip-build"
echo "=== [4] d2k8s (otelcol+wrk2api daemonsets; wrk2api no-local-policy)${SKIPF:+ [skip-build]} ==="
# wrk2api: DaemonSet (default) OR a single Deployment (--wrk2api-deploy) so it can
# be node-pinned and anti-affined from its backends (un-masks the gateway->svc hop).
if [ "$WRK_DAEMON" = 1 ]; then
  DAEMON_SVCS="${OTELCOL_SVC},${WRK_SVC}"; NOLOCAL=(--no-local-policy "${WRK_SVC}")
else
  DAEMON_SVCS="${OTELCOL_SVC}";            NOLOCAL=()
  echo "   wrk2api as Deployment (not DaemonSet)"
fi
python3 "$REPO/d2k8s/d2k8s.py" \
  --registry "$REGISTRY" \
  --daemon-services "${DAEMON_SVCS}" \
  "${NOLOCAL[@]}" \
  $SKIPF \
  "$BUILD/docker/docker-compose.yml" "$K8S" || die "d2k8s failed"
[ -d "$K8S" ] || die "d2k8s produced no k8s dir"

# 5. inject manual perf env (Jaeger ES knobs + otelcol GOMEMLIMIT/resources [+GC])
echo "=== [5] inject Jaeger/otelcol perf env ${GC:+(+gc=$GC)} ==="
python3 "$UTILS/inject_perf_env.py" "$K8S" "$VARIANT" ${GC:+--gc "$GC"} || die "perf-env injection failed"

# 6. node pinning (d2k8s strips it -> must re-apply each regeneration) -----------
#    If no pinning file exists for this build, generate the canonical rev2
#    asymmetric-pressure placement (hot node-1 = composepost+hometimeline) stamped
#    with this variant's suffix. An existing file is respected (hand edits kept).
NODEPIN=$DSB/node-pinning-${NAME}.yaml
NOREQ=""; [ "$NOPIN_REQ" = 1 ] && NOREQ="--no-requests"   # placement-only (no CPU requests)
AAFLAG=""; [ "$ANTI" = 1 ] && AAFLAG="--anti-affinity"    # no co-located traced call edges (pins wrk2api too)
[ "$ONEPER" = 1 ] && AAFLAG="--one-per-node"              # every named service isolated (auto-detected nodes)
if [ ! -f "$NODEPIN" ]; then
  echo "=== [6] no $NODEPIN -> generating placement (${AAFLAG:-canonical}) for '$VARIANT'${NOREQ:+ (placement-only)} ==="
  python3 "$UTILS/gen_node_pinning.py" "$VARIANT" "$NODEPIN" $NOREQ $AAFLAG || die "pinning generation failed"
fi
echo "=== [6] node pinning from $NODEPIN${NOREQ:+ (placement-only: strip requests)} ==="
python3 "$UTILS/pin_nodes.py" "$NODEPIN" "$K8S" $NOREQ || die "pin_nodes failed"

echo "=== BUILD COMPLETE -> $K8S ==="
[ "$DO_APPLY" = 1 ] || { echo "(skip apply; run with --apply to deploy)"; exit 0; }

# 7. teardown ACTIVE variant(s) + apply + wait Running --------------------------
#    --apply evicts whatever DSB-SN variant is currently live (detected from the
#    running otelcol-<variant>-ctr pods — otelcol is unique, one per variant), not
#    just this build: only one variant should occupy the shared-pressure cluster
#    at a time. Each active variant is removed via its own build_*/k8s manifests
#    when the dir is locatable, else by name match on the live objects.
echo "=== [7] teardown active variant(s) + apply ==="
ACTIVE=$(kubectl get pods -o name 2>/dev/null | sed -nE 's#^pod/otelcol-(.+)-ctr-.*$#\1#p' | sort -u)
if [ -n "$ACTIVE" ]; then
  for v in $ACTIVE; do
    DIR=""
    for d in "$DSB"/build_*/k8s; do
      [ -d "$d" ] && ls "$d"/otelcol-"$v"-ctr-* >/dev/null 2>&1 && { DIR="$d"; break; }
    done
    if [ -n "$DIR" ]; then
      echo "   teardown active variant '$v' via $DIR"
      kubectl delete -f "$DIR" --ignore-not-found=true --wait=true
    else
      echo "   teardown active variant '$v' by name (build dir not found)"
      for kind in deployment daemonset statefulset service configmap; do
        kubectl get "$kind" -o name 2>/dev/null | grep -E -- "-${v}-ctr$" \
          | xargs -r kubectl delete --ignore-not-found=true --wait=true
      done
    fi
  done
else
  echo "   no active DSB-SN variant detected (clean apply)"
fi
kubectl delete -f "$K8S" --ignore-not-found=true --wait=true >/dev/null 2>&1  # same-variant safety
kubectl apply  -f "$K8S" || die "kubectl apply failed"
echo "=== wait for $VARIANT pods Running ==="
for i in $(seq 1 120); do
  total=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v' | wc -l)
  run=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v && $3=="Running"' | wc -l)
  [ "$total" -gt 0 ] && [ "$run" = "$total" ] && { echo "all $total Running"; break; }
  echo "[$i/120] $run/$total Running"; sleep 5
done

# 8. NodePort-patch wrk2api (always, for --seed or --noseed), then init_social_graph
#    ONLY for --seed. --noseed exists for zero-work variants that have no DBs to
#    seed but still need the wrk2api NodePort for load generation.
if [ "$DO_SEED" != 1 ] && [ "$DO_NOSEED" != 1 ]; then echo "(skip seed; run with --seed or --noseed)"; exit 0; fi
echo "=== [8] wrk2api NodePort patch ==="
kubectl patch service "$WRK_SVC" -p '{"spec":{"type":"NodePort"}}'  # NodePort works on a DaemonSet-backed svc
NP=""
for i in $(seq 1 15); do
  NP=$(kubectl get svc "$WRK_SVC" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}' 2>/dev/null)
  [ -n "$NP" ] && break; sleep 1
done
[ -n "$NP" ] || die "could not get wrk2api NodePort"
echo "   wrk2api NodePort: $NP"
if [ "$DO_SEED" = 1 ]; then
  sleep 15
  ( cd "$SEED_DIR" && python3 "$SEED_PY" --ip 10.10.1.1 --port "$NP" 2>&1 | tail -5 ) \
    || die "seed failed (need: pip install aiohttp; CWD must be $SEED_DIR)"
  echo "=== DONE: $VARIANT deployed + seeded. Drive load via wrk2api NodePort $NP, or per-node wrk2api daemonset pods. ==="
else
  echo "=== DONE: $VARIANT deployed + NodePort ready (--noseed; DB init skipped). NodePort $NP ==="
fi
