#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK bridges WITH reverse_passthrough (user 2026-09-24: "we need hotel with
# passthrough"): PB, CGPB, SB, each a FRESH n=5 -- 5 back-to-back passes in one deployment, 10k.., pass 1 stops at the
# kind's plateau (3 points without >= 1 % gain), 60 s between passes -- on the images built with the passthrough SDK
# (hotelnw_pt_build.sh), derive --reverse-passthrough. Flags identical to cluster A's hotel no-work sweep; each root must
# match A's n=1 bridge root (plan + manifest) except rates / passes / passthrough / app image digests.
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94
N1=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-mem-br-20260924T071346Z
PT=$(cat $ST/hotelnw_pt_src_root.txt)
COMMON="--repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --pass-gap-seconds 60"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
ROOTS=$ST/hotelnw_pt_roots.txt; : > $ROOTS; rm -f $ST/hotelnw_pt.done
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
check() {
  python3 - "$@" <<'PYV' || fail "$3 plan/manifest differs from its n=1 root"
import sys, json, re
R, N1, k = sys.argv[1], sys.argv[2], sys.argv[3]
a, b = json.load(open(f'{N1}/plan.json')), json.load(open(f'{R}/plan.json'))
skip = {'created', 'note', 'ramp_rates', 'raw_data_storage', 'ramp_passes', 'plateau_stop', 'knee_windows', 'derived_from', 'cases', 'case_order_note', 'discovery_override'}
diff = {x: (a.get(x), b.get(x)) for x in set(a) | set(b) if x not in skip and a.get(x) != b.get(x)}
assert not diff, diff
assert b.get('discovery_override') == {'reverse_passthrough': True}, b.get('discovery_override')
assert b['ramp_passes']['passes'] == 5 and b['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01} and b['ramp_rates'][0] == 10000
def norm(t):
    t = re.sub(r'x2026\d{4}t\d{4,6}z|2026\d{4}T\d{6}Z|/storage/tomislav-retctx-e2e/[^\s"]+', 'S', t)
    t = re.sub(r'(-service-hotel-[a-z]+-es-nw[^@\s]*)@sha256:[0-9a-f]{64}', r'\1@APP', t)
    return re.sub(r'\n\s*reverse_passthrough: true', '', t)
assert norm(open(f'{N1}/builds/{k}/manifest.yaml').read()) == norm(open(f'{R}/builds/{k}/manifest.yaml').read()), 'manifest'
m = open(f'{R}/builds/{k}/manifest.yaml').read()
assert 'reverse_passthrough: true' in m and 'reverse_policy: depth_cubic' in m and '1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94' in m
print(f'{k}: plan + manifest match n=1 ({N1.split("/")[-1]}) except rates/passes/passthrough/app image digests')
PYV
}
cd /users/tomislav/blueprint-docc-mod/utils
setsid -f python3 -u $H/scripts/trace_census.py $ROOTS --until-file $ST/hotelnw_pt.done > $ST/hotelnw_pt_census.log 2>&1 < /dev/null
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for k in pb cgpb sb; do
  R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-n5pt-$k-$STAMP
  $PY derive_dsb_sn_nw.py --source $PT --provenance-from $PT $COMMON $BR --reverse-passthrough --plateau-stop --ramp-passes 5 --rates $(seq -s ' ' 10000 1000 60000) --out $R --kinds $k --note "HotelReservation ZERO-WORK, bridges WITH reverse_passthrough; n=5 back-to-back passes, 10k.., pass 1 stops at the kind's plateau, one deployment, 60 s between passes; current best stack, cpd 2..4 depth_cubic. Cluster B. No smoke." > $ST/hotelnw_pt_${k}_derive.log 2>&1 || fail "derive $k"
  check $R $N1 $k; echo $R >> $ROOTS
  echo "$(date -u +%H:%M:%S) run hotelnw $k (passthrough) 5 passes $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/hotelnw_pt_${k}_run.log 2>&1 || fail "run $k"
  echo "$(date -u +%H:%M:%S) done hotelnw $k"
done
touch $ST/hotelnw_pt.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelnw passthrough bridges"
