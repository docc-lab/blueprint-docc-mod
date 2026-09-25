#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-24 ~21:05Z): HotelReservation ZERO-WORK bridges (pb cgpb sb) RERUN WITHOUT reverse_passthrough,
# on cluster A after the SN no-work chain. Identical to hotelnw_fd2_chain.sh's br group (same FIX images = passthrough-capable
# SDK + census off, same stack, n=5 fresh deployments, rep 1 climbs 10k.. to the kind's plateau, 2..5 re-run its grid, no
# census, no trace capture, cpd 2..4 depth_cubic) minus --reverse-passthrough; checked against the hotel n=1 bridge root.
# nt and v are unaffected by passthrough and stand (retctx-hotelnw-n5-nt / retctx-hotelnw-fd-v).
# usage: setsid nohup hotelnw_nopt_chain.sh > hotelnw_nopt_chain.log 2>&1 < /dev/null &        (DRYRUN=1: derive + verify only)
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff; REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"; DRYRUN=${DRYRUN:-}
FIX=$(cat $S/hotelnw_fix_src_root.txt); D=$(cat $S/collector_queue3_digest); N1=$(cat $S/hotelnw_br_root.txt)
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
if [ -z "$DRYRUN" ]; then
  echo $$ > $S/hotelnw_nopt_chain.pid
  until grep -q "PASSES CHAIN COMPLETE snnw" $S/snnw_nopt_resume.log 2>/dev/null; do
    grep -q "PASSES CHAIN FAILED" $S/snnw_nopt_resume.log 2>/dev/null && fail "SN no-work chain failed; not starting"; sleep 30; done
  echo "$(date -u +%H:%M:%S) SN no-work chain complete; starting the hotel no-work bridge rerun without passthrough"
fi
COMMON="--collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture --plateau-stop --repeat-grid --repetitions 5 --rates $(seq -s ' ' 10000 1000 60000)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
ROOTS=$S/hotelnw_nopt_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$S/hotelnw_nopt_dryrun_roots.txt; : > $ROOTS
cd $REPO/utils
R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-nopt-br-$(date -u +%Y%m%dT%H%M%SZ); [ -n "$DRYRUN" ] && R=$S/dryrun/$(basename $R)
$PY derive_dsb_sn_nw.py --source $FIX --provenance-from $FIX $COMMON $BR --out $R --kinds pb cgpb sb --note "HotelReservation ZERO-WORK bridges WITHOUT reverse_passthrough (rerun of retctx-hotelnw-fd-br): n=5, FRESH deployment per repetition, rep 1 climbs 10k.. to the kind's plateau, reps 2..5 re-run its grid; images with passthrough-capable SDK + census OFF; no trace capture, no census; current best stack, cpd 2..4 depth_cubic. No smoke." > $S/hotelnw_nopt_derive.log 2>&1 || fail "derive (see $S/hotelnw_nopt_derive.log)"
for k in pb cgpb sb; do
  ! grep -q reverse_passthrough $R/builds/$k/manifest.yaml || fail "$k sets reverse_passthrough"
  grep -q GOMEMLIMIT $R/builds/$k/manifest.yaml && grep -q -i clickhouse $R/builds/$k/manifest.yaml || fail "$k stack"
  $PY $H/scripts/compare_to_n1.py $R $N1 $k --new-images || fail "$k differs from its n=1 root"
done
python3 -c "import json,sys; p=json.load(open('$R/plan.json')); sys.exit(0 if p.get('trace_capture') is False and p['repetitions']==5 and p['repeat_grid'] and not p.get('discovery_override') else 1)" || fail "plan protocol"
echo $R >> $ROOTS
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE hotelnw nopt $R"; exit 0; }
echo "$(date -u +%H:%M:%S) run hotelnw pb cgpb sb (no passthrough): 5 fresh-deploy repetitions $R"
$PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_nopt_run.log 2>&1 || fail "run (see $S/hotelnw_nopt_run.log)"
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw bridges n=5 without passthrough"
