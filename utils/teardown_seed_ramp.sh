#!/bin/bash
# Wrapper: teardown -> apply -> NodePort patch -> seed -> ramp_with_snapshots.sh
#
# Usage: teardown_seed_ramp.sh <build_dir> <variant> <out_dir> [BREAK_SECS]
#
#   build_dir   the directory name under examples/dsb_sn/ (e.g. sb_esrev0 or vesrev0)
#   variant     the kompose-service-suffix (e.g. sb-esrev0 or v-esrev0)
#   out_dir     where to write snapshots.tsv, ramp.log, summary.tsv
#   BREAK_SECS  optional pause between ramp steps (default 0 = continuous)
#
# Examples:
#   teardown_seed_ramp.sh sb_esrev0 sb-esrev0 /tmp/ramp_sb_3tier_$(date +%H%M%S)
#   teardown_seed_ramp.sh vesrev0   v-esrev0  /tmp/ramp_vanilla_$(date +%H%M%S)

# Intentionally NO `set -e` / `pipefail` — kubectl-top hiccups and tail
# pipelines that SIGPIPE shouldn't kill the wrapper mid-run.

BUILD_DIR=$1
VARIANT=$2
OUT_DIR=$3
BREAK_SECS=${4:-0}

if [ -z "$BUILD_DIR" ] || [ -z "$VARIANT" ] || [ -z "$OUT_DIR" ]; then
  echo "Usage: $0 <build_dir> <variant> <out_dir> [BREAK_SECS]" >&2
  echo "  e.g.: $0 sb_esrev0 sb-esrev0 /tmp/ramp_3tier_$(date +%H%M%S)" >&2
  echo "  e.g.: $0 vesrev0  v-esrev0  /tmp/ramp_vanilla_$(date +%H%M%S)" >&2
  exit 1
fi

K8S=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_${BUILD_DIR}/k8s
if [ ! -d "$K8S" ]; then
  echo "ERROR: $K8S does not exist" >&2; exit 1
fi
mkdir -p "$OUT_DIR"

echo "=== Teardown $VARIANT ($(date +%H:%M:%S)) ==="
kubectl delete -f "$K8S" --ignore-not-found=true --wait=true

echo "=== Apply $VARIANT ($(date +%H:%M:%S)) ==="
kubectl apply -f "$K8S"

echo "=== Wait for all $VARIANT pods Running ==="
for i in {1..120}; do
  total=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v' | wc -l)
  running=$(kubectl get pods 2>/dev/null | awk -v v="$VARIANT" '$0 ~ v && $3=="Running"' | wc -l)
  if [ "$total" -gt 0 ] && [ "$running" = "$total" ]; then
    echo "all $total $VARIANT pods Running"; break
  fi
  echo "[${i}/120] $running/$total Running"
  sleep 5
done

echo "=== Patch NodePorts ==="
kubectl patch service wrk2api-service-${VARIANT}-ctr -p '{"spec":{"type":"NodePort"}}'
kubectl patch service tracepressure-service-${VARIANT}-ctr -p '{"spec":{"type":"NodePort"}}' 2>/dev/null || true
for i in {1..15}; do
  NP=$(kubectl get svc wrk2api-service-${VARIANT}-ctr -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
  [ -n "$NP" ] && break
  sleep 1
done
echo "wrk2api NodePort: $NP"

# Settle before seeding
sleep 15

echo "=== Seed ==="
cd /users/tomislav/DeathStarBench/socialNetwork
python3 /users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/init_social_graph.py --ip 10.10.1.1 --port "$NP" 2>&1 | tail -5

echo "=== Run ramp_with_snapshots.sh $VARIANT $OUT_DIR $BREAK_SECS ==="
/users/tomislav/blueprint-docc-mod/utils/ramp_with_snapshots.sh "$VARIANT" "$OUT_DIR" "$BREAK_SECS"
echo "=== DONE → $OUT_DIR ==="
