#!/bin/bash
# Collector-throughput sweep with a NEUTRAL OTLP spammer (telemetrygen) that
# reaches EVERY otelcol DaemonSet pod evenly: one telemetrygen instance per
# otelcol pod IP, run from node-0 (off the app/collector nodes). App held at a
# fixed rate (the bridge-data load); neutral spam ramped to saturate the collector.
# Pair with a file->/dev/null otelcol sink.
#
# Usage: ramp_telemetrygen_at_fixed_app.sh <variant> <out_dir> <APP_RPS> [STEP_SECS=30] [TOTAL_LIST="0 100000 200000 500000 1000000 1500000"]
#   per-instance rate = TOTAL / (#otelcol pods)
set -uo pipefail
VARIANT=$1; OUT_DIR=$2; APP_RPS=$3
STEP_SECS=${4:-30}; TOTAL_LIST=${5:-"0 100000 200000 500000 1000000 1500000"}
TGEN=/tmp/telemetrygen
mkdir -p "$OUT_DIR"; SNAP="$OUT_DIR/snapshots.tsv"
POD_LABEL="io.kompose.service=otelcol-${VARIANT}-ctr"
LUA=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua
NP_WRK=$(kubectl get svc "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
WRK_URL="http://10.10.1.1:$NP_WRK"
echo -e "time\tphase\tstep_rps\tcategory\tsource\tmetric\tvalue" > "$SNAP"

# otelcol pod IPs (stable within a run)
mapfile -t IPS < <(kubectl get pods -l "$POD_LABEL" -o jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' | grep -v '^$')
NPODS=${#IPS[@]}
echo "otelcol pods: $NPODS  IPs: ${IPS[*]}" >&2

snapshot(){ # $1 phase  $2 step(=total spam/s)
  local PHASE=$1 STEP=$2 T; T=$(date +%s)
  echo "[$(date +%H:%M:%S)] snapshot $PHASE step=$STEP" >&2
  local PORT=19001; declare -A PP; local PIDS=()
  for pod in $(kubectl get pods -l "$POD_LABEL" --no-headers 2>/dev/null | awk '{print $1}'); do
    kubectl port-forward "$pod" "$PORT:8888" >/dev/null 2>&1 & PIDS+=("$!"); PP[$pod]=$PORT; PORT=$((PORT+1))
  done
  sleep 2
  for pod in "${!PP[@]}"; do
    local p=${PP[$pod]} raw; raw=$(curl -s --max-time 5 "http://localhost:$p/metrics" 2>/dev/null)
    [ -z "$raw" ] && { echo "  WARN no metrics $pod" >&2; continue; }
    echo "$raw" | awk -v t="$T" -v step="$STEP" -v pod="$pod" '
      /^# /{next} /_bucket{/{next} /_count[ {]/{next}
      /^otelcol_/ { n=$1; sub(/[{ ].*/,"",n); sums[pod"\t"n]+=$NF }
      END { for(k in sums){ split(k,a,"\t"); printf "%s\tsnap\t%s\totelcol\t%s\t%s\t%.0f\n",t,step,a[1],a[2],sums[k] } }' >> "$SNAP"
  done
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${PIDS[@]}"; do wait "$pid" 2>/dev/null || true; done
}

AC=$(( (APP_RPS*APP_RPS + 19999) / 20000 )); AT=$(( (AC + 9) / 10 )); [ "$AT" -lt 1 ] && AT=1
echo "=== app $APP_RPS rps (fg/step) + neutral telemetrygen x${NPODS} pods, totals [$TOTAL_LIST] ===" >&2
echo "=== warmup 100rps x 30s ===" >&2
wrk -t1 -c10 -d30s -L -s "$LUA" "$WRK_URL" -R 100 >/dev/null 2>&1 || true
echo -e "spam_total\tapp_achieved_rps\tapp_non2xx" > "$OUT_DIR/app_rps.tsv"

for TOT in $TOTAL_LIST; do
  snapshot before "$TOT"
  TPIDS=()
  if [ "$TOT" -gt 0 ]; then
    R=$(( TOT / NPODS ))
    for ip in "${IPS[@]}"; do
      "$TGEN" traces --otlp-endpoint "$ip:4317" --otlp-insecure --rate "$R" --duration "${STEP_SECS}s" --workers 4 >/dev/null 2>&1 &
      TPIDS+=("$!")
    done
  fi
  wrk -t"$AT" -c"$AC" -d"${STEP_SECS}s" -L -s "$LUA" "$WRK_URL" -R "$APP_RPS" > "$OUT_DIR/app_step_${TOT}.log" 2>&1 || true
  for pid in "${TPIDS[@]}"; do wait "$pid" 2>/dev/null || true; done
  snapshot after "$TOT"
  ARPS=$(grep -i 'Requests/sec' "$OUT_DIR/app_step_${TOT}.log" | awk '{print $2}')
  echo -e "${TOT}\t${ARPS:-NA}\t$(grep -i 'Non-2xx' "$OUT_DIR/app_step_${TOT}.log" | awk '{print $NF}')" >> "$OUT_DIR/app_rps.tsv"
done
echo "=== DONE -> $OUT_DIR ===" >&2; cat "$OUT_DIR/app_rps.tsv" >&2
