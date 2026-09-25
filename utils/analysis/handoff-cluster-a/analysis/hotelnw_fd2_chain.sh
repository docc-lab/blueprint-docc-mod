#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK performance sweep, traced kinds, n=5, FRESH DEPLOYMENT PER PASS, NO CENSUS
# (user 2026-09-24: "not do any census stuff during these runs anymore ... leave loss eval for a different experiment").
# Images: A's tree = passthrough SDK + bounded refused-trace census, census OFF by default (refused batches only add span
# counters) -- source root in scratchpad hotelnw_fix_src_root.txt. Runner: no /retctx/refused log download (default), no
# per-point trace sampling (--no-trace-capture), no trace_census.py. Collector counters are still snapshotted.
#  1. v: 5 fresh-deployment repetitions; rep 1 climbs 10k.. to v's plateau (3 points without >= 1 % gain), reps 2..5 re-run
#     its grid; seeds 1001..1005.
#  2. PB, CGPB, SB with reverse_passthrough: same protocol, kinds rotated per repetition.
# nt: its 5 back-to-back passes stand (untraced: no SDK state; its passes are flat). Every root must match its n=1 root
# (compare_to_n1.py: stack/plan/manifest identical except rates, repetitions, passthrough, app image digests).
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff; REPO=/users/tomislav/blueprint-docc-mod
echo $$ > $S/hotelnw_fd2_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
FIX=$(cat $S/hotelnw_fix_src_root.txt)
grep -q "HOTELNW FIX BUILD COMPLETE $FIX" $S/hotelnw_fix_build_chain.log || { echo "PASSES CHAIN FAILED: fix build not complete"; exit 1; }
D=$(cat $S/collector_queue3_digest)
COMMON="--collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture --plateau-stop --repeat-grid --repetitions 5 --rates $(seq -s ' ' 10000 1000 60000)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-passthrough"
ROOTS=$S/hotelnw_fd2_roots.txt; : > $ROOTS
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
cd $REPO/utils
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for group in v br; do
  R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-fd-$group-$STAMP
  if [ $group = br ]; then KINDS="pb cgpb sb"; EXTRA=$BR; N1=$(cat $S/hotelnw_br_root.txt); FLAGS="--passthrough --new-images"
  else KINDS=v; EXTRA=""; N1=$(cat $S/hotelnw_v_root.txt); FLAGS="--new-images"; fi
  $PY derive_dsb_sn_nw.py --source $FIX --provenance-from $FIX $COMMON $EXTRA --out $R --kinds $KINDS --note "HotelReservation ZERO-WORK, $KINDS: n=5, FRESH deployment per repetition, rep 1 climbs 10k.. to the kind's plateau, reps 2..5 re-run its grid; images with passthrough SDK + census OFF; no trace capture, no census; current best stack, cpd 2..4 depth_cubic. No smoke." > $S/hotelnw_fd2_${group}_derive.log 2>&1 || fail "derive $group"
  for k in $KINDS; do $PY $H/scripts/compare_to_n1.py $R $N1 $k $FLAGS || fail "$k differs from its n=1 root"; done
  python3 -c "import json,sys; p=json.load(open('$R/plan.json')); sys.exit(0 if p.get('trace_capture') is False and p['repetitions']==5 and p['repeat_grid'] else 1)" || fail "$group plan protocol"
  echo $R >> $ROOTS
  echo "$(date -u +%H:%M:%S) run hotelnw $KINDS: 5 fresh-deploy repetitions, no census $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_fd2_${group}_run.log 2>&1 || fail "run $group (see $S/hotelnw_fd2_${group}_run.log)"
  echo "$(date -u +%H:%M:%S) done hotelnw $KINDS"
done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw fresh-deploy n=5 (v, pb cgpb sb passthrough), no census"
