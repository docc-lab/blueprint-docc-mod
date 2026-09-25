#!/usr/bin/env bash
# Tomislav-RetCtx: hotel n=3 on the pre-decode collector set (user-approved collector work 2026-09-23).
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-20260923T1321Z
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
until [ -f $(cat $S/hotel_predecode_root.txt)/run-complete.json ]; do
  st=$(python3 -c "import json;print(json.load(open('$(cat $S/hotel_predecode_root.txt)/run-status.json'))['state'])" 2>/dev/null || echo unknown)
  [ "$st" = failed ] && { echo "CHAIN ABORTED: probe failed"; exit 1; }
  sleep 15
done
echo "$(date -u +%H:%M:%S) probe complete"
R=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-n3pd-$(date -u +%Y%m%dT%H%MZ)
echo $R > $S/hotel_n3_root.txt
cd $REPO/utils
$PY derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $R --kinds nt v pb cgpb sb --repetitions 3 \
  --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates $(seq 1000 1000 24000) --seconds-per-rate 30 \
  --collector-image 10.10.1.1:30000/otelcontribcol@sha256:6f56145875453477899d40473d9fff68b57a6632038d84d28e6ecd38fdd685ee --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted \
  --priority-receiver --cpu-shed-threshold 0.9 --gateway-cpu-shed-threshold 0.9 \
  --note "HotelReservation SearchHandler n=3, real-work, all mechanisms on, cubic, CPD 2..4. Collectors: 500m 256Mi agents 55/80, 2-CPU gateway, GOMAXPROCS = CPU limit, priority backlog margin, gateway LP refusals non-retryable, bridges use the priorityotlp pre-decode receiver with cpu_shed_threshold 0.9 on agents and gateway (image prio-predecode-20260923). ClickHouse store; memcached -c 65536; 1000..24000 step 1000, 30 s; seeds 1001..1003; 5000 uniform trace samples per point."
echo "$(date -u +%H:%M:%S) derived $R"
export RETCTX_TRACE_SAMPLE_WINDOWS=10 RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random
$PY run_dsb_sn_nw.py smoke --out $R
echo "$(date -u +%H:%M:%S) smoke passed"
$PY run_dsb_sn_nw.py run --out $R
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $R"
