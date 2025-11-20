#!/bin/bash
set -e   # exit on error

WRK="../wrk2/wrk"
DURATION=60

mkdir -p wrk_logs perf_logs

for R in 1000 1500 2000 2500 3000; do
  echo "=== Testing R=$R ==="

  # 1) Start perf on node3
  ssh node-3 "sudo perf stat -I 1000 -a --per-socket -x, \
      -e power/energy-pkg/ \
      -- sleep ${DURATION} 2> perf_node3_R${R}.txt" &

  # 2) Run wrk2 from node0, capture *both* stdout and stderr
  $WRK -t8 -c80 -d${DURATION}s -R${R} --timeout 10s \
      'http://10.10.1.1:17313/ListItems?pageSize=100&pageNum=1' \
      2>&1 | tee "wrk_logs/wrk_R${R}.txt"

  # 3) Wait for perf to finish
  wait

  # 4) Copy perf file back from node3 to node0
  scp node-3:perf_node3_R${R}.txt "perf_logs/perf_node3_R${R}.txt"
done
