#!/bin/bash
# Steady-state workload + concurrent trace-pressure spam.
#
# Phases:
#   1. Warmup    — wrk2api at 100 rps × 100s    (cache fill, no measurement)
#   2. Ramp      — wrk2api at 2000 rps × 60s    (heat the pipeline)
#   3. Steady    — wrk2api at STEADY_RPS × STEADY_SECS, with concurrent
#                  TracePressureService spam at SPAM_RPS req/s (each
#                  request producing 1 CP + SPAM_N LP child spans).
#
# Captures:
#   - per-step wrk output (warmup.log, ramp.log, steady.log, spam.log)
#   - pre-steady and post-steady snapshots of every otelcol_ counter
#     across all otelcol pods (via port-forward + /metrics scrape)
#   - pre-steady and post-steady snapshots of every service's
#     sb_processor_metrics / vanilla_processor_metrics log line
#
# Usage:
#   run_steady_with_spam.sh <variant: vrev0|sbrev0> <out_dir> \
#       [STEADY_RPS=3000] [STEADY_SECS=300] [SPAM_RPS=90] [SPAM_N=100]

set -e
VARIANT=$1
OUT_DIR=$2
STEADY_RPS=${3:-3000}
STEADY_SECS=${4:-300}
SPAM_RPS=${5:-90}
SPAM_N=${6:-100}
[ -z "$VARIANT" ] || [ -z "$OUT_DIR" ] && {
  echo "Usage: $0 <variant> <out_dir> [STEADY_RPS=3000] [STEADY_SECS=300] [SPAM_RPS=90] [SPAM_N=100]" >&2
  exit 1
}
mkdir -p "$OUT_DIR"

LUA=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua
SPAMLUA=/users/tomislav/blueprint-docc-mod/utils/spam_post.lua
POD_LABEL="io.kompose.service=otelcol-${VARIANT}-ctr"

