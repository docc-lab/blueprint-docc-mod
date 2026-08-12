#!/usr/bin/env bash
# run_ramp.sh — PayloadBench ramp runner.
#
# Drives the EdgeService NodePort with workload/payload.lua at a stepped
# constant rate (wrk2 -R, coordinated-omission corrected), fixed payload sizes
# both ways. One wrk output file per step lands in results/<run>/step_<rps>.txt.
#
# Usage: ./run_ramp.sh [--start N] [--end N] [--step N] [--duration S] [--break S]
#                      [--req-size B] [--res-size B] [--url URL] [--name TAG]
set -euo pipefail

START=2000; END=28000; STEP=1000
DURATION=30; BREAK=10
REQ_SIZE=1024; RES_SIZE=1024
# Size distributions passed through to payload.lua. `fixed` uses REQ/RES_SIZE;
# `uniform` uses the MIN/MAX pair; normal/exp use MEAN (+STD) with MIN/MAX as clamps.
REQ_DIST=fixed; RES_DIST=fixed
REQ_MIN=""; REQ_MAX=""; RES_MIN=""; RES_MAX=""
REQ_MEAN=""; RES_MEAN=""; REQ_STD=""; RES_STD=""
REQ_P_LARGE=""; RES_P_LARGE=""; REQ_SMALL=""; RES_SMALL=""; REQ_LARGE=""; RES_LARGE=""
WARMUP_RPS=500; WARMUP_DURATION=60
THREADS=16; CONNS=1024
ROUNDS=1; ROUND_START=1
# --out reuses an existing results dir (no new timestamp) and --round-start N
# begins at round N, so an interrupted campaign can be resumed in place.
OUTDIR=""
URL=""; NAME="nt_es"
DIR="$(cd "$(dirname "$0")" && pwd)"
LUA="$DIR/workload/payload.lua"

while [[ $# -gt 0 ]]; do case "$1" in
  --start) START=$2; shift 2;; --end) END=$2; shift 2;; --step) STEP=$2; shift 2;;
  --duration) DURATION=$2; shift 2;; --break) BREAK=$2; shift 2;;
  --req-size) REQ_SIZE=$2; shift 2;; --res-size) RES_SIZE=$2; shift 2;;
  --threads) THREADS=$2; shift 2;; --conns) CONNS=$2; shift 2;;
  --url) URL=$2; shift 2;; --name) NAME=$2; shift 2;;
  --rounds) ROUNDS=$2; shift 2;;
  --round-start) ROUND_START=$2; shift 2;;
  --out) OUTDIR=$2; shift 2;;
  --req-dist) REQ_DIST=$2; shift 2;; --res-dist) RES_DIST=$2; shift 2;;
  --req-min) REQ_MIN=$2; shift 2;;   --req-max) REQ_MAX=$2; shift 2;;
  --res-min) RES_MIN=$2; shift 2;;   --res-max) RES_MAX=$2; shift 2;;
  --req-mean) REQ_MEAN=$2; shift 2;; --res-mean) RES_MEAN=$2; shift 2;;
  --req-std) REQ_STD=$2; shift 2;;   --res-std) RES_STD=$2; shift 2;;
  --req-p-large) REQ_P_LARGE=$2; shift 2;;  --res-p-large) RES_P_LARGE=$2; shift 2;;
  --req-small) REQ_SMALL=$2; shift 2;;      --res-small) RES_SMALL=$2; shift 2;;
  --req-large) REQ_LARGE=$2; shift 2;;      --res-large) RES_LARGE=$2; shift 2;;
  # Convenience: --size-jitter PCT sets BOTH directions to uniform
  # [size*(1-pct), size*(1+pct)] around --req-size/--res-size.
  --size-jitter) J=$2; shift 2
    REQ_DIST=uniform; RES_DIST=uniform
    REQ_MIN=$(( REQ_SIZE * (100 - J) / 100 )); REQ_MAX=$(( REQ_SIZE * (100 + J) / 100 ))
    RES_MIN=$(( RES_SIZE * (100 - J) / 100 )); RES_MAX=$(( RES_SIZE * (100 + J) / 100 ));;
  *) echo "unknown arg: $1"; exit 1;;
