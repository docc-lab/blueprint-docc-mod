#!/bin/bash
# Per-step snapshot ramp driver.
# Replaces utils/run_dsb_sn_ramp.sh with one that captures full counter
# state before and after each wrk step, so we get per-step deltas of
# every otelcol Prometheus counter + every SDK slog metric.
#
# Usage: ramp_with_snapshots.sh <variant: vrev0|sbrev0> <out_dir> [BREAK_SECS]
#
# BREAK_SECS controls the pause between ramp steps. Default 20s, but set
# to 0 (or any small value) to keep continuous pressure on the collector
# — with longer breaks the system drains and recovers between steps,
# masking the steady-state behavior at each rps level.
#
# Out_dir layout:
#   snapshots.tsv         — per-step before/after counter snapshots
#                           (time, phase, step_rps, category, source, metric, value)
#   ramp.log              — wrk output, one block per step
#   summary.tsv           — per-step deltas (rps, achieved, mean_ms, p99_ms, deltas...)

set -e
VARIANT=$1
OUT_DIR=$2
BREAK_SECS=${3:-20}
[ -z "$VARIANT" ] || [ -z "$OUT_DIR" ] && { echo "Usage: $0 <variant> <out_dir> [BREAK_SECS]" >&2; exit 1; }
mkdir -p "$OUT_DIR"
SNAP="$OUT_DIR/snapshots.tsv"
LOG="$OUT_DIR/ramp.log"
SUMMARY="$OUT_DIR/summary.tsv"

POD_LABEL="io.kompose.service=otelcol-${VARIANT}-ctr"
LUA=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua

echo -e "time\tphase\tstep_rps\tcategory\tsource\tmetric\tvalue" > "$SNAP"
echo -e "step_rps\tachieved_rps\tmean_ms\tp99_ms\tnon_2xx" > "$SUMMARY"

