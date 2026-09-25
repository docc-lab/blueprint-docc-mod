#!/usr/bin/env bash
# Tomislav-RetCtx: wait for a run to reach its measurement window, then capture a
# CPU profile from a service pod. Designed to be launched with setsid so it
# survives a dropped connection or a killed parent shell.
#
# Usage: capture_profile.sh <run-root> <service-prefix> <local-port> <out.pb.gz> [seconds]
set -eu

ROOT=$1; PREFIX=$2; PORT=$3; OUT=$4; SECONDS_TO_SAMPLE=${5:-45}
LOG="$OUT.log"

log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

log "waiting for measuring stage in $ROOT"
for _ in $(seq 1 360); do
  stage=$(python3 -c "import json;print(json.load(open('$ROOT/run-status.json')).get('stage',''))" 2>/dev/null || echo "")
  [ "$stage" = "measuring" ] && break
  if [ -f "$ROOT/run-complete.json" ]; then log "run already complete, nothing to capture"; exit 1; fi
  sleep 5
done
if [ "$stage" != "measuring" ]; then log "timed out waiting for measuring (stage=$stage)"; exit 1; fi
log "measuring; locating pod"

POD=$(kubectl -n dsb-sn get pods --no-headers -o custom-columns=:metadata.name | grep "^$PREFIX" | head -1)
if [ -z "$POD" ]; then log "no pod matching $PREFIX"; exit 1; fi
log "pod $POD"

kubectl -n dsb-sn port-forward "$POD" "$PORT:6060" >/dev/null 2>&1 &
PF=$!
trap 'kill $PF 2>/dev/null || true' EXIT
sleep 4

if curl -s --max-time $((SECONDS_TO_SAMPLE + 30)) -o "$OUT" \
     "http://localhost:$PORT/debug/pprof/profile?seconds=$SECONDS_TO_SAMPLE"; then
  log "captured $(stat -c %s "$OUT") bytes into $OUT"
else
  log "capture FAILED"
  exit 1
fi
