#!/bin/bash
set -euo pipefail

###############################################
# wrk2 auto-tuner: find high (but stable) load
#
# Rationale for the approach and thresholds:
# - We increase offered load as a constant rate (R) because wrk2 guarantees
#   a steady request rate independent of response latency, making it ideal
#   for capacity probing without client-side feedback loops.
# - We watch Non-2xx/3xx and socket errors. When the system overloads, queues
#   build, timeouts happen, and servers return errors; keeping these at 0 (or
#   under a tiny bound) ensures we are below the failure knee.
# - We use p99 latency as the stability signal: as we approach saturation,
#   tail latency (p99) grows much faster than median. Constraining p99 keeps
#   the system close to but below the knee of the latency curve.
# - We step R upwards, stopping at the highest R that satisfies both tail
#   latency and error constraints. This finds a practical “high but safe” load.
###############################################

# Defaults (override via env or flags)
# THREADS: start ultra-conservative to validate plumbing first
THREADS=${THREADS:-1}
# CONNS: start with a single connection; increase later as needed
CONNS=${CONNS:-1}
# DURATION: short runs to iterate quickly at first
DURATION=${DURATION:-5s}
# TIMEOUT: generous timeout to avoid client-side aborts in early checks
TIMEOUT=${TIMEOUT:-10s}
# START_R/STEP_R/MAX_R: begin at 1 RPS and ramp slowly
START_R=${START_R:-1}
STEP_R=${STEP_R:-5}
MAX_R=${MAX_R:-5000}
# p99 cap reflects a typical user-facing SLO ballpark for SockShop-like apps
P99_MAX_MS=${P99_MAX_MS:-500}        # max acceptable p99 latency in ms
# Hard error budgets during tuning—if errors occur, we’re past the knee
ERR_MAX=${ERR_MAX:-0}                # max acceptable Non-2xx/3xx
SOCKET_ERR_MAX=${SOCKET_ERR_MAX:-0}  # max acceptable socket errors total
# Target service discovery helpers
NODE_HOST=${NODE_HOST:-"c220g1-031105.wisc.cloudlab.us"}
ENDPOINT=${ENDPOINT:-"/LoadCatalogue"}

usage() {
  cat << EOF
Usage: $(basename "$0") [--url URL] [--endpoint PATH] [--discover]

Options:
  --url URL        Full target URL (overrides --discover/host)
  --endpoint PATH  HTTP path (default: /LoadCatalogue)
  --discover       Auto-discover frontend NodePort and build URL

Env overrides:
  THREADS ($THREADS), CONNS ($CONNS), DURATION ($DURATION), TIMEOUT ($TIMEOUT)
  START_R ($START_R), STEP_R ($STEP_R), MAX_R ($MAX_R)
  P99_MAX_MS ($P99_MAX_MS), ERR_MAX ($ERR_MAX), SOCKET_ERR_MAX ($SOCKET_ERR_MAX)
  NODE_HOST ($NODE_HOST)

Examples:
  $(basename "$0") --discover
  ENDPOINT="/ListItems?pageSize=100&pageNum=1" $(basename "$0") --discover
  $(basename "$0") --url "http://localhost:18080/LoadCatalogue"
EOF
}

URL=""
DISCOVER=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --url) URL="$2"; shift 2;;
    --endpoint) ENDPOINT="$2"; shift 2;;
    --discover) DISCOVER=1; shift;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1"; usage; exit 1;;
  esac
done

if [[ $DISCOVER -eq 1 && -z "$URL" ]]; then
  echo "[INFO] Discovering frontend NodePort..."
  if ! command -v kubectl >/dev/null 2>&1; then
    echo "[ERROR] kubectl not found for discovery. Provide --url instead."; exit 1
  fi
  NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[?(@.port==12349)].nodePort}' 2>/dev/null || true)
  if [[ -z "${NODEPORT:-}" ]]; then
    echo "[ERROR] Could not discover NodePort for frontend-ctr port 12349"; exit 1
  fi
  URL="http://$NODE_HOST:$NODEPORT$ENDPOINT"
  echo "[INFO] Using discovered URL: $URL"
fi

if [[ -z "$URL" ]]; then
  echo "[ERROR] No URL provided. Use --url or --discover"; usage; exit 1
fi

