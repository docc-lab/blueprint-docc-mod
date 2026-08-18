#!/usr/bin/env bash
# Set reverse-truss runtime knobs on deployed dsb_sn services (after --apply)
# ./rt_env.sh <policy> <depth> <parentid> <leaf-reject> [leaf-regex] [root-regex]
set -euo pipefail
POL="${1:-1}"; DEPTH="${2:-3}"; PAR="${3:-off}"; REJ="${4:-0.2}"
LEAF="${5:-(mongodb|redis|memcached|social-graph|url-shorten)}"; ROOT="${6:-(wrk2api|frontend)}"
kubectl set env deploy,daemonset --all --containers='*' \
  REVERSE_TRUSS=on RT_POLICY="$POL" RT_DEPTH="$DEPTH" RT_PARENTID="$PAR"
kubectl get deploy,daemonset -o name | grep -E "$ROOT" | xargs -r -I{} kubectl set env {} RT_ROOT=on
kubectl get deploy,daemonset -o name | grep -E "$LEAF" | xargs -r -I{} kubectl set env {} RT_LEAF_REJECT="$REJ"
echo "reverse-truss ON; policy=$POL depth=$DEPTH parentid=$PAR; roots=$ROOT; leaves=$LEAF @ $REJ"