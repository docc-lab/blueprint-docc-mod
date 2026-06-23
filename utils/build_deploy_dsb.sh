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
# do NOT produce and that get wiped on every regeneration.
#
# Controller knobs (priorityprocessor): --soft/--hard/--cp-safety (defaults
# 50/70/1 = rev2-proven); force_gc + gc intervals are script vars near the top.
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

SPEC=""; NAME=""; CPD=""; GC=""; EXTRA=""
BUILD_COLLECTOR=0; DO_APPLY=0; DO_SEED=0; SKIP_BUILD=0
# priorityprocessor controller config (rev2-proven defaults; the wiring emits a
# STALE legacy schema -> step [2] rewrites the block to these current-schema keys).
SOFT_PCT=50; HARD_PCT=70; CP_SAFETY=1; FORCE_GC=true; GC_SOFT=1s; GC_ULTRA=0s

die(){ echo "ERROR: $*" >&2; exit 1; }
usage(){ sed -n '2,20p' "$0"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    -s) SPEC=$2; shift 2;;
    -n) NAME=$2; shift 2;;
    --cpd) CPD=$2; shift 2;;
    --gc) GC=$2; shift 2;;
    --extra) EXTRA=$2; shift 2;;
    --build-collector) BUILD_COLLECTOR=1; shift;;
    --soft) SOFT_PCT=$2; shift 2;;
    --hard) HARD_PCT=$2; shift 2;;
    --cp-safety) CP_SAFETY=$2; shift 2;;
    --skip-build) SKIP_BUILD=1; shift;;
    --apply) DO_APPLY=1; shift;;
    --seed) DO_SEED=1; shift;;
    -h|--help) usage;;
    *) die "unknown arg: $1 (try -h)";;
  esac
done
[ -n "$SPEC" ] && [ -n "$NAME" ] || usage
[ -n "$GC" ] && [ "$GC" != natural ] && [ "$GC" != forced ] && die "--gc must be natural|forced"

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

# 2. otelcol config.yaml: normalize the priority processor block + set cpd -------
#    The wiring config-gen is pinned to an OLDER priorityprocessor and emits the
#    legacy {high,mid,low}_percentage schema, which the freshly-built collector
#    image rejects ("invalid keys") -> CrashLoopBackOff. Rewrite the block to the
#    current schema (soft/hard/cp_safety_factor/force_gc/gc intervals). cpd lives
#    under receivers.configdiscovery.config_map, NOT the priority processor.
OTELCFG=$BUILD/docker/otelcol_${SUF}_ctr/config.yaml
[ -f "$OTELCFG" ] || die "otelcol config.yaml not found at $OTELCFG"
echo "=== [2] normalize priority config (soft=$SOFT_PCT hard=$HARD_PCT cp_safety=$CP_SAFETY) + cpd=${CPD:-unchanged} ==="
python3 - "$OTELCFG" "${CPD:-}" "$SOFT_PCT" "$HARD_PCT" "$CP_SAFETY" "$FORCE_GC" "$GC_SOFT" "$GC_ULTRA" <<'PY' || die "priority-config normalize failed"
import sys, yaml
path, cpd, soft, hard, safety, force_gc, gc_soft, gc_ultra = sys.argv[1:9]
with open(path) as f: cfg = yaml.safe_load(f)
pr = (cfg.get('processors') or {}).get('priority')
if isinstance(pr, dict):
    cfg['processors']['priority'] = {
        'check_interval': pr.get('check_interval', '100ms'),
        'soft_percentage': int(soft),
        'hard_percentage': int(hard),
        'cp_safety_factor': float(safety) if '.' in safety else int(safety),
        'force_gc': force_gc.lower() == 'true',
        'gc_soft_interval': gc_soft,
        'gc_ultrasoft_interval': gc_ultra,
    }
    print("  priority ->", cfg['processors']['priority'])
else:
    print("  WARN: no 'priority' processor in config; left untouched")
if cpd:
    cm = ((cfg.get('receivers') or {}).get('configdiscovery') or {}).get('config_map')
    if isinstance(cm, dict) and 'cpd' in cm:
        cm['cpd'] = int(cpd); print("  cpd ->", cm['cpd'])
    else:
        sys.exit("ERROR: --cpd given but receivers.configdiscovery.config_map.cpd not found")