WRK_BIN="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/wrk"
if [[ ! -x "$WRK_BIN" ]]; then
  echo "[ERROR] wrk binary not found at $WRK_BIN. Build wrk2 first."; exit 1
fi

echo "=========================================="
echo "==== wrk2 auto-tune: $URL ===="
echo "THREADS=$THREADS CONNS=$CONNS DURATION=$DURATION TIMEOUT=$TIMEOUT"
echo "START_R=$START_R STEP_R=$STEP_R MAX_R=$MAX_R"
echo "P99_MAX_MS=$P99_MAX_MS ERR_MAX=$ERR_MAX SOCKET_ERR_MAX=$SOCKET_ERR_MAX"
echo "=========================================="

best_r=0

run_once() {
  local rps=$1
  echo "\n[RUN] R=$rps"
  # Allow local run without installing LuaJIT systemwide
  local LD_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/deps/luajit/src"
  set +e
  OUT=$(LD_LIBRARY_PATH="$LD_PATH:${LD_LIBRARY_PATH:-}" \
        "$WRK_BIN" -t"$THREADS" -c"$CONNS" -d"$DURATION" -R"$rps" \
        --timeout "$TIMEOUT" --latency "$URL" 2>&1)
  rc=$?
  set -e
  echo "$OUT"
  if [[ $rc -ne 0 ]]; then
    echo "[WARN] wrk exited with rc=$rc"; return 1
  fi

  # Parse Non-2xx/3xx: server-side failures indicate overload or bugs
  non2xx=$(echo "$OUT" | awk '/Non-2xx or 3xx/ {print $4+0}' | tail -1)
  non2xx=${non2xx:-0}

  # Parse socket errors total: client-side transport failures (connect/read/write/timeouts)
  sock_err=$(echo "$OUT" | awk '/Socket errors/ {gsub(","," "); for(i=1;i<=NF;i++){ if($i~/connect|read|write|timeouts/){split($i,a,":"); s+=a[2]+0 }} } END{print s+0}')
  sock_err=${sock_err:-0}

  # Parse p99 latency (assumes wrk2 summary); tail latency is a sensitive overload indicator
  p99=$(echo "$OUT" | awk '/\s*99%/ {print $2}' | tail -1)
  # Convert to ms if in us or s
  p99_ms=$p99
  if [[ "$p99" == *ms ]]; then p99_ms=${p99%ms};
  elif [[ "$p99" == *us ]]; then v=${p99%us}; p99_ms=$(awk -vv="$v" 'BEGIN{printf "%.3f", v/1000}');
  elif [[ "$p99" == *s ]]; then v=${p99%s}; p99_ms=$(awk -vv="$v" 'BEGIN{printf "%.3f", v*1000}');
  fi

  echo "[PARSE] non2xx=$non2xx socket_errors=$sock_err p99_ms=$p99_ms"

  # Evaluate
  if (( non2xx > ERR_MAX )); then echo "[FAIL] Non-2xx=$non2xx > $ERR_MAX"; return 1; fi
  if (( sock_err > SOCKET_ERR_MAX )); then echo "[FAIL] SocketErrors=$sock_err > $SOCKET_ERR_MAX"; return 1; fi
  awk -v x="$p99_ms" -v m="$P99_MAX_MS" 'BEGIN{exit !(x<=m)}' || { echo "[FAIL] p99_ms=$p99_ms > $P99_MAX_MS"; return 1; }

  return 0
}

R=$START_R
while (( R<=MAX_R )); do
  if run_once "$R"; then
    best_r=$R
    R=$(( R + STEP_R ))
  else
    break
  fi
done

echo "\n=========================================="
if (( best_r > 0 )); then
  echo "[RESULT] Recommended RPS: $best_r"
  echo "[RESULT] Command:"
  echo "  LD_LIBRARY_PATH=\"$(cd \"$(dirname \"${BASH_SOURCE[0]}\")\" && pwd)/deps/luajit/src:\${LD_LIBRARY_PATH:-}\" \\
  $WRK_BIN -t$THREADS -c$CONNS -d$DURATION -R$best_r --timeout $TIMEOUT --latency \"$URL\""
else
  echo "[RESULT] No stable R found under constraints. Try relaxing P99_MAX_MS or error thresholds, or lower START_R."
fi
echo "=========================================="