esac; done

# Resolve the edge NodePort URL if not given: target the node the edge pod
# actually runs on (avoids an extra kube-proxy hop for the measurement).
if [ -z "$URL" ]; then
  # NB: early-exiting consumers (head/grep -m1/awk exit) SIGPIPE kubectl under
  # pipefail — capture full output first, then filter.
  SVC=$(kubectl get svc -o name); SVC=$(grep -E "edge-service-.*-ctr" <<<"$SVC" | head -1 | cut -d/ -f2)
  [ -n "$SVC" ] || { echo "no edge-service svc found"; exit 1; }
  PORT=$(kubectl get svc "$SVC" -o jsonpath='{.spec.ports[0].nodePort}')
  [ -n "$PORT" ] || { echo "svc $SVC has no NodePort (patch it first)"; exit 1; }
  PODS=$(kubectl get pods -o wide --no-headers); NODE=$(awk '/edge-service-.*-ctr/ {print $7; exit}' <<<"$PODS")
  IP=$(kubectl get node "$NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')
  URL="http://$IP:$PORT"
fi

if [ -n "$OUTDIR" ]; then OUT="$OUTDIR"; else
  TS=$(date +%Y%m%d_%H%M%S); OUT="$DIR/results/ramp_${NAME}_${TS}"
fi
mkdir -p "$OUT"
{
  echo "url=$URL"
  echo "req: dist=$REQ_DIST size=$REQ_SIZE min=${REQ_MIN:--} max=${REQ_MAX:--} mean=${REQ_MEAN:--} std=${REQ_STD:--}"
  echo "res: dist=$RES_DIST size=$RES_SIZE min=${RES_MIN:--} max=${RES_MAX:--} mean=${RES_MEAN:--} std=${RES_STD:--}"
  echo "start=$START end=$END step=$STEP duration=${DURATION}s break=${BREAK}s rounds=${ROUND_START}..${ROUNDS}"
  echo "threads=$THREADS conns=$CONNS warmup=${WARMUP_RPS}rps/${WARMUP_DURATION}s"
} | tee -a "$OUT/config.txt"

# Only export the knobs that are set — payload.lua treats unset/empty as absent
# (numenv falls back to its default), so an empty REQ_MAX would NOT mean "uncapped".
export REQ_DIST RES_DIST REQ_SIZE RES_SIZE
for v in REQ_MIN REQ_MAX RES_MIN RES_MAX REQ_MEAN RES_MEAN REQ_STD RES_STD \
         REQ_P_LARGE RES_P_LARGE REQ_SMALL RES_SMALL REQ_LARGE RES_LARGE; do
  [ -n "${!v}" ] && export "$v" || unset "$v"
done

# Pre-flight: a distribution whose parameters are missing does NOT fail in the
# lua — numenv() silently falls back to 0, which would measure ZERO-byte
# payloads for the whole campaign. Fail loudly here instead.
for p in REQ RES; do
  eval "d=\$${p}_DIST"
  case "$d" in
    fixed) eval "s=\$${p}_SIZE"; [ "${s:-0}" -gt 0 ] || { echo "ERROR: ${p}_DIST=fixed needs --${p,,}-size > 0"; exit 1; };;
    uniform) eval "mn=\${${p}_MIN:-}; mx=\${${p}_MAX:-}"
      [ -n "$mn" ] && [ -n "$mx" ] || { echo "ERROR: ${p}_DIST=uniform needs --${p,,}-min and --${p,,}-max"; exit 1; };;
    exp) eval "m=\${${p}_MEAN:-}"
      [ -n "$m" ] && [ "$m" -gt 0 ] 2>/dev/null || { echo "ERROR: ${p}_DIST=exp needs --${p,,}-mean > 0"; exit 1; };;
    normal) eval "m=\${${p}_MEAN:-}; sd=\${${p}_STD:-}"
      [ -n "$m" ] && [ -n "$sd" ] || { echo "ERROR: ${p}_DIST=normal needs --${p,,}-mean and --${p,,}-std"; exit 1; };;
    bimodal) eval "pl=\${${p}_P_LARGE:-}; sm=\${${p}_SMALL:-}; lg=\${${p}_LARGE:-}"
      [ -n "$pl" ] && [ -n "$sm" ] && [ -n "$lg" ] || { echo "ERROR: ${p}_DIST=bimodal needs --${p,,}-p-large, --${p,,}-small, --${p,,}-large"; exit 1; };;
    zipf) eval "mn=\${${p}_MIN:-}; mx=\${${p}_MAX:-}"
      [ -n "$mn" ] && [ -n "$mx" ] || { echo "ERROR: ${p}_DIST=zipf needs --${p,,}-min and --${p,,}-max"; exit 1; };;
    *) echo "ERROR: unsupported ${p}_DIST='$d'"; exit 1;;
  esac
