#!/bin/bash

# Simple performance harness:
# 0) (optional) sync node clock
# 1) start perf stat (package/core/DRAM energy) for PRE+LOAD+POST window
# 2) wait PRE seconds (baseline)
# 3) drive wrk2 load for LOAD seconds
# 4) wait POST seconds (cool-down), stop perf
# Output:
#   * wrk results printed to stdout and saved to wrk.log
#   * power samples saved to power.log

set -euo pipefail

WRK_BIN="$HOME/blueprint-docc-mod/wrk2/wrk"
TARGET_URL="${TARGET_URL:-http://10.10.1.1:17313/ListItems?pageSize=10&pageNum=1}"
THREADS="${THREADS:-24}"
CONNECTIONS="${CONNECTIONS:-512}"
LOAD_DURATION="${LOAD_DURATION:-30}"   # seconds for wrk2
PRE_DELAY="${PRE_DELAY:-5}"
POST_DELAY="${POST_DELAY:-5}"
RATE="${RATE:-75000}"                  # requests per second (tune for high CPU)
SYNC_CLOCK="${SYNC_CLOCK:-false}"
NTP_SERVER="${NTP_SERVER:-pool.ntp.org}"

if [ ! -x "$WRK_BIN" ]; then
  echo "[ERROR] wrk binary not found or not executable at $WRK_BIN"
  exit 1
fi

if ! command -v perf >/dev/null 2>&1; then
  echo "[ERROR] perf not found. Install perf (linux-tools-common/linux-tools-<kernel>)."
  exit 1
fi

if [ "$SYNC_CLOCK" = "true" ]; then
  echo "[INFO] Syncing clock with $NTP_SERVER"
  if command -v ntpdate >/dev/null 2>&1; then
    sudo ntpdate -u "$NTP_SERVER"
  else
    echo "[WARNING] ntpdate not installed; skipping clock sync"
  fi
fi

echo "[INFO] Running wrk2 load for ${LOAD_DURATION}s at ${RATE} rps against ${TARGET_URL}"
echo "[INFO] Threads=${THREADS}, Connections=${CONNECTIONS}"
echo "[INFO] PRE delay=${PRE_DELAY}s, POST delay=${POST_DELAY}s"
echo "[INFO] Sampling power every second with perf stat (requires sudo)"

POWER_LOG="${POWER_LOG:-power.log}"
WRK_LOG="${WRK_LOG:-wrk.log}"
TOTAL_PERF_TIME=$((PRE_DELAY + LOAD_DURATION + POST_DELAY))

# Start perf stat sampling in the background (includes pre+load+post windows)
sudo perf stat -a -I 1000 \
  -e power/energy-pkg/ \
  -e power/energy-cores/ \
  -e power/energy-ram/ \
  sleep "$TOTAL_PERF_TIME" \
  >"$POWER_LOG" 2>&1 &
PERF_PID=$!

# PRE-delay (baseline sampling)
sleep "$PRE_DELAY"

# Run wrk2 workload (foreground)
"$WRK_BIN" \
  -t"$THREADS" \
  -c"$CONNECTIONS" \
  -d"${LOAD_DURATION}s" \
  -R"$RATE" \
  --latency \
  "$TARGET_URL" | tee "$WRK_LOG"

# POST-delay (cool-down)
sleep "$POST_DELAY"

# Wait for perf to finish
wait "$PERF_PID"

echo "[INFO] Test complete."
echo "[INFO] wrk output saved to $WRK_LOG"
echo "[INFO] Power samples saved to $POWER_LOG"
