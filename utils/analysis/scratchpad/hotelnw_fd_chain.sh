#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK, traced kinds with a FRESH DEPLOYMENT PER PASS (2026-09-24: back-to-back
# passes are contaminated for traced kinds -- the frontend's live heap grows over overloaded pass tails, 90 -> 931 MB by
# pass 5 against GOMEMLIMIT 1GiB, and the trace store grows 3 -> 52 GiB; nt's back-to-back passes are clean).
#  1. PB, CGPB, SB with reverse_passthrough (images built on cluster B with the passthrough SDK): 5 repetitions, each a
#     fresh deployment, kinds rotated per repetition; repetition 1 climbs 10k.. to the kind's plateau (3 points without
#     >= 1 % gain), repetitions 2..5 re-run its grid; seeds 1001..1005.
#  2. v: 3 fresh-deployment repetitions over 10k..33k (seeds 1002..1004) replacing the contaminated passes 3..5; with the
#     n=1 ramp and pass 2 (first pass after its deploy) that is 5 clean samples.
# Every root must match its n=1 root (compare_to_n1.py). Flags = the n=1 sweep's.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff; WT=/users/tomislav/bp-passthrough; REPO=/users/tomislav/blueprint-docc-mod
echo $$ > $S/hotelnw_fd_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt); PT=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-pt-src-20260924T1345Z
D=$(cat $S/collector_queue3_digest)
COMMON="--collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
ROOTS=$S/hotelnw_fd_roots.txt; : > $ROOTS; rm -f $S/hotelnw_fd.done
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
VPID=$(cat $S/hotelnw_v_runner.pid); while kill -0 $VPID 2>/dev/null; do sleep 10; done
# A's tree -> the passthrough worktree; must then be identical
( cd $WT && for f in utils/derive_dsb_sn_nw.py utils/run_dsb_sn_nw.py runtime/plugins/otelcol/reverse_policy.go runtime/plugins/otelcol/reverse_checkpoint.go runtime/plugins/otelcol/pb_processor.go runtime/plugins/otelcol/cgpb_processor.go runtime/plugins/otelcol/sb_processor.go runtime/plugins/otelcol/reverse_passthrough_test.go; do cp $f $REPO/$f; done )
[ "$(cd $REPO && git diff HEAD | md5sum)" = "$(cd $WT && git diff HEAD | md5sum)" ] || fail "A's tree != passthrough worktree after sync"
[ -z "$(cd $REPO && git ls-files --others --exclude-standard | grep -v /build_ | sort | comm -3 - <(cd $WT && git ls-files --others --exclude-standard | grep -v /build_ | sort))" ] || fail "untracked files differ"
echo "$(date -u +%H:%M:%S) A tree synced to the passthrough worktree"
cd $REPO/utils
setsid nohup python3 -u $H/scripts/trace_census.py $ROOTS --until-file $S/hotelnw_fd.done > $S/hotelnw_fd_census.log 2>&1 < /dev/null &
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-fd-br-$STAMP
$PY derive_dsb_sn_nw.py --source $PT --provenance-from $PT $COMMON $BR --reverse-passthrough --plateau-stop --repeat-grid --repetitions 5 --rates $(seq -s ' ' 10000 1000 60000) --out $R --kinds pb cgpb sb --note "HotelReservation ZERO-WORK bridges WITH reverse_passthrough; n=5, FRESH deployment per repetition (kinds rotated), rep 1 climbs 10k.. to the kind's plateau, reps 2..5 re-run its grid; current best stack, cpd 2..4 depth_cubic. No smoke." > $S/hotelnw_fd_br_derive.log 2>&1 || fail "derive bridges"
for k in pb cgpb sb; do $PY $H/scripts/compare_to_n1.py $R $(cat $S/hotelnw_br_root.txt) $k --passthrough --new-images || fail "$k differs from its n=1 root"; done
echo $R >> $ROOTS
echo "$(date -u +%H:%M:%S) run hotelnw bridges (passthrough) 5 fresh-deploy repetitions $R"
$PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_fd_br_run.log 2>&1 || fail "run bridges"
echo "$(date -u +%H:%M:%S) done hotelnw bridges"
R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-fd-v-$STAMP
$PY derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC $COMMON --repetitions 3 --seeds 1002 1003 1004 --rates $(seq -s ' ' 10000 1000 33000) --out $R --kinds v --note "HotelReservation ZERO-WORK vanilla: 3 FRESH-deployment repetitions over 10k..33k replacing the heap-leak-contaminated back-to-back passes 3..5. No smoke." > $S/hotelnw_fd_v_derive.log 2>&1 || fail "derive v"
$PY $H/scripts/compare_to_n1.py $R $(cat $S/hotelnw_v_root.txt) v || fail "v differs from its n=1 root"
echo $R >> $ROOTS
echo "$(date -u +%H:%M:%S) run hotelnw v 3 fresh-deploy repetitions $R"
$PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_fd_v_run.log 2>&1 || fail "run v"
echo "$(date -u +%H:%M:%S) done hotelnw v"
touch $S/hotelnw_fd.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw fresh-deploy (bridges passthrough n=5, v +3)"