done

for (( round=ROUND_START; round<=ROUNDS; round++ )); do
  RDIR="$OUT"
  if [ "$ROUNDS" -gt 1 ]; then RDIR="$OUT/round_$round"; mkdir -p "$RDIR"; fi
  echo "=== round $round/$ROUNDS: warmup ${WARMUP_RPS} rps / ${WARMUP_DURATION}s ==="
  wrk -t 8 -c 128 -d ${WARMUP_DURATION}s -L -s "$LUA" "$URL" -R $WARMUP_RPS \
    > "$RDIR/warmup.txt" 2>&1 || { echo "warmup failed:"; tail -5 "$RDIR/warmup.txt"; exit 1; }
  grep -E "Non-2xx|Socket errors" "$RDIR/warmup.txt" || true

  for (( rps=START; rps<=END; rps+=STEP )); do
    echo "=== round $round/$ROUNDS step $rps rps / ${DURATION}s ==="
    wrk -t $THREADS -c $CONNS -d ${DURATION}s -L -s "$LUA" "$URL" -R $rps \
      > "$RDIR/step_${rps}.txt" 2>&1 || true
    grep -E "^Requests/sec|Non-2xx|Socket errors" "$RDIR/step_${rps}.txt" | sed 's/^/    /'
    grep -E "^ 50.000%|^ 99.000%" "$RDIR/step_${rps}.txt" | sed 's/^/    /'
    sleep $BREAK
  done
done

echo "=== DONE -> $OUT ==="
# Per-round compact summary (single-round runs keep the old summary.txt shape);
# cross-round aggregation with proper stats lives in aggregate_rounds.py.
for (( round=1; round<=ROUNDS; round++ )); do
  RDIR="$OUT"; SUM="$OUT/summary.txt"
  if [ "$ROUNDS" -gt 1 ]; then RDIR="$OUT/round_$round"; SUM="$RDIR/summary.txt"; fi
  printf "%-8s %-12s %-10s %-10s %s\n" offered achieved p50 p99 non2xx > "$SUM"
  for (( rps=START; rps<=END; rps+=STEP )); do
    f="$RDIR/step_${rps}.txt"; [ -f "$f" ] || continue
    ach=$(awk '/^Requests\/sec/{print $2}' "$f")
    p50=$(awk '/^ 50.000%/{print $2}' "$f")
    p99=$(awk '/^ 99.000%/{print $2}' "$f")
    bad=$(awk '/Non-2xx/{print $NF}' "$f"); bad=${bad:-0}
    printf "%-8s %-12s %-10s %-10s %s\n" "$rps" "$ach" "$p50" "$p99" "$bad" >> "$SUM"
  done
  cat "$SUM"
done
if [ "$ROUNDS" -gt 1 ] && [ -x "$DIR/aggregate_rounds.py" ]; then
  python3 "$DIR/aggregate_rounds.py" "$OUT" | tee "$OUT/aggregate.txt" || true
fi
