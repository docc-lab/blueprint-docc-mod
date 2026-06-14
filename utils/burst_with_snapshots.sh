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
  # Vanilla SDK emits "vanilla_processor_metrics"; SB emits "sb_processor_metrics".
  # VARIANT can be vrev0 / vesrev0 / v-esrev0 / sb_rev0 / sb-esrev0 / etc.
  # Treat anything containing "sb" as SB; everything else as vanilla.
  if [[ "$VARIANT" == *sb* ]]; then
    MKEY="sb_processor_metrics"
  else
    MKEY="vanilla_processor_metrics"
  fi
  for svc in wrk2api composepost hometimeline media post-storage socialgraph text uniqueid urlshorten user userid usermention usertimeline; do
    local pod=$(kubectl get pods --no-headers 2>/dev/null | grep "^${svc}-service-${VARIANT}-ctr" | awk '{print $1}' | head -1)
    [ -z "$pod" ] && continue
    local line=$(kubectl logs "$pod" --tail=2 2>/dev/null | grep "$MKEY" | tail -1)
    [ -z "$line" ] && continue
    for k in spans_received spans_sent spans_dropped spans_flushed batches_sent batches_dropped send_unavailable send_deadline send_exhausted send_canceled send_other buffer_depth cp_received lp_received cp_sent lp_sent cp_dropped lp_dropped; do
      local v=$(echo "$line" | grep -oP "$k=\K[0-9]+" || true)
      if [ -n "$v" ]; then
        echo -e "$T\tsnap\t$STEP\tsdk\t$svc\t$k\t$v" >> "$SNAP"
      fi
    done
    true  # ensure loop exit is success even if last iter's grep matched nothing
  done
}

# Burst profile params (positional 3..6, with defaults).
#   $3 BASE_RPS   steady baseline rate before the burst   (default 2000)
#   $4 BURST_RPS  sudden jump rate                          (default 3800)
#   $5 BASE_SECS  seconds to hold baseline                  (default 45)
#   $6 BURST_SECS seconds to hold burst                     (default 60)
BASE_RPS=${3:-2000}
BURST_RPS=${4:-3800}
BASE_SECS=${5:-45}
BURST_SECS=${6:-60}

TIMELINE="$OUT_DIR/burst_timeline.tsv"
echo -e "t\telapsed\tphase\tcp_received\tcp_dropped\tlp_received\tlp_dropped\tspans_received\tspans_dropped" > "$TIMELINE"

# --- run_load: fire wrk at a target rps for a given duration (no
#     summary parsing; the fine sampler captures the dynamics) ---
run_load() {
  local RPS=$1
  local DUR=$2
  local C=$(( (RPS*RPS + 19999) / 20000 ))
  local T=$(( (C + 9) / 10 ))
  local NP=$(kubectl get service "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
  local URL="http://10.10.1.1:$NP"
  echo "" >> "$LOG"
  echo "=== run_load rps=$RPS dur=${DUR}s c=$C t=$T url=$URL ===" >> "$LOG"
  local OUT=$(wrk -t "$T" -c "$C" -d "${DUR}s" -L -s "$LUA" "$URL" -R "$RPS" 2>&1)
  echo "$OUT" >> "$LOG"
  local ACHIEVED=$(echo "$OUT" | grep "Requests/sec" | awk '{print $NF}')
  local MEAN=$(echo "$OUT" | grep -A 1 "Thread Stats" | tail -1 | awk '{print $2}')
  local P99=$(echo "$OUT" | grep -A 1 "Thread Stats" | tail -1 | awk '{print $4}')
  local NON2XX=$(echo "$OUT" | grep "Non-2xx" | awk '{print $NF}')
  [ -z "$NON2XX" ] && NON2XX=0
  echo -e "$RPS\t$ACHIEVED\t$MEAN\t$P99\t$NON2XX" >> "$SUMMARY"
}

# --- fine_sampler: every 1s, read the colocated composepost SDK's
#     latest *_processor_metrics line and append cumulative counters to
#     the timeline. Runs in the background for the whole load window so
#     the burst transient is captured at 1Hz without pausing load. ---
fine_sampler() {
  local DUR=$1
  if [[ "$VARIANT" == *sb* ]]; then MKEY="sb_processor_metrics"; else MKEY="vanilla_processor_metrics"; fi
  local pod=$(kubectl get pods --no-headers 2>/dev/null | grep "^composepost-service-${VARIANT}-ctr" | awk '{print $1}' | head -1)
  local start=$(date +%s)
  local i=0
  while [ "$i" -lt "$DUR" ]; do
    local now=$(date +%s)
    local el=$(( now - start ))
    local phase="baseline"; [ "$el" -ge "$BASE_SECS" ] && phase="burst"
    local line=$(kubectl logs "$pod" --tail=2 2>/dev/null | grep "$MKEY" | tail -1)
    if [ -n "$line" ]; then
      g() { echo "$line" | grep -oP "$1=\K[0-9]+" || echo ""; }
      echo -e "$now\t$el\t$phase\t$(g cp_received)\t$(g cp_dropped)\t$(g lp_received)\t$(g lp_dropped)\t$(g spans_received)\t$(g spans_dropped)" >> "$TIMELINE"
    fi
    i=$(( i + 1 ))
    sleep 1
  done
}

# --- DRIVER (burst) ---
echo "=== Pre-warmup snapshot ===" >&2
snapshot pre_warmup 0

echo "=== Warmup 100rps × 60s ===" >&2
NP=$(kubectl get service "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
wrk -t 1 -c 10 -d 60s -L -s "$LUA" "http://10.10.1.1:$NP" -R 100 >> "$LOG" 2>&1 || true

echo "=== Snapshot before baseline ===" >&2
snapshot before_baseline "$BASE_RPS"

# Launch the 1Hz fine sampler covering baseline + burst, then drive load
# back-to-back with NO gap so the burst is a true step function.
TOTAL_SECS=$(( BASE_SECS + BURST_SECS ))
echo "=== Fine sampler ${TOTAL_SECS}s; baseline ${BASE_RPS}×${BASE_SECS}s → BURST ${BURST_RPS}×${BURST_SECS}s ===" >&2
fine_sampler "$TOTAL_SECS" &
SAMPLER_PID=$!

# Back-to-back with NO snapshot in between → true step function. The
# fine sampler captures the onset transient at 1Hz.
run_load "$BASE_RPS" "$BASE_SECS"
run_load "$BURST_RPS" "$BURST_SECS"

wait "$SAMPLER_PID" 2>/dev/null || true
echo "=== Snapshot after burst ===" >&2
snapshot after_burst "$BURST_RPS"

echo "=== Done ===" >&2