# Get NodePorts.
NP_WRK=$(kubectl get service "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
NP_TP=$(kubectl get service "tracepressure-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null)

[ -z "$NP_WRK" ] && { echo "wrk2api NodePort not found" >&2; exit 1; }
[ -z "$NP_TP" ]  && { echo "tracepressure NodePort not found" >&2; exit 1; }

WRK_URL="http://10.10.1.1:$NP_WRK"
SPAM_URL="http://10.10.1.1:$NP_TP/Spam?n=$SPAM_N"

echo "wrk2api URL:        $WRK_URL"
echo "tracepressure URL:  $SPAM_URL"
echo "Steady:  $STEADY_RPS rps × $STEADY_SECS s"
echo "Spam:    $SPAM_RPS req/s × n=$SPAM_N (= $((SPAM_RPS * (1 + SPAM_N))) spans/s cluster-wide)"

# --- snapshot_otelcol() captures all otelcol_ counters across all daemon pods ---
snapshot_otelcol() {
  local PHASE=$1
  local out="$OUT_DIR/otelcol.${PHASE}.tsv"
  echo -e "pod\tmetric\tvalue" > "$out"

  local port=21000
  declare -A PP
  local pids=()
  for pod in $(kubectl get pods -l "$POD_LABEL" --no-headers 2>/dev/null | awk '{print $1}'); do
    kubectl port-forward "$pod" "$port:8888" >/dev/null 2>&1 &
    pids+=("$!")
    PP[$pod]=$port
    port=$((port+1))
  done
  sleep 2
  for pod in "${!PP[@]}"; do
    local p=${PP[$pod]}
    local raw=$(curl -s --max-time 3 "http://localhost:$p/metrics" 2>/dev/null)
    [ -z "$raw" ] && continue
    echo "$raw" | awk -v pod="$pod" '
      /^# / { next }
      /_bucket{/ { next }
      /_count[ {]/ { next }
      /^otelcol_/ {
        n = $1; sub(/[{ ].*/, "", n)
        v = $NF
        key = pod "\t" n
        sums[key] += v
      }
      END { for (k in sums) printf "%s\t%.0f\n", k, sums[k] }
    ' >> "$out"
  done
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}

# --- snapshot_sdk() captures every service's sb_processor_metrics ---
snapshot_sdk() {
  local PHASE=$1
  local out="$OUT_DIR/sdk.${PHASE}.tsv"
  local MKEY="sb_processor_metrics"
  [[ "$VARIANT" == v* ]] && MKEY="vanilla_processor_metrics"

  echo -e "service\tpod\tkey\tvalue" > "$out"
  for svc in wrk2api composepost hometimeline media post-storage socialgraph text uniqueid urlshorten user userid usermention usertimeline tracepressure; do
    local pod=$(kubectl get pods --no-headers 2>/dev/null | grep "^${svc}-service-${VARIANT}-ctr" | awk 'NR==1{print $1}')
    [ -z "$pod" ] && continue
    # Metric lines emit every 1s but compete with arbitrary debug/error
    # noise. Grep the LAST 5 min of log and take the most-recent metric
    # line — guaranteed to land at least one snapshot regardless of noise.
    local line=$(kubectl logs "$pod" --since=5m 2>/dev/null | grep "$MKEY" | tail -1)
    [ -z "$line" ] && continue
    echo "$line" | grep -oE '[a-z_]+=[0-9]+' | while IFS='=' read -r k v; do
      printf "%s\t%s\t%s\t%s\n" "$svc" "$pod" "$k" "$v" >> "$out"
    done
  done
}

snapshot_pods() {
  local PHASE=$1
  kubectl get pods --no-headers -o wide 2>/dev/null | grep "$VARIANT" \
    | awk -v phase="$PHASE" '{print phase"\t"$1"\t"$3"\t"$4"\t"$7}' \
    >> "$OUT_DIR/pods.tsv"
}

# Headers
echo -e "phase\tpod\tstatus\trestarts\tnode" > "$OUT_DIR/pods.tsv"

# Phase 0: reset jaeger to release any accumulated in-memory traces from
# previous runs. jaeger-all-in-one's default storage is unbounded — a few
# heavy spam runs can fill 100s of GB of RAM and OOM the host node.
# Restarting between runs gives every experiment a clean ~0 GB starting
# point. (Future: switch jaeger to badger/disk or elasticsearch backing
# and skip this step.)
echo "=== Phase 0: Jaeger reset ===" >&2
kubectl rollout restart deployment/jaeger-${VARIANT}-ctr >/dev/null 2>&1
kubectl rollout status deployment/jaeger-${VARIANT}-ctr --timeout=120s >/dev/null 2>&1 || true

# Phase 1: Warmup
echo "=== Phase 1: Warmup 100rps × 60s ===" >&2
wrk -t 1 -c 10 -d 60s -L -s "$LUA" "$WRK_URL" -R 100 > "$OUT_DIR/warmup.log" 2>&1 || true

# Phase 2: Ramp
echo "=== Phase 2: Ramp 2000rps × 60s ===" >&2
wrk -t 20 -c 200 -d 60s -L -s "$LUA" "$WRK_URL" -R 2000 > "$OUT_DIR/ramp.log" 2>&1 || true

# Pre-steady snapshot
echo "=== Pre-steady snapshot ===" >&2
snapshot_pods pre_steady
snapshot_otelcol pre_steady
snapshot_sdk pre_steady

# Phase 3: Steady state + (optional) concurrent spam
echo "=== Phase 3: Steady $STEADY_RPS rps × $STEADY_SECS s + spam $SPAM_RPS req/s × n=$SPAM_N ===" >&2
C=$(( (STEADY_RPS*STEADY_RPS + 19999) / 20000 ))
T=$(( (C + 9) / 10 ))
[ "$T" -lt 1 ] && T=1
SPAM_PID=""
if [ "$SPAM_RPS" -gt 0 ]; then
  # Spam in background. Right-size wrk: spam is low-rate (~90 rps), so
  # don't over-provision connections. Use ~2× pod count for keep-alive
  # parallelism (each connection round-robins through the 9 backends).
  SC=18
  ST=9
  wrk -t "$ST" -c "$SC" -d "${STEADY_SECS}s" -L -s "$SPAMLUA" "$SPAM_URL" -R "$SPAM_RPS" > "$OUT_DIR/spam.log" 2>&1 &
  SPAM_PID=$!
fi
# Foreground real workload
wrk -t "$T" -c "$C" -d "${STEADY_SECS}s" -L -s "$LUA" "$WRK_URL" -R "$STEADY_RPS" > "$OUT_DIR/steady.log" 2>&1 || true
[ -n "$SPAM_PID" ] && wait "$SPAM_PID" 2>/dev/null || true

# Post-steady snapshot
echo "=== Post-steady snapshot ===" >&2
snapshot_pods post_steady
snapshot_otelcol post_steady
snapshot_sdk post_steady

echo "=== Done ===" >&2