with open(path, 'w') as f: yaml.safe_dump(cfg, f, default_flow_style=False, sort_keys=False)
PY

# 3. env map into compose (.local.env -> docker/.env) ---------------------------
echo "=== [3] env into compose ==="
cp "$BUILD/.local.env" "$BUILD/docker/.env" || die "no .local.env"
set -a; . "$BUILD/docker/.env"; set +a      # export the address map for compose interpolation

# 4. d2k8s: build/push images + emit k8s; otelcol=DaemonSet+Local, wrk2api=DaemonSet (no Local)
SKIPF=""; [ "$SKIP_BUILD" = 1 ] && SKIPF="--skip-build"
echo "=== [4] d2k8s (otelcol+wrk2api daemonsets; wrk2api no-local-policy)${SKIPF:+ [skip-build]} ==="
python3 "$REPO/d2k8s/d2k8s.py" \
  --registry "$REGISTRY" \
  --daemon-services "${OTELCOL_SVC},${WRK_SVC}" \
  --no-local-policy "${WRK_SVC}" \
  $SKIPF \
  "$BUILD/docker/docker-compose.yml" "$K8S" || die "d2k8s failed"
[ -d "$K8S" ] || die "d2k8s produced no k8s dir"

# 5. inject manual perf env (Jaeger ES knobs + otelcol GOMEMLIMIT/resources [+GC])
echo "=== [5] inject Jaeger/otelcol perf env ${GC:+(+gc=$GC)} ==="
python3 "$UTILS/inject_perf_env.py" "$K8S" "$VARIANT" ${GC:+--gc "$GC"} || die "perf-env injection failed"

# 6. node pinning (d2k8s strips it -> must re-apply each regeneration) -----------
NODEPIN=$DSB/node-pinning-${NAME}.yaml
if [ -f "$NODEPIN" ]; then
  echo "=== [6] node pinning from $NODEPIN ==="
  python3 "$UTILS/pin_nodes.py" "$NODEPIN" "$K8S" || die "pin_nodes failed"
else
  echo "   WARN: no $NODEPIN -> pods will deploy UNPINNED. Create one to pin per-node CPU/mem."
fi

echo "=== BUILD COMPLETE -> $K8S ==="
[ "$DO_APPLY" = 1 ] || { echo "(skip apply; run with --apply to deploy)"; exit 0; }

# 7. teardown + apply + wait Running --------------------------------------------
echo "=== [7] teardown + apply ==="
kubectl delete -f "$K8S" --ignore-not-found=true --wait=true
kubectl apply  -f "$K8S" || die "kubectl apply failed"
echo "=== wait for $VARIANT pods Running ==="
for i in $(seq 1 120); do
  total=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v' | wc -l)
  run=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v && $3=="Running"' | wc -l)
  [ "$total" -gt 0 ] && [ "$run" = "$total" ] && { echo "all $total Running"; break; }
  echo "[$i/120] $run/$total Running"; sleep 5
done

# 8. seed (NodePort-patch wrk2api, then init_social_graph from the DSB seed dir) -
[ "$DO_SEED" = 1 ] || { echo "(skip seed; run with --seed)"; exit 0; }
echo "=== [8] seed ==="
kubectl patch service "$WRK_SVC" -p '{"spec":{"type":"NodePort"}}'  # NodePort works on a DaemonSet-backed svc
NP=""
for i in $(seq 1 15); do
  NP=$(kubectl get svc "$WRK_SVC" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}' 2>/dev/null)
  [ -n "$NP" ] && break; sleep 1
done
[ -n "$NP" ] || die "could not get wrk2api NodePort"
echo "   wrk2api NodePort: $NP"
sleep 15
( cd "$SEED_DIR" && python3 "$SEED_PY" --ip 10.10.1.1 --port "$NP" 2>&1 | tail -5 ) \
  || die "seed failed (need: pip install aiohttp; CWD must be $SEED_DIR)"
echo "=== DONE: $VARIANT deployed + seeded. Drive load via wrk2api NodePort $NP, or per-node wrk2api daemonset pods. ==="
