#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-24 18:45Z): SN REAL-WORK bridges WITHOUT reverse_passthrough (depth_cubic cpd 2..6; a
# scheduled checkpoint terminates every returned truss), otherwise exactly snrw_chain.sh's br group: same FINAL source
# root / images, stack, 2k+STEP ramp, repetition 1 to the kind's own plateau, repetitions 2..REPS a fresh deployment each
# re-running its grid, no census, no trace capture. Then SN real-work nt repetition 6 (seed 6001, nt's grid 2000..3600) --
# the cold-machine rule: the first deployment after idle / rebuild counts as warm-up, so nt rep 1 can be dropped.
# Each group's plan + manifests are checked structurally against the matching finished / superseded root.
# usage: STEP=100 REPS=5 setsid nohup snrw_nopt_chain.sh > state/snrw_nopt_chain.log 2>&1 < /dev/null &   (DRYRUN=1: derive + verify only)
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
STEP=${STEP:?STEP}; REPS=${REPS:?REPS}; START=${START:-2000}; MAXRATE=${MAXRATE:-20000}; DRYRUN=${DRYRUN:-}
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94
DS=/users/tomislav/deployments/dsb-sn
PT_REF=$DS/retctx-snrw-final-br-20260924T163105Z-superseded-passthrough   # same derive + --reverse-passthrough
NT_REF=$DS/retctx-snrw-final-nt-20260924T163105Z
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
grep -q "SN rw FINAL BUILD COMPLETE" $ST/sn_final_build.log || fail "final SN real-work build not complete"
SRC=$(cat $ST/snrw_final_src_root.txt)
ROOTS=$ST/snrw_nopt_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$ST/snrw_nopt_dryrun_roots.txt; : > $ROOTS
COMMON="--source $SRC --provenance-from $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture"
BASE="$COMMON --plateau-stop --repeat-grid --repetitions $REPS --rates $(seq -s ' ' $START $STEP $MAXRATE)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
NOTE="SN REAL-WORK bridges WITHOUT reverse_passthrough (user 2026-09-24 18:45Z), otherwise as retctx-snrw-final-br (FINAL SDK, census off, no census / trace capture, GOGC=off GOMEMLIMIT=1GiB gctrace, ${START}+${STEP}, repetition 1 to the kind's plateau, 2..$REPS re-run its grid, fresh deployment each); cpd 2..6 depth_cubic, scheduled checkpoints terminate returned trusses."
NOTE6="SN REAL-WORK nt repetition 6 (seed 6001) on nt's final grid 2000..3600: the first deployment after idle/rebuild counts as warm-up (drop nt rep 1). Same stack as retctx-snrw-final-nt."
verify_br() {
  local R=$1 m k
  for m in $R/builds/*/manifest.yaml; do grep -q -i clickhouse $m && ! grep -q -i elasticsearch $m && grep -q GOMEMLIMIT $m || fail "$m stack check"; done
  python3 - $R $REPS $START $STEP <<'PYV' || fail "$R plan check"
import sys, json
R, reps, start, step = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
p = json.load(open(R + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['repetitions'] == reps and len(p['seeds']) == reps and p.get('repeat_grid') and p['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01}, 'protocol'
assert p['ramp_rates'][0] == start and p['ramp_rates'][1] - p['ramp_rates'][0] == step and not p.get('ramp_passes'), 'grid'
assert p.get('discovery_override') == {'reverse_policy': 'depth_cubic'}, p.get('discovery_override')
cases = json.load(open(R + '/cases.json'))
assert all(c.get('app') == 'snrw' for c in cases) and [c['kind'] for c in cases] == ['pb', 'cgpb', 'sb'], cases
assert p.get('trace_capture') is False, 'trace capture must be off'
print(f"br (no passthrough): {[c['kind'] for c in cases]} x {reps} reps, rates {p['ramp_rates'][0]}+{step}.., seeds {p['seeds']}")
PYV
  for k in pb cgpb sb; do
    for s in "reverse_policy: depth_cubic" "cpd_min: 2" "cpd_max: 6" 1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94; do
      grep -q "$s" $R/builds/$k/manifest.yaml || fail "$k manifest lacks '$s'"; done
    ! grep -q "reverse_passthrough" $R/builds/$k/manifest.yaml || fail "$k manifest still sets reverse_passthrough"
    # identical to the passthrough root except the reverse_passthrough key (compare_to_n1 --passthrough drops it from both)
    $PY $H/scripts/compare_to_n1.py $PT_REF $R $k --passthrough || fail "$k differs from $PT_REF beyond passthrough"
  done
}
verify_nt6() {
  local R=$1
  python3 - $R <<'PYV' || fail "$R plan check"
import sys, json
p = json.load(open(sys.argv[1] + '/plan.json'))
assert p['ramp_rates'] == list(range(2000, 3601, 100)) and p['seeds'] == [6001] and p['repetitions'] == 1, (p['ramp_rates'], p['seeds'])
assert not p.get('plateau_stop') and not p.get('repeat_grid') and p.get('trace_capture') is False
print(f"nt rep 6: rates {p['ramp_rates'][0]}..{p['ramp_rates'][-1]}, seed {p['seeds']}")
PYV
  $PY $H/scripts/compare_to_n1.py $R $NT_REF nt || fail "nt differs from $NT_REF"
}
cd /users/tomislav/blueprint-docc-mod/utils
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
R=$DS/retctx-snrw-nopt-br-$STAMP; [ -n "$DRYRUN" ] && R=$ST/dryrun/retctx-snrw-nopt-br-$STAMP
$PY derive_dsb_sn_nw.py $BASE $BR --out $R --kinds pb cgpb sb --note "$NOTE" > $ST/snrw_nopt_br_derive.log 2>&1 || fail "derive br (see $ST/snrw_nopt_br_derive.log)"
verify_br $R; echo $R >> $ROOTS
R6=$DS/retctx-snrw-final-nt6-$STAMP; [ -n "$DRYRUN" ] && R6=$ST/dryrun/retctx-snrw-final-nt6-$STAMP
$PY derive_dsb_sn_nw.py $COMMON --repetitions 1 --seeds 6001 --rates $(seq -s ' ' 2000 100 3600) --out $R6 --kinds nt --note "$NOTE6" > $ST/snrw_nt6_derive.log 2>&1 || fail "derive nt6 (see $ST/snrw_nt6_derive.log)"
verify_nt6 $R6; echo $R6 >> $ROOTS
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snrw nopt"; exit 0; }
echo "$(date -u +%H:%M:%S) run SN real-work pb cgpb sb (no passthrough) x $REPS reps $R"
$PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/snrw_nopt_br_run.log 2>&1 || fail "run br (see $ST/snrw_nopt_br_run.log)"
echo "$(date -u +%H:%M:%S) done SN real-work pb cgpb sb (no passthrough)"
echo "$(date -u +%H:%M:%S) run SN real-work nt rep 6 $R6"
$PY run_dsb_sn_nw.py run --out $R6 --skip-smoke > $ST/snrw_nt6_run.log 2>&1 || fail "run nt6 (see $ST/snrw_nt6_run.log)"
echo "$(date -u +%H:%M:%S) done SN real-work nt rep 6"
touch $ST/snrw_nopt.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE snrw nopt"
