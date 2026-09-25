#!/usr/bin/env bash
# Tomislav-RetCtx: SN REAL-WORK sweep on the current best stack (user 2026-09-24): start 2k, STEP increments, each kind
# stops at its OWN plateau in repetition 1 (3 consecutive points without >= 1 % more delivered throughput); REPS
# repetitions, EVERY repetition a fresh deployment (fresh databases and caches, social graph re-seeded by the runner's
# check_initialized), repetitions 2..REPS re-run repetition 1's grid for that kind. Bridges: depth_cubic cpd 2..6 WITH
# reverse_passthrough. Stack as the SN no-work memlimit round: otelcol agents admissionotel500m + ClickHouse gateway 2 CPU,
# queue3 collector, GOMAXPROCS auto, backlog margin, gateway LP resource_exhausted, agent compression none; bridges +
# priority receiver, SDK HP retry, queue stage at agents + gateway; GOGC=off GOMEMLIMIT=1GiB gctrace on app services.
# usage: STEP=100|200 REPS=n setsid nohup snrw_chain.sh > state/snrw_chain.log 2>&1 < /dev/null &   (DRYRUN=1: derive + verify only)
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
STEP=${STEP:?STEP}; REPS=${REPS:?REPS}; START=${START:-2000}; MAXRATE=${MAXRATE:-20000}; DRYRUN=${DRYRUN:-}
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
if [ -z "$DRYRUN" ]; then
  while pgrep -f "[s]nrw_build.sh|[h]otelnw_pt_build.sh|[s]n_final_build.sh" >/dev/null; do sleep 20; done  # no compiling during measurement
fi
# Tomislav-RetCtx (user 2026-09-24): FINAL images (passthrough + census off by default), no census, no trace capture
grep -q "SN rw FINAL BUILD COMPLETE" $ST/sn_final_build.log || fail "final SN real-work build not complete"
SRC=$(cat $ST/snrw_final_src_root.txt)
ROOTS=$ST/snrw_final_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$ST/snrw_dryrun_roots.txt; : > $ROOTS
BASE="--source $SRC --provenance-from $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture --plateau-stop $([ $REPS -gt 1 ] && echo --repeat-grid) --repetitions $REPS --rates $(seq -s ' ' $START $STEP $MAXRATE)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic --reverse-passthrough"
NOTE="SN REAL-WORK (seeded social graph, fresh deployment per repetition), FINAL SDK (passthrough, census off), no census / trace capture; hotel opt2 best config on ClickHouse, GOGC=off GOMEMLIMIT=1GiB gctrace; ${START}+${STEP} steps, repetition 1 stops at the kind's plateau, repetitions 2..$REPS re-run its grid; bridges cpd 2..6 depth_cubic WITH reverse_passthrough. No smoke."
verify() {
  local R=$1 group=$2 m k
  for m in $R/builds/*/manifest.yaml; do grep -q -i clickhouse $m && ! grep -q -i elasticsearch $m && grep -q GOMEMLIMIT $m || fail "$m stack check"; done
  python3 - $R $REPS $START $STEP $group <<'PYV' || fail "$R plan check"
import sys, json
R, reps, start, step, group = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
p = json.load(open(R + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['repetitions'] == reps and len(p['seeds']) == reps and bool(p.get('repeat_grid')) == (reps > 1) and p['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01}, 'protocol'
assert p['ramp_rates'][0] == start and p['ramp_rates'][1] - p['ramp_rates'][0] == step and not p.get('ramp_passes'), 'grid'
assert p.get('discovery_override') == ({'reverse_policy': 'depth_cubic', 'reverse_passthrough': True} if group == 'br' else None), p.get('discovery_override')
cases = json.load(open(R + '/cases.json'))
assert all(c.get('app') == 'snrw' for c in cases), [c.get('app') for c in cases]
assert p.get('trace_capture') is False, 'trace capture must be off'
print(f"{group}: {[c['kind'] for c in cases]} x {reps} reps, rates {p['ramp_rates'][0]}+{step}.., seeds {p['seeds']}")
PYV
  for k in pb cgpb sb; do
    [ -f $R/builds/$k/manifest.yaml ] || continue
    for s in "reverse_policy: depth_cubic" "reverse_passthrough: true" "cpd_min: 2" "cpd_max: 6" 1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94; do
      grep -q "$s" $R/builds/$k/manifest.yaml || fail "$k manifest lacks '$s'"; done
  done
}
cd /users/tomislav/blueprint-docc-mod/utils
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for group in nt v br; do
  if [ $group = br ]; then KINDS="pb cgpb sb"; EXTRA=$BR; else KINDS=$group; EXTRA=""; fi
  R=/users/tomislav/deployments/dsb-sn/retctx-snrw-final-$group-$STAMP; [ -n "$DRYRUN" ] && R=$ST/dryrun/retctx-snrw-$group-$STAMP
  $PY derive_dsb_sn_nw.py $BASE $EXTRA --out $R --kinds $KINDS --note "$NOTE" > $ST/snrw_${group}_derive.log 2>&1 || fail "derive $group (see $ST/snrw_${group}_derive.log)"
  verify $R $group; echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  echo "$(date -u +%H:%M:%S) run SN real-work $KINDS x $REPS reps $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/snrw_${group}_run.log 2>&1 || fail "run $group (see $ST/snrw_${group}_run.log)"
  echo "$(date -u +%H:%M:%S) done SN real-work $KINDS"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snrw"; exit 0; }
touch $ST/snrw.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE snrw"
