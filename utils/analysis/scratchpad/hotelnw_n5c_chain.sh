#!/usr/bin/env bash
# Tomislav-RetCtx: bridge part of hotelnw_n5b_chain.sh, with the structural n=1 check (compare_to_n1.py; the line-diff check
# failed on benign build-stamp / folded-ConfigMap differences). Continuation of hotelnw_n5_chain.sh (user 2026-09-24: "after that let's recompile stuff with the
# passthrough"). nt passes 2..5 finish under the old chain's runner; v passes 2..5 as planned (vanilla is untouched by
# reverse_passthrough, same images as its n=1 pass 1); then PB, CGPB, SB are a FRESH n=5 on images built WITH the
# reverse_passthrough SDK (cluster B, hotelnw_pt_build.sh), run with --reverse-passthrough (scheduled checkpoints route
# returned trusses by depth_cubic instead of terminating them): 10k.., pass 1 stops at the kind's plateau, 5 passes, one
# deployment per kind, 60 s between passes. Before the bridges: A's tree is synced to the passthrough worktree, the
# images are imported digest-preserving, and each root must match its n=1 root except rates / passes / passthrough / app
# image digests.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff; B=tomislav@c220g5-111207.wisc.cloudlab.us; WT=/users/tomislav/bp-passthrough; REPO=/users/tomislav/blueprint-docc-mod
echo $$ > $S/hotelnw_n5c_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt)
D=$(cat $S/collector_queue3_digest)
COMMON="--repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --pass-gap-seconds 60"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
ROOTS=$S/hotelnw_n5_roots.txt
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
check() {  # root, its n=1 root, kind, passthrough(0/1)
  python3 - "$@" <<'PYV' || fail "$3 plan/manifest differs from its n=1 root"
import sys, json, re
R, N1, k, pt = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == '1'
a, b = json.load(open(f'{N1}/plan.json')), json.load(open(f'{R}/plan.json'))
skip = {'created', 'note', 'ramp_rates', 'raw_data_storage', 'ramp_passes', 'plateau_stop', 'knee_windows', 'derived_from', 'cases', 'case_order_note', 'discovery_override'}
diff = {x: (a.get(x), b.get(x)) for x in set(a) | set(b) if x not in skip and a.get(x) != b.get(x)}
assert not diff, diff
assert b.get('discovery_override') == ({'reverse_passthrough': True} if pt else None), b.get('discovery_override')
def norm(t):
    t = re.sub(r'x2026\d{4}t\d{4,6}z|2026\d{4}T\d{6}Z|/storage/tomislav-retctx-e2e/[^\s"]+', 'S', t)
    if pt:  # new app images (same source + passthrough), and the one config_map line
        t = re.sub(r'(-service-hotel-[a-z]+-es-nw[^@\s]*)@sha256:[0-9a-f]{64}', r'\1@APP', t)
        t = re.sub(r'\n\s*reverse_passthrough: true', '', t)
    return t
assert norm(open(f'{N1}/builds/{k}/manifest.yaml').read()) == norm(open(f'{R}/builds/{k}/manifest.yaml').read()), 'manifest'
if pt:
    m = open(f'{R}/builds/{k}/manifest.yaml').read()
    assert 'reverse_passthrough: true' in m and 'reverse_policy: depth_cubic' in m and '1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94' in m
print(f'{k}: plan + manifest match n=1 ({N1.split("/")[-1]}) except rates/passes' + (' / passthrough / app image digests' if pt else '') + f'; rates {b["ramp_rates"][0]}..{b["ramp_rates"][-1]}')
PYV
}
cd $REPO/utils
VPID=$(cat $S/hotelnw_v_runner.pid); VR=$(sed -n 2p $ROOTS)
echo "$(date -u +%H:%M:%S) waiting for the v runner (pid $VPID, $VR)"
while kill -0 $VPID 2>/dev/null; do sleep 15; done
[ -f $VR/run/01-v/complete.json ] || fail "v runner exited without complete.json"
echo "$(date -u +%H:%M:%S) done hotelnw v"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
# --- bridges: passthrough images from cluster B
echo "$(date -u +%H:%M:%S) waiting for cluster B's passthrough bridge images"
until ssh -n -o BatchMode=yes -o ConnectTimeout=15 $B "test -f $H/state/hotelnw_pt_build.done"; do
  ssh -n -o BatchMode=yes -o ConnectTimeout=15 $B "grep -q 'BUILD FAILED' $H/state/hotelnw_pt_build_chain.log" 2>/dev/null && fail "cluster B hotel passthrough build failed"
  sleep 30; done
PT=$(ssh -n -o BatchMode=yes $B "cat $H/state/hotelnw_pt_src_root.txt")
rsync -a $B:$H/images/hotelnw-pt-registry/ $H/images/hotelnw-pt-registry/ && rsync -a $B:$PT/ $PT/ || fail "copy from B"
python3 $H/scripts/registry_copy.py import $H/images/hotelnw-pt-registry > $S/hotelnw_pt_import.log 2>&1 || fail "image import"
echo "$(date -u +%H:%M:%S) imported $(grep -c ^imported $S/hotelnw_pt_import.log) passthrough images; source root $PT"
# A's tree -> the passthrough worktree (runtime + derive flag); must then equal it exactly
( cd $WT && for f in utils/derive_dsb_sn_nw.py runtime/plugins/otelcol/reverse_policy.go runtime/plugins/otelcol/reverse_checkpoint.go runtime/plugins/otelcol/pb_processor.go runtime/plugins/otelcol/cgpb_processor.go runtime/plugins/otelcol/sb_processor.go runtime/plugins/otelcol/reverse_passthrough_test.go; do cp $f $REPO/$f; done )
[ "$(cd $REPO && git diff HEAD | md5sum)" = "$(cd $WT && git diff HEAD | md5sum)" ] || fail "A's tree != passthrough worktree after sync"
echo "$(date -u +%H:%M:%S) A tree synced to the passthrough worktree"
N1=$(cat $S/hotelnw_br_root.txt)
for k in pb cgpb sb; do
  R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-n5pt-$k-$STAMP
  $PY derive_dsb_sn_nw.py --source $PT --provenance-from $PT $COMMON $BR --reverse-passthrough --plateau-stop --ramp-passes 5 --rates $(seq -s ' ' 10000 1000 60000) --out $R --kinds $k --note "HotelReservation ZERO-WORK, bridges WITH reverse_passthrough (images built on cluster B from A's tree + passthrough); n=5 back-to-back passes, 10k.., pass 1 stops at the kind's plateau, one deployment, 60 s between passes; current best stack, cpd 2..4 depth_cubic. No smoke." > $S/hotelnw_n5pt_${k}_derive.log 2>&1 || fail "derive $k"
  $PY $H/scripts/compare_to_n1.py $R $N1 $k --passthrough --new-images || fail "$k differs from its n=1 root"; echo $R >> $ROOTS
  echo "$(date -u +%H:%M:%S) run hotelnw $k (passthrough) 5 passes $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/hotelnw_n5pt_${k}_run.log 2>&1 || fail "run $k"
  echo "$(date -u +%H:%M:%S) done hotelnw $k"
done
touch $S/hotelnw_n5.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw n5 (nt, v top-up; pb cgpb sb passthrough)"
