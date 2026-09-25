#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-24 ~21:05Z): Social Network NO-WORK on cluster A, after hotel no-work finishes, bridges WITHOUT
# reverse_passthrough. Same protocol as the SN real-work final round on B: n=5, a FRESH deployment per repetition, 30 s
# points, repetition 1 climbs 1k.. in 1k steps to the kind's own plateau (3 points without >= 1 % gain), repetitions 2..5
# re-run its grid; no census, no trace capture. Stack: otelcol admissionotel500m agents + ClickHouse gateway 2 CPU, queue3
# collector, GOMAXPROCS auto, backlog margin, gateway LP resource_exhausted, agent compression none, GOGC=off
# GOMEMLIMIT=1GiB gctrace; bridges + priority receiver, SDK HP retry, priority queue at agents + gateway, depth_cubic
# (cpd 2..6). Images: v/PB/CGPB/SB = the FINAL SDK build from cluster B (retctx-snnw-final-src-20260924T1609Z; census off
# by default; passthrough off unless the discovery key says so), imported digest-preserving; nt = the matrix images.
# Every root is checked structurally against its n=1 memlimit-round root (compare_to_n1.py) before anything deploys.
# usage: setsid nohup snnw_nopt_chain.sh > snnw_nopt_chain.log 2>&1 < /dev/null &      (DRYRUN=1: derive + verify only)
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
H=/storage/retctx-handoff; REPO=/users/tomislav/blueprint-docc-mod; DS=/users/tomislav/deployments/dsb-sn
PY="$REPO/.venv/bin/python -B -u"; DRYRUN=${DRYRUN:-}
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94  # queue3
SRC=$DS/retctx-snnw-final-src-20260924T1609Z
SRC_NT=$DS/retctx-nw-m5-20260920T045217Z
PROV=$DS/retctx-nwe2e-m5-on-r1-20260920T052509Z
N1_NT=$DS/retctx-nwe2e-mem-nt-20260924T025903Z
N1_V=$DS/retctx-nwe2e-memch-v-20260924T035236Z
N1_BR=$DS/retctx-nwe2e-memch-br-20260924T041154Z
IMG=$S/images-snnw-final
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
[ -z "$DRYRUN" ] && echo $$ > $S/snnw_nopt_chain.pid
if [ -z "$DRYRUN" ]; then
  # one application on the cluster at a time: start only after the hotel no-work chain has finished
  until grep -q "PASSES CHAIN COMPLETE" $S/hotelnw_fd2_chain.log; do
    grep -q "PASSES CHAIN FAILED" $S/hotelnw_fd2_chain.log && fail "hotel chain failed; not starting"; sleep 20; done
  echo "$(date -u +%H:%M:%S) hotel no-work chain complete; importing the final SN no-work images"
  python3 $H/scripts/registry_copy.py import $IMG > $S/snnw_nopt_import.log 2>&1 || fail "image import (see $S/snnw_nopt_import.log)"
fi
python3 $H/scripts/registry_copy.py check $(cat $S/snnw_final_refs.txt) | tail -1 | grep -q "^\([0-9]*\)/\1 present" \
  || { [ -n "$DRYRUN" ] && echo "(dry run: images not imported yet)" || fail "registry images missing"; }
COMMON="--provenance-from $PROV --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture --plateau-stop --repeat-grid --repetitions 5 --rates $(seq -s ' ' 1000 1000 60000)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
NOTE="SN NO-WORK, n=5, FRESH deployment per repetition, repetition 1 climbs 1k.. (1k steps) to the kind's plateau, 2..5 re-run its grid; FINAL SDK images (census off), bridges WITHOUT reverse_passthrough (cpd 2..6 depth_cubic); hotel opt2 best config on ClickHouse, GOGC=off GOMEMLIMIT=1GiB gctrace; no census, no trace capture. No smoke."
ROOTS=$S/snnw_nopt_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$S/snnw_nopt_dryrun_roots.txt; : > $ROOTS
cd $REPO/utils
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
for group in nt v br; do
  case $group in
    nt) KINDS=nt; SOURCE=$SRC_NT; EXTRA=""; N1=$N1_NT; FLAGS="--app-only";;  # n=1 nt predates the ClickHouse stack (idle for nt)
    v)  KINDS=v; SOURCE=$SRC; EXTRA=""; N1=$N1_V; FLAGS="--new-images";;
    br) KINDS="pb cgpb sb"; SOURCE=$SRC; EXTRA=$BR; N1=$N1_BR; FLAGS="--new-images";;
  esac
  R=$DS/retctx-snnw-nopt-$group-$STAMP; [ -n "$DRYRUN" ] && R=$S/dryrun/retctx-snnw-nopt-$group-$STAMP
  $PY derive_dsb_sn_nw.py --source $SOURCE $COMMON $EXTRA --out $R --kinds $KINDS --note "$NOTE" > $S/snnw_nopt_${group}_derive.log 2>&1 || fail "derive $group (see $S/snnw_nopt_${group}_derive.log)"
  for m in $R/builds/*/manifest.yaml; do grep -q GOMEMLIMIT $m || fail "$m lacks GOMEMLIMIT"; done
  python3 - $R $group <<'PYV' || fail "$R plan check"
import sys, json
R, group = sys.argv[1], sys.argv[2]
p = json.load(open(R + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['repetitions'] == 5 and len(p['seeds']) == 5 and p.get('repeat_grid') and p['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01}, 'protocol'
assert p['ramp_rates'][0] == 1000 and p['ramp_rates'][1] == 2000 and not p.get('ramp_passes'), 'grid'
assert p.get('trace_capture') is False, 'trace capture must be off'
assert p.get('discovery_override') == ({'reverse_policy': 'depth_cubic'} if group == 'br' else None), p.get('discovery_override')
print(f"{group}: {[c['kind'] for c in json.load(open(R + '/cases.json'))]} x 5 reps, rates 1000+1000.., seeds {p['seeds']}")
PYV
  for k in $KINDS; do
    if [ $k != nt ]; then
      grep -q -i clickhouse $R/builds/$k/manifest.yaml && ! grep -q -i elasticsearch $R/builds/$k/manifest.yaml || fail "$k stack (ClickHouse)"
      ! grep -q reverse_passthrough $R/builds/$k/manifest.yaml || fail "$k sets reverse_passthrough"
    fi
    $PY $H/scripts/compare_to_n1.py $R $N1 $k $FLAGS || fail "$k differs from its n=1 root $N1"
  done
  echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  echo "$(date -u +%H:%M:%S) run SN no-work $KINDS x 5 reps $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/snnw_nopt_${group}_run.log 2>&1 || fail "run $group (see $S/snnw_nopt_${group}_run.log)"
  echo "$(date -u +%H:%M:%S) done SN no-work $KINDS"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snnw nopt"; exit 0; }
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE snnw no-work n=5 (nt, v, pb cgpb sb no passthrough)"
