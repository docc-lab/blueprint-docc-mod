#!/bin/bash
set -euo pipefail

########################################
# Config
########################################

# Where wrk2 binary lives (relative to this script)
WRK_BIN="../wrk2/wrk"

# Target endpoint (frontend on node-3)
TARGET_URL="http://10.10.1.1:17313/ListItems?pageSize=100&pageNum=1"

# wrk2 parameters
T=8          # threads
C=80         # connections
R=2000       # target RPS
WL_DURATION=60   # workload duration (seconds)

# perf measurement window: 5s before + 60s workload + 5s after
MEASURE_DURATION=70
WARMUP_BEFORE=5   # seconds to wait after starting perf, before starting wrk

# Nodes to measure (controller is node-0)
NODES=("node-0" "node-2" "node-3")

# Default number of iterations; can be overridden with -n
ITERATIONS=10

########################################
# Optional: clock-sync helper (manual)
########################################
# If you have NTP tools and sudo, you could uncomment this and call sync_clocks
# before the loop.
#
# sync_clocks() {
#   for host in "${NODES[@]}"; do
#     echo "Syncing clock on ${host} (if ntpdate/chronyc available)..."
#     ssh "${host}" "sudo ntpdate -u pool.ntp.org || sudo chronyc makestep || echo '  [WARN] clock sync failed/skipped on ${host}'"
#   done
# }

########################################
# Parse flags
########################################

usage() {
  echo "Usage: $0 [-n NUM_ITERATIONS]"
  echo "  -n NUM   Number of experiment iterations (default: ${ITERATIONS})"
  exit 1
}

while getopts ":n:h" opt; do
  case "$opt" in
    n)
      ITERATIONS="$OPTARG"
      ;;
    h|*)
      usage
      ;;
  esac
done

########################################
# Prep
########################################

mkdir -p wrk_logs perf_logs

echo "wrk2 binary:      ${WRK_BIN}"
echo "Target URL:       ${TARGET_URL}"
echo "wrk2 params:      -t ${T} -c ${C} -R ${R} -d ${WL_DURATION}s"
echo "Perf duration:    ${MEASURE_DURATION}s (with ${WARMUP_BEFORE}s pre-workload)"
echo "Nodes measured:   ${NODES[*]}"
echo "Iterations:       ${ITERATIONS}"
echo

########################################
# Main loop
########################################

for ((i=1; i<=ITERATIONS; i++)); do
  iter_tag=$(printf "%03d" "$i")
  echo "==============================="
  echo "=== Iteration ${iter_tag}/${ITERATIONS} ==="
  echo "==============================="

  #
  # 1) Start perf on all nodes in the background
  #
  for host in "${NODES[@]}"; do
    echo "  [${iter_tag}] Starting perf on ${host}..."
    ssh "${host}" "sudo perf stat -I 1000 -a --per-socket -x, \
        -e power/energy-pkg/ \
        -- sleep ${MEASURE_DURATION} 2> perf_node_${host}_iter${iter_tag}.txt" &
  done

  #
  # 2) Give perf a head start
  #
  echo "  [${iter_tag}] Sleeping ${WARMUP_BEFORE}s before starting workload..."
  sleep "${WARMUP_BEFORE}"

  #
  # 3) Run wrk2 on node-0
  #
  echo "  [${iter_tag}] Running wrk2 workload..."
  "${WRK_BIN}" -t"${T}" -c"${C}" -d"${WL_DURATION}s" -R"${R}" --timeout 10s \
    "${TARGET_URL}" \
    > "wrk_logs/wrk_iter${iter_tag}.txt" 2>&1

  #
  # 4) Wait for all perf processes to finish
  #
  echo "  [${iter_tag}] Waiting for perf to finish on all nodes..."
  wait

  #
  # 5) Copy perf logs back to node-0
  #
  for host in "${NODES[@]}"; do
    remote_file="perf_node_${host}_iter${iter_tag}.txt"
    local_file="perf_logs/perf_${host}_iter${iter_tag}.txt"
    echo "  [${iter_tag}] Copying ${remote_file} from ${host} -> ${local_file}"
    scp "${host}:${remote_file}" "${local_file}"
  done

  echo "  [${iter_tag}] Iteration complete."
  echo
done

echo "All ${ITERATIONS} iterations completed."
echo "wrk logs:  $(realpath wrk_logs)"
echo "perf logs: $(realpath perf_logs)"
