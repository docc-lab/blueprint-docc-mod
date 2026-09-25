#!/usr/bin/env bash
# Tomislav-RetCtx: N back-to-back FULL ramps per kind, one deployment per kind (user 2026-09-24: "run ramps one after
# another without redeploy for both no-work things ... n=3 or n=5 ... increments of 1k ... start the hotelres at 5k or
# 10k"). Per kind: deploy + warm-up once; pass 1 climbs START, START+STEP, ... until the kind's OWN plateau (3 consecutive
# points without >= 1 % more delivered throughput); passes 2..N re-run exactly pass 1's rates (same number of trials at
# every point), GAP s idle between passes. Order nt, v, then pb cgpb sb (as every A-side sweep).
# Stack = the current best stack, identical to the A-side runs: otelcol agents (admissionotel500m) + ClickHouse gateway
# (2 CPU), queue3 collector image, GOMAXPROCS auto, backlog margin, gateway LP resource_exhausted, agent compression
# none; bridges + priority receiver, SDK HP retry, queue stage at agents + gateway; depth_cubic reverse policy (hotel
# cpd 2..4 = app default; SN cpd 2..6 + --reverse-policy depth_cubic); GOGC=off GOMEMLIMIT=1GiB gctrace on app services.
# usage: passes_chain.sh hotelnw|snnw     env: PASSES (5) START (hotelnw 5000, snnw 1000) STEP (1000) GAP (60) MAXRATE (60000)
#        DRYRUN=1: derive + verify the three roots under DRYROOT (default: \$H/state/dryrun), deploy nothing
# launch detached:  setsid nohup scripts/passes_chain.sh hotelnw > state/hotelnw_passes_chain.log 2>&1 < /dev/null &
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state; mkdir -p $ST
APP=$1; PASSES=${PASSES:-5}; STEP=${STEP:-1000}; GAP=${GAP:-60}; MAXRATE=${MAXRATE:-60000}
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94  # queue3
case $APP in
  hotelnw) START=${START:-5000}; NS=dsb-hotel; POL=""
           SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-src-20260924T0631Z; PROV=$SRC; SRC_NT=$SRC
           NOTEAPP="HotelReservation ZERO-WORK (workflow/hotelnw), SDK at 79e9e9fa, cpd 2..4 depth_cubic";;
  snnw)    START=${START:-1000}; NS=dsb-sn; POL="--reverse-policy depth_cubic"
           SRC=/users/tomislav/deployments/dsb-sn/retctx-nw-opt-20260924T0145Z
           SRC_NT=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z  # the opt build has no nt; A's SN nt runs used these
           PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
           NOTEAPP="SN no-work, optimized SDK (5f021f53 images), cpd 2..6 depth_cubic";;
  *) echo "PASSES CHAIN FAILED: app $APP"; exit 1;;
esac
DRYRUN=${DRYRUN:-}
[ -z "$DRYRUN" ] && echo $$ > $ST/${APP}_passes_chain.pid
ROOTS=$ST/${APP}_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$ST/${APP}_dryrun_roots.txt; : > $ROOTS; [ -z "$DRYRUN" ] && rm -f $ST/${APP}_passes.done
RATES=$(seq -s ' ' $START $STEP $MAXRATE)
BASE="--provenance-from $PROV --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --plateau-stop --ramp-passes $PASSES --pass-gap-seconds $GAP --rates $RATES"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue $POL"
NOTE="$NOTEAPP, hotel opt2 best config on ClickHouse, GOGC=off GOMEMLIMIT=1GiB gctrace; $PASSES back-to-back ramp passes per deployment, ${START}+${STEP} steps, per-kind plateau stop (pass 1 fixes the grid), ${GAP}s between passes. No smoke."
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
verify() {  # the stack and protocol actually in the derived root, before anything deploys
  local R=$1 m k
  for m in $R/builds/*/manifest.yaml; do
    grep -q -i clickhouse $m && ! grep -q -i elasticsearch $m && grep -q GOMEMLIMIT $m || fail "$m stack check"
  done
  python3 - $R $PASSES $START $GAP <<'PYV' || fail "$R plan check"
import sys, json
p = json.load(open(sys.argv[1] + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['ramp_passes']['passes'] == int(sys.argv[2]) and p['ramp_passes']['gap_seconds'] == int(sys.argv[4]), 'passes'
assert p['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01}, 'plateau'
assert p['ramp_rates'][0] == int(sys.argv[3]) and p['ramp_rates'][1] - p['ramp_rates'][0] == 1000, 'grid'
assert not p.get('knee_windows'), 'knee windows off'
PYV
  for k in pb cgpb sb; do
    [ -f $R/builds/$k/manifest.yaml ] || continue
    grep -q "reverse_policy: depth_cubic" $R/builds/$k/manifest.yaml || fail "$k not depth_cubic"
    grep -q "1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94" $R/builds/$k/manifest.yaml || fail "$k not queue3"
  done
}
cd /users/tomislav/blueprint-docc-mod/utils
# preflight: every image the manifests pin is in this cluster's registry (scripts/registry_copy.py import)
python3 $H/scripts/registry_copy.py check $(cat $H/images/${APP}-refs.txt) | tail -1 | grep -q "^\([0-9]*\)/\1 present" || fail "registry images missing"
# the trace census runs detached for the whole chain (ClickHouse is only queryable while a case is deployed)
[ -z "$DRYRUN" ] && setsid nohup python3 -u $H/scripts/trace_census.py $ROOTS --until-file $ST/${APP}_passes.done > $ST/${APP}_census.log 2>&1 < /dev/null &
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for group in nt v br; do
  R=/users/tomislav/deployments/$NS/retctx-${APP}-passes-$group-$STAMP
  [ -n "$DRYRUN" ] && R=${DRYROOT:-$ST/dryrun}/retctx-${APP}-passes-$group-$STAMP
  if [ $group = br ]; then KINDS="pb cgpb sb"; EXTRA=$BR; else KINDS=$group; EXTRA=""; fi
  SOURCE=$SRC; [ $group = nt ] && SOURCE=$SRC_NT
  $PY derive_dsb_sn_nw.py --source $SOURCE $BASE $EXTRA --out $R --kinds $KINDS --note "$NOTE" > $ST/${APP}_${group}_derive.log 2>&1 || fail "derive $group (see $ST/${APP}_${group}_derive.log)"
  verify $R; echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && { echo "$(date -u +%H:%M:%S) DRYRUN ok: $APP $KINDS $R"; continue; }
  echo "$(date -u +%H:%M:%S) run $APP $KINDS ($PASSES passes) $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/${APP}_${group}_run.log 2>&1 || fail "run $group (see $ST/${APP}_${group}_run.log)"
  echo "$(date -u +%H:%M:%S) done $APP $KINDS"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE $APP"; exit 0; }
touch $ST/${APP}_passes.done
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE $APP"
