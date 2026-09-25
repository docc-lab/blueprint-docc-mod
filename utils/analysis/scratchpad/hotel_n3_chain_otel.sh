#!/usr/bin/env bash
# Tomislav-RetCtx: after the ClickHouse probe completes, derive and run the hotel n=3 campaign
# (all five kinds, real-work agents, OTel gateway + ClickHouse, cubic, memcached -c 65536).
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PROBE=$(cat $S/hotel_chprobe_root.txt)
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-20260923T1321Z
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
R=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-n3-$(date -u +%Y%m%dT%H%MZ)
echo $R > $S/hotel_n3_root.txt
cd $REPO/utils
$PY derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $R --kinds nt v pb cgpb sb --repetitions 3 \
  --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates $(seq 1000 1000 24000) --seconds-per-rate 30 \
  --note "HotelReservation SearchHandler n=3, real-work, all mechanisms on (response path, leaf rejection, CPD 2..4, depth_cubic). Agents 500m 256Mi with OTel Helm default thresholds 55/80 (memory_limiter 80/25); OTel gateway 2 CPU 2Gi + ClickHouse 16 CPU store; memcached -c 65536; 1000..24000 step 1000, 30 s; seeds 1001..1003; 5000 uniform trace samples per point."
echo "$(date -u +%H:%M:%S) derived $R"
export RETCTX_TRACE_SAMPLE_WINDOWS=10 RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random
$PY run_dsb_sn_nw.py smoke --out $R
echo "$(date -u +%H:%M:%S) smoke passed"
$PY run_dsb_sn_nw.py run --out $R
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $R"