# --- snapshot() captures the current state of ALL counters/gauges ---
#   $1 = phase tag (e.g. "before:2000", "after:2000")
#   $2 = step_rps (e.g. 2000)
snapshot() {
  local PHASE=$1
  local STEP=$2
  local T=$(date +%s)
  echo "[$(date +%H:%M:%S)] snapshot $PHASE step=$STEP" >&2

  # Start port-forwards in parallel
  local PORT=19001
  declare -A PP
  local PIDS=()
  for pod in $(kubectl get pods -l "$POD_LABEL" --no-headers 2>/dev/null | awk '{print $1}'); do
    kubectl port-forward "$pod" "$PORT:8888" > /dev/null 2>&1 &
    PIDS+=("$!")
    PP[$pod]=$PORT
    PORT=$((PORT+1))
  done
  sleep 2  # let port-forwards establish

  # Scrape each pod
  for pod in "${!PP[@]}"; do
    local p=${PP[$pod]}
    local raw=$(curl -s --max-time 5 "http://localhost:$p/metrics" 2>/dev/null)
    if [ -z "$raw" ]; then
      echo "  WARN: no metrics from $pod port $p" >&2
      continue
    fi
    # All otelcol_ counter/gauge values (excluding histogram buckets/counts)
    echo "$raw" | awk -v t="$T" -v phase="$PHASE" -v step="$STEP" -v pod="$pod" '
      /^# / { next }
      /_bucket{/ { next }
      /_count[ {]/ { next }
      /^otelcol_/ {
        # metric name is up to first { or space
        n = $1
        sub(/[{ ].*/, "", n)
        # value is last whitespace token
        v = $NF
        # sum across all label sets per metric per pod
        key = pod "\t" n
        sums[key] += v
        seen[key] = 1
      }
      END {
        for (key in sums) {
          split(key, parts, "\t")
          printf "%s\tsnap\t%s\totelcol\t%s\t%s\t%.0f\n", t, step, parts[1], parts[2], sums[key]
        }
      }' >> "$SNAP"
  done

  # Kill port-forwards
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true

  # Restart counts
  kubectl get pods -l "$POD_LABEL" -o json 2>/dev/null | python3 -c "
import json, sys
data = json.load(sys.stdin)
for pod in data['items']:
    n = pod['metadata']['name']
    for cs in pod['status'].get('containerStatuses', []):
        r = cs.get('restartCount', 0)
        print(f'$T\tsnap\t$STEP\tk8s\t{n}\trestart_count\t{r}')
" >> "$SNAP" 2>/dev/null

  # SDK-side metrics
  if [ "$VARIANT" = "vrev0" ]; then
    MKEY="vanilla_processor_metrics"
  else
    MKEY="sb_processor_metrics"
  fi
  for svc in wrk2api composepost hometimeline media post-storage socialgraph text uniqueid urlshorten user userid usermention usertimeline; do
    local pod=$(kubectl get pods --no-headers 2>/dev/null | grep "^${svc}-service-${VARIANT}-ctr" | awk '{print $1}' | head -1)
    [ -z "$pod" ] && continue
    local line=$(kubectl logs "$pod" --tail=2 2>/dev/null | grep "$MKEY" | tail -1)
    [ -z "$line" ] && continue
    for k in spans_received spans_sent spans_dropped spans_flushed batches_sent batches_dropped send_unavailable send_deadline send_exhausted send_canceled send_other buffer_depth; do
      local v=$(echo "$line" | grep -oP "$k=\K[0-9]+")
      [ -n "$v" ] && echo -e "$T\tsnap\t$STEP\tsdk\t$svc\t$k\t$v" >> "$SNAP"
    done
  done
}

# --- Run a single wrk step ---
#   $1 = target rps
#   captures wrk output to LOG
run_step() {
  local RPS=$1
  local C=$(( (RPS*RPS + 19999) / 20000 ))
  local T=$(( (C + 9) / 10 ))
  local NP=$(kubectl get service "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
  local URL="http://10.10.1.1:$NP"

  echo "" >> "$LOG"
  echo "=== rps=$RPS c=$C t=$T url=$URL ===" >> "$LOG"
  local OUT=$(wrk -t "$T" -c "$C" -d 60s -L -s "$LUA" "$URL" -R "$RPS" 2>&1)
  echo "$OUT" >> "$LOG"

  local ACHIEVED=$(echo "$OUT" | grep "Requests/sec" | awk '{print $NF}')
  local MEAN=$(echo "$OUT" | grep -A 1 "Thread Stats" | tail -1 | awk '{print $2}')
  local P99=$(echo "$OUT" | grep -A 1 "Thread Stats" | tail -1 | awk '{print $4}')
  local NON2XX=$(echo "$OUT" | grep "Non-2xx" | awk '{print $NF}')
  [ -z "$NON2XX" ] && NON2XX=0
  echo -e "$RPS\t$ACHIEVED\t$MEAN\t$P99\t$NON2XX" >> "$SUMMARY"
}

# --- DRIVER ---
echo "=== Pre-warmup snapshot ===" >&2
snapshot pre_warmup 0

# Warmup
echo "=== Warmup 100rps × 100s ===" >&2
NP=$(kubectl get service "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
wrk -t 1 -c 10 -d 100s -L -s "$LUA" "http://10.10.1.1:$NP" -R 100 >> "$LOG" 2>&1 || true

echo "=== Post-warmup snapshot ===" >&2
snapshot post_warmup 0

# Ramp
for RPS in 2000 2200 2400 2600 2800 3000 3200 3400 3600 3800 4000; do
  snapshot before "$RPS"
  echo "=== Running step rps=$RPS ===" >&2
  run_step "$RPS"
  snapshot after "$RPS"
  [ "$BREAK_SECS" -gt 0 ] && sleep "$BREAK_SECS"  # break (configurable, default 20s)
done

echo "=== Done ===" >&2
