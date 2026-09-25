#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK to a complete n=5 (user 2026-09-24: "no-work hotelres, 10k thru knee ...
# however many more trials until we have a complete n=5"). The n=1 sweep (retctx-hotelnw-mem-{nt,v,br}) is pass 1; this
# adds passes 2..5 per kind, back to back in ONE deployment per kind (no redeploy between passes, 60 s gap), over
# 10k..that kind's plateau stop in the n=1 data (3 consecutive points without >= 1 % gain): nt 41k, v 33k, pb 31k,
# cgpb 30k, sb 29k. Every derive flag is the n=1 sweep's (hotelnw_chain.sh / hotelnw_chain2.sh); each root's plan must
# equal its n=1 root's except rates / passes before it deploys.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff
echo $$ > $S/hotelnw_n5_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt)
D=$(cat $S/collector_queue3_digest)
BASE="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --ramp-passes 4 --first-pass 2 --pass-gap-seconds 60"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
NOTE="HotelReservation ZERO-WORK (workflow/hotelnw), SDK at 79e9e9fa, hotel opt2 best config on ClickHouse, cpd 2..4 depth_cubic, GOGC=off GOMEMLIMIT=1GiB gctrace; passes 2..5 (n=5 with the n=1 sweep as pass 1), 10k..plateau stop, one deployment per kind, 60 s between passes. No smoke."
ROOTS=$S/hotelnw_n5_roots.txt; : > $ROOTS
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
cd /users/tomislav/blueprint-docc-mod/utils
setsid nohup python3 -u $H/scripts/trace_census.py $ROOTS --until-file $S/hotelnw_n5.done > $S/hotelnw_n5_census.log 2>&1 < /dev/null &
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for spec in nt:41000:$(cat $S/hotelnw_nt_root.txt) v:33000:$(cat $S/hotelnw_v_root.txt) pb:31000:$(cat $S/hotelnw_br_root.txt) cgpb:30000:$(cat $S/hotelnw_br_root.txt) sb:29000:$(cat $S/hotelnw_br_root.txt); do
  k=${spec%%:*}; rest=${spec#*:}; top=${rest%%:*}; N1=${rest#*:}
  EXTRA=""; case $k in pb|cgpb|sb) EXTRA=$BR;; esac
  R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-n5-$k-$STAMP
  $PY derive_dsb_sn_nw.py $BASE $EXTRA --rates $(seq -s ' ' 10000 1000 $top) --out $R --kinds $k --note "$NOTE" > $S/hotelnw_n5_${k}_derive.log 2>&1 || fail "derive $k"
  python3 - $R $N1 $k <<'PYV' || fail "$k plan/manifest differs from its n=1 root $N1"
import sys, json, re
R, N1, k = sys.argv[1:]
a, b = json.load(open(f'{N1}/plan.json')), json.load(open(f'{R}/plan.json'))
skip = {'created', 'note', 'ramp_rates', 'raw_data_storage', 'ramp_passes', 'plateau_stop', 'knee_windows', 'derived_from', 'cases', 'case_order_note'}
diff = {x: (a.get(x), b.get(x)) for x in set(a) | set(b) if x not in skip and a.get(x) != b.get(x)}
assert not diff, diff
norm = lambda t: re.sub(r'x2026\d{4}t\d{4,6}z|2026\d{4}T\d{6}Z|/storage/tomislav-retctx-e2e/[^\s"]+', 'S', t)
assert norm(open(f'{N1}/builds/{k}/manifest.yaml').read()) == norm(open(f'{R}/builds/{k}/manifest.yaml').read()), 'manifest'
assert b['ramp_passes'] == {'passes': 4, 'gap_seconds': 60, 'first_pass': 2, 'grid': b['ramp_passes']['grid']} and not b.get('plateau_stop')
print(f'{k}: plan + manifest identical to n=1 ({N1.split("/")[-1]}); rates {b["ramp_rates"][0]}..{b["ramp_rates"][-1]}')
PYV
  echo $R >> $ROOTS
  echo "$(date -u +%H:%M:%S) run hotelnw $k passes 2..5 10000..$top $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_n5_${k}_run.log 2>&1 || fail "run $k (see $S/hotelnw_n5_${k}_run.log)"
  echo "$(date -u +%H:%M:%S) done hotelnw $k"
done
touch $S/hotelnw_n5.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw n5"
