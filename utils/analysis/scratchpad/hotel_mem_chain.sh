#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation real-work n=1 sweep on the SAME stack as the SN ClickHouse+memlimit round (user 2026-09-24):
# hotel opt2 best config (ClickHouse gateway 2 CPU, admissionotel500m agents, queue3 image, GOMAXPROCS auto, backlog margin,
# gateway LP resource_exhausted, agents compression none; bridges: priority receiver, SDK HP retry, queue stage agents+gw),
# hotel checkpoint policy (dsb_apps: cpd 2..4 depth_cubic), optimized SDK images (opt2src = 5f021f53-equivalent),
# GOGC=off GOMEMLIMIT=1GiB gctrace on every service. 1k steps. nt first, auto-stopped two points past its knee
# (two consecutive points below 97 % of offered); then v, pb, cgpb, sb from 1000 to nt's knee rate. No smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/hotel_mem_chain.pid
until grep -q "SNNW MEM CHAIN COMPLETE" $S/snnw_mem_chain.log; do sleep 10; done
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-opt2src-20260923T2350Z
D=$(cat $S/collector_queue3_digest)
BASE="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
NOTE="HotelReservation real-work, optimized SDK (opt2src), hotel opt2 best config on ClickHouse, cpd 2..4 depth_cubic, GOGC=off GOMEMLIMIT=1GiB gctrace; 1k steps. No smoke."
cd /users/tomislav/blueprint-docc-mod/utils
verify() { for m in $1/builds/*/manifest.yaml; do grep -q -i clickhouse $m && ! grep -q -i elasticsearch $m && grep -q GOMEMLIMIT $m || { echo "CHAIN FAILED: $m stack check"; exit 1; }; done; }
N=/users/tomislav/deployments/dsb-hotel/retctx-hotel-mem-nt-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $BASE --rates $(seq -s ' ' 1000 1000 40000) --out $N --kinds nt --note "$NOTE" > $S/hotel_mem_nt_derive.log 2>&1
verify $N; echo $N > $S/hotel_mem_nt_root.txt
echo "$(date -u +%H:%M:%S) run hotel nt $N"
bash $S/run_only.sh $N &
RUNPID=$!
KNEE=""
while kill -0 $RUNPID 2>/dev/null; do
  KNEE=$(python3 - $N <<'PYK'
import sys, json, glob
pts = []
for f in sorted(glob.glob(sys.argv[1] + '/run/01-nt/rate-*/result.json')):
    d = json.load(open(f))
    if 'collector_deltas' in d: pts.append((d['offered_rps'], d['completed_rps'] / d['offered_rps']))
for i in range(1, len(pts)):
    if pts[i][1] < 0.97 and pts[i - 1][1] < 0.97:
        print(pts[i - 1][0]); break
PYK
)
  [ -n "$KNEE" ] && break
  sleep 10
done
if [ -n "$KNEE" ]; then
  RP=$(pgrep -f "[r]un_dsb_sn_nw.py run --out $N" || true); RO=$(pgrep -f "[r]un_only.sh $N" || true)
  [ -n "$RP" ] && kill -TERM $RP; sleep 3; kill -KILL $RO $RP 2>/dev/null || true
  python3 -c "
import json, datetime
json.dump({'finished': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'case': 'nt', 'kind': 'nt', 'stopped_by_user': True,
 'note': 'auto-stopped two points past the knee (user 2026-09-24: stop no-tracing two points after its knee); knee rate $KNEE'}, open('$N/run/01-nt/complete.json', 'w'), indent=2)"
  echo "$(date -u +%H:%M:%S) hotel nt knee at $KNEE (stopped two points past it)"
else
  KNEE=40000; echo "$(date -u +%H:%M:%S) hotel nt: no knee up to 40000"
fi
V=/users/tomislav/deployments/dsb-hotel/retctx-hotel-mem-v-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $BASE --rates $(seq -s ' ' 1000 1000 $KNEE) --out $V --kinds v --note "$NOTE ceiling = nt knee $KNEE" > $S/hotel_mem_v_derive.log 2>&1
verify $V; echo $V > $S/hotel_mem_v_root.txt
sleep 1
BB=/users/tomislav/deployments/dsb-hotel/retctx-hotel-mem-br-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $BASE $BR --rates $(seq -s ' ' 1000 1000 $KNEE) --out $BB --kinds pb cgpb sb --note "$NOTE ceiling = nt knee $KNEE" > $S/hotel_mem_br_derive.log 2>&1
verify $BB; for k in pb cgpb sb; do grep -q "reverse_policy: depth_cubic" $BB/builds/$k/manifest.yaml || { echo "CHAIN FAILED: $k not depth_cubic"; exit 1; }; done
echo $BB > $S/hotel_mem_br_root.txt
echo "$(date -u +%H:%M:%S) run hotel v $V then pb cgpb sb $BB"
bash $S/run_only.sh $V
bash $S/run_only.sh $BB
echo "$(date -u +%H:%M:%S) HOTEL MEM CHAIN COMPLETE"
