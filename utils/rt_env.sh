#!/usr/bin/env bash
# Set reverse-truss runtime knobs on deployed dsb_sn services (after --apply)
# ./rt_env.sh <policy> <depth> <parentid> <leaf-reject> [leaf-regex] [root-regex]
# Tomislav-RetCtx: RT_LEAF_REJECT applies only to unscheduled leaves; spans
# still export. Collector config selects per-truss TTL or probability routing.
# The first two positional arguments remain accepted but no longer affect SDK
# routing. See docs/dev/reverse_truss.md for original-checkpoint/TTL emission.
set -euo pipefail
POL="${1:-1}"; DEPTH="${2:-3}"; PAR="${3:-off}"; REJ="${4:-0.2}"
LEAF="${5:-(poststorage|socialgraph|urlshorten|uniqueid|media|usertimeline)-service}"; ROOT="${6:-(wrk2api|frontend)}"
kubectl set env deploy,daemonset --all --containers='*' \
  REVERSE_TRUSS=on RT_POLICY="$POL" RT_DEPTH="$DEPTH" RT_PARENTID="$PAR"
kubectl get deploy,daemonset -o name | grep -E "$ROOT" | xargs -r -I{} kubectl set env {} RT_ROOT=on
kubectl get deploy,daemonset -o name | grep -E "$LEAF" | xargs -r -I{} kubectl set env {} RT_LEAF_REJECT="$REJ"
echo "reverse-truss ON; routing uses collector configuration; parentid=$PAR; roots=$ROOT; leaves=$LEAF @ $REJ"
echo "Tomislav-RetCtx: legacy policy=$POL/depth=$DEPTH arguments are ignored by SDK routing"
