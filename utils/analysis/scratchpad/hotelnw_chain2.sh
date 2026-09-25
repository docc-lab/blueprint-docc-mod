#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK sweep, continued (user 2026-09-24: the knee is where throughput stops
# growing, not "two points below 97 % of offered"). Watches the already-running no-tracing runner and stops it once
# 3 consecutive points do not beat the best delivered rate by >= 1 %; then v, pb, cgpb, sb from 1000 to that rate.
# Same stack as hotelnw_chain.sh (hotel opt2 best config on ClickHouse, cpd 2..4 depth_cubic, GOGC=off GOMEMLIMIT=1GiB).
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/hotelnw_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt)
D=$(cat $S/collector_queue3_digest)
BASE="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
NOTE="HotelReservation ZERO-WORK (workflow/hotelnw), SDK at 79e9e9fa, hotel opt2 best config on ClickHouse, cpd 2..4 depth_cubic, GOGC=off GOMEMLIMIT=1GiB gctrace; 1k steps. No smoke."
cd /users/tomislav/blueprint-docc-mod/utils
verify() { for m in $1/builds/*/manifest.yaml; do grep -q -i clickhouse $m && ! grep -q -i elasticsearch $m && grep -q GOMEMLIMIT $m || { echo "CHAIN FAILED: $m stack check"; exit 1; }; done; }
N=$(cat $S/hotelnw_nt_root.txt)
RP=$(pgrep -f "[r]un_dsb_sn_nw.py run --out $N" | head -1 || true)
echo "$(date -u +%H:%M:%S) plateau rule watching hotelnw nt (runner $RP)"
KNEE=""
while [ -n "$RP" ] && kill -0 $RP 2>/dev/null; do
  KNEE=$(python3 - $N <<'PYK'
import sys, json, glob
best, flat = 0.0, 0
for f in sorted(glob.glob(sys.argv[1] + '/run/01-nt/rate-*/result.json')):
    d = json.load(open(f))
    if 'collector_deltas' not in d: continue
    if d['completed_rps'] >= best * 1.01: best, flat = d['completed_rps'], 0
    else: flat += 1
    if flat >= 3: print(d['offered_rps']); break
PYK
)
  [ -n "$KNEE" ] && break
  sleep 10
done
RO=$(pgrep -f "[r]un_only.sh $N" || true)
[ -n "$RP" ] && kill -TERM $RP 2>/dev/null || true; sleep 3; kill -KILL $RO $RP 2>/dev/null || true
if [ -z "$KNEE" ]; then KNEE=60000; fi
python3 - $N $KNEE <<'PYC'
import sys, json, datetime
N, K = sys.argv[1], sys.argv[2]
json.dump({'finished': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'case': 'nt', 'kind': 'nt', 'stopped_by_user': True,
           'note': f'stopped by the plateau rule (user 2026-09-24): 3 consecutive points without >= 1 pct more throughput, confirmed at {K}'},
          open(f'{N}/run/01-nt/complete.json', 'w'), indent=2)
p = f'{N}/run-status.json'; d = json.load(open(p)); d['state'] = 'stopped'; d['note'] = f'plateau confirmed at {K}'; json.dump(d, open(p, 'w'), indent=2)
PYC
echo "$(date -u +%H:%M:%S) hotelnw nt plateau confirmed at $KNEE"
V=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-mem-v-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $BASE --rates $(seq -s ' ' 1000 1000 $KNEE) --out $V --kinds v --note "$NOTE ceiling = nt plateau $KNEE" > $S/hotelnw_v_derive.log 2>&1
verify $V; echo $V > $S/hotelnw_v_root.txt
sleep 1
BB=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-mem-br-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $BASE $BR --rates $(seq -s ' ' 1000 1000 $KNEE) --out $BB --kinds pb cgpb sb --note "$NOTE ceiling = nt plateau $KNEE" > $S/hotelnw_br_derive.log 2>&1
verify $BB; for k in pb cgpb sb; do grep -q "reverse_policy: depth_cubic" $BB/builds/$k/manifest.yaml || { echo "CHAIN FAILED: $k not depth_cubic"; exit 1; }; done
echo $BB > $S/hotelnw_br_root.txt
echo "$(date -u +%H:%M:%S) run hotelnw v $V then pb cgpb sb $BB"
bash $S/run_only.sh $V
bash $S/run_only.sh $BB
echo "$(date -u +%H:%M:%S) HOTELNW CHAIN COMPLETE"
