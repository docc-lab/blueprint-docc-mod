#!/usr/bin/env bash
# Tomislav-RetCtx: wait for the measurement window, then profile SEVERAL services
# at once. Detached-safe: run under setsid, nothing depends on the parent shell.
#
# Usage: capture_multi.sh <run-root> <out-prefix> <seconds> <svc:port> [svc:port ...]
set -eu
ROOT=$1; OUTPREFIX=$2; SECS=$3; shift 3
LOG="$OUTPREFIX.log"
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

log "waiting for measuring in $ROOT"
stage=""
for _ in $(seq 1 360); do
  stage=$(python3 -c "import json;print(json.load(open('$ROOT/run-status.json')).get('stage',''))" 2>/dev/null || echo "")
  [ "$stage" = "measuring" ] && break
  sleep 5
done
[ "$stage" = "measuring" ] || { log "timed out (stage=$stage)"; exit 1; }
log "measuring; starting captures"

pids=""
for spec in "$@"; do
  svc=${spec%%:*}; port=${spec##*:}
  pod=$(kubectl -n dsb-sn get pods --no-headers -o custom-columns=:metadata.name | grep "^$svc" | head -1)
  if [ -z "$pod" ]; then log "no pod for $svc"; continue; fi
  log "$svc -> $pod (port $port)"
  # Tomislav-RetCtx: keep the forwarder's output. Silencing it once cost a whole
  # measurement window: all four curls failed instantly and the log said nothing.
  kubectl -n dsb-sn port-forward "$pod" "$port:6060" >>"$LOG.pf" 2>&1 &
  pids="$pids $!"
done

# Wait for each port to actually accept a connection instead of sleeping blind.
for spec in "$@"; do
  port=${spec##*:}
  ready=""
  for _ in $(seq 1 40); do
    if (exec 3<>/dev/tcp/127.0.0.1/"$port") 2>/dev/null; then ready=1; break; fi
    sleep 0.5
  done
  [ -n "$ready" ] || log "port $port never became ready"
done
for spec in "$@"; do
  svc=${spec%%:*}; port=${spec##*:}
  ( curl -s --max-time $((SECS + 30)) -o "$OUTPREFIX-$svc.pb.gz" \
      "http://localhost:$port/debug/pprof/profile?seconds=$SECS" \
      && echo "$(date -u +%H:%M:%S) captured $(stat -c %s "$OUTPREFIX-$svc.pb.gz") bytes for $svc" >> "$LOG" \
      || echo "$(date -u +%H:%M:%S) FAILED $svc" >> "$LOG" ) &
done
wait
for p in $pids; do kill "$p" 2>/dev/null || true; done
log "all captures done"
