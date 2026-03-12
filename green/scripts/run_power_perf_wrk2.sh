#!/usr/bin/env bash
set -euo pipefail

# ---- CONFIG (edit these) ----
NODE0_HOST="node-0"                   # node0 hostname (or "localhost")
REMOTE_NODES=("node-2" "node-3")      # nodes to run perf on too
ALL_NODES=("$NODE0_HOST" "${REMOTE_NODES[@]}")

# Number of experiment rounds (set to 10, 20, 30, 50, ...)
NUM_RUNS=100

# Time sync (tries chrony first, then ntpdate)
CLOCK_SYNC_CMD='sudo chronyc -a makestep || sudo ntpdate -u pool.ntp.org'

# perf: 1Hz RAPL energy (all sockets aggregated). 70 samples ~= 70s.
PERF_STAT_CMD='sudo perf stat -I 1000 -a -x, -e power/energy-pkg/ -- sleep 70'

# wrk2 (absolute path to your binary)
WRK2_CMD='/users/maxLliu/wrk -t8 -c80 -d60s -R1200 --latency "http://10.10.1.1:23688/ListItems?pageSize=100&pageNum=1"'

# -----------------------------

run_local_or_ssh() {
  local host="$1"
  local cmd="$2"
  if [[ "$host" == "$NODE0_HOST" || "$host" == "localhost" ]]; then
    bash -lc "$cmd"
  else
    ssh "$host" "$cmd"
  fi
}

echo "[0/5] Syncing clocks on all nodes (once before all runs)"
for host in "${ALL_NODES[@]}"; do
  echo "  - $host"
  run_local_or_ssh "$host" "$CLOCK_SYNC_CMD"
done

for ((iter = 1; iter <= NUM_RUNS; iter++)); do
  RUN_LABEL="run_${iter}"
  REMOTE_RUN_DIR="/tmp/${RUN_LABEL}"
  LOCAL_OUT_DIR="${HOME}/power_runs/${RUN_LABEL}"

  echo ""
  echo "=============================="
  echo "Starting ${RUN_LABEL} (${iter}/${NUM_RUNS})"
  echo "=============================="

  echo "[1/5] Creating run dir on all nodes: $REMOTE_RUN_DIR"
  for host in "${ALL_NODES[@]}"; do
    # Optional: clear old data if reusing run_X names
    run_local_or_ssh "$host" "rm -rf '$REMOTE_RUN_DIR'; mkdir -p '$REMOTE_RUN_DIR'"
  done

  echo "[2/5] Starting perf stat (70s) on all nodes"
  for host in "${ALL_NODES[@]}"; do
    # perf stat writes to stderr; we redirect both to a file
    cmd="cd '$REMOTE_RUN_DIR' && ( $PERF_STAT_CMD ) > perf_power.csv 2>&1"
    if [[ "$host" == "$NODE0_HOST" || "$host" == "localhost" ]]; then
      bash -lc "nohup bash -lc \"$cmd\" >/dev/null 2>&1 &"
    else
      ssh "$host" "nohup bash -lc \"$cmd\" >/dev/null 2>&1 &"
    fi
  done

  echo "[3/5] Waiting 5s, then running wrk2 for 60s on node0"
  sleep 5
  run_local_or_ssh "$NODE0_HOST" "cd '$REMOTE_RUN_DIR' && ( $WRK2_CMD ) | tee wrk2.log"

  echo "[4/5] Waiting 5s, then collecting data back to node0"
  sleep 5

  mkdir -p "$LOCAL_OUT_DIR"

  # Copy perf + wrk2 from node-0 (local) and flatten
  cp "${REMOTE_RUN_DIR}/perf_power.csv" "${LOCAL_OUT_DIR}/node-0_perf.csv"
  cp "${REMOTE_RUN_DIR}/wrk2.log"       "${LOCAL_OUT_DIR}/wrk2.log"

  # Copy perf from node-2 and flatten
  scp "node-2:${REMOTE_RUN_DIR}/perf_power.csv" "${LOCAL_OUT_DIR}/node-2_perf.csv"

  # Copy perf from node-3 and flatten
  scp "node-3:${REMOTE_RUN_DIR}/perf_power.csv" "${LOCAL_OUT_DIR}/node-3_perf.csv"

  echo "[5/5] ${RUN_LABEL} complete. Collected under: $LOCAL_OUT_DIR"
  echo "       Expect per node:"
  echo "         - perf_power.csv (perf stat -I 1000 output)"
  echo "         - wrk2.log (only on node0)"
done

echo ""
echo "All ${NUM_RUNS} runs completed. Results under: ${HOME}/power_runs/run_1, run_2, ..."