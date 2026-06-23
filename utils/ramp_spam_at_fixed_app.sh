#!/bin/bash
# Collector-throughput sweep: hold the app at a FIXED request rate, then ramp
# the TracePressureService spammer (plain spans) and snapshot cluster otelcol
# counters before/after each spam step. Pair with a file->/dev/null sink so the
# COLLECTOR (not jaeger/ES) is what saturates.
#
# Usage: ramp_spam_at_fixed_app.sh <variant> <out_dir> <APP_RPS> [SPAM_RPS=200] [STEP_SECS=30] [N_LIST="0 50 100 250 500 1000 2000"]
#   spam spans/s per step ~= SPAM_RPS * (1 + N)
set -uo pipefail
VARIANT=$1; OUT_DIR=$2; APP_RPS=$3
SPAM_RPS=${4:-200}; STEP_SECS=${5:-30}; N_LIST=${6:-"0 100 250 500 1000 2000 3500 5000 7500"}
mkdir -p "$OUT_DIR"
SNAP="$OUT_DIR/snapshots.tsv"
POD_LABEL="io.kompose.service=otelcol-${VARIANT}-ctr"
LUA=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua
SPAMLUA=/users/tomislav/blueprint-docc-mod/utils/spam_post.lua
echo -e "time\tphase\tstep_rps\tcategory\tsource\tmetric\tvalue" > "$SNAP"

NP_WRK=$(kubectl get svc "wrk2api-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[?(@.port==2000)].nodePort}')
NP_TP=$(kubectl get svc "tracepressure-service-${VARIANT}-ctr" -o jsonpath='{.spec.ports[0].nodePort}')
WRK_URL="http://10.10.1.1:$NP_WRK"
[ -z "$NP_WRK" ] && { echo "no wrk2api NodePort"; exit 1; }
[ -z "$NP_TP" ]  && { echo "no tracepressure NodePort"; exit 1; }

snapshot(){ # $1 phase  $2 step(=spam spans/s)
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
  for pid in "${PIDS[@]}"; do wait "$pid" 2>/dev/null || true; done   # reap ONLY port-forwards, never the bg app wrk
}

# Per-step: app in FOREGROUND (so each step logs its own achieved rps) +
# spam in background. App runs every step at the fixed rate (brief gaps only
# during snapshots).
AC=$(( (APP_RPS*APP_RPS + 19999) / 20000 )); AT=$(( (AC + 9) / 10 )); [ "$AT" -lt 1 ] && AT=1
echo "=== app $APP_RPS rps (fg per step)  spam $SPAM_RPS req/s x N in [$N_LIST] ===" >&2
echo "=== warmup 100rps x 30s ===" >&2
wrk -t1 -c10 -d30s -L -s "$LUA" "$WRK_URL" -R 100 >/dev/null 2>&1 || true

echo -e "spam_rate\tapp_achieved_rps\tapp_non2xx" > "$OUT_DIR/app_rps.tsv"
for N in $N_LIST; do
  RATE=$(( SPAM_RPS * (1 + N) ))
  snapshot before "$RATE"
  SPID=""
  if [ "$N" -gt 0 ]; then
    wrk -t9 -c18 -d"${STEP_SECS}s" -L -s "$SPAMLUA" "http://10.10.1.1:$NP_TP/Spam?n=$N" -R "$SPAM_RPS" > "$OUT_DIR/spam_n${N}.log" 2>&1 &
    SPID=$!
  fi
  wrk -t"$AT" -c"$AC" -d"${STEP_SECS}s" -L -s "$LUA" "$WRK_URL" -R "$APP_RPS" > "$OUT_DIR/app_step_${RATE}.log" 2>&1 || true
  [ -n "$SPID" ] && wait "$SPID" 2>/dev/null || true
  snapshot after "$RATE"
  # record app achieved rps + non-2xx for this step
  ARPS=$(grep -i 'Requests/sec' "$OUT_DIR/app_step_${RATE}.log" | awk '{print $2}')
  AN2=$(grep -i 'Non-2xx' "$OUT_DIR/app_step_${RATE}.log" | awk '{print $NF}')
  echo -e "${RATE}\t${ARPS:-NA}\t${AN2:-0}" >> "$OUT_DIR/app_rps.tsv"
done
echo "=== DONE -> $OUT_DIR ===" >&2
cat "$OUT_DIR/app_rps.tsv" >&2
