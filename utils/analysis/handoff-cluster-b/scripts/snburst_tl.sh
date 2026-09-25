#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-25 ~06:05Z): burst-mechanism timeline pair on cluster B: bursty VANILLA and SB REV with the
# runner keeping WHOLE pod logs (RETCTX_LOG_TAIL=-1), so the per-second SDK counters (vanilla spans_dropped; SB cp/lp_dropped)
# and the agents' per-second priority-queue counters cover the whole 600 s point. Same as cluster A's snburst runs otherwise:
# SN no-work current stack, mean 7500, Pareto alpha 1.5 cap 2.0 epoch 10 s, 600 s, seed 1001, census on (+records), 5000
# uniform trace samples, fresh deployment per case, no smoke.      usage: setsid ... snburst_tl.sh > state/snburst_tl.log   (DRYRUN=1)
set -euo pipefail
H=/storage/retctx-handoff; ST=$H/state; REPO=/users/tomislav/blueprint-docc-mod; DS=/users/tomislav/deployments/dsb-sn
PY="$REPO/.venv/bin/python -B -u"; DRYRUN=${DRYRUN:-}
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94  # queue3
SRC=$DS/retctx-snnw-final-src-20260924T1609Z; PROV=$DS/retctx-nwe2e-m5-on-r1-20260920T052509Z
fail() { echo "$(date -u +%H:%M:%S) SNBURST-TL FAILED: $*"; exit 1; }
log() { echo "$(date -u +%H:%M:%S) $*"; }
STACK="--provenance-from $PROV --source $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --repetitions 1 --seeds 1001"
GEN="--rates 7500 --seconds-per-rate 600 --generator pareto --wrk-binary /users/tomislav/DeathStarBench/wrk2/wrk --burst-alpha 1.5 --burst-cap 2.0 --burst-epoch 10s --census"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
NOTE="SN no-work BURSTY timeline pair on cluster B (whole pod logs kept for per-second counters): mean 7500 = vanilla's last clean sustained rate on cluster A, Pareto alpha 1.5 cap 2.0 epoch 10 s, 600 s, seed 1001; census on; 5000 uniform traces."
ROOTS=$ST/snburst_tl_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$ST/snburst_tl_dryrun_roots.txt; : > $ROOTS
[ -z "$DRYRUN" ] && echo $$ > $ST/snburst_tl.pid
cd $REPO/utils
for group in v sbrev; do
  case $group in v) KINDS=v; EXTRA="--reverse on";; sbrev) KINDS=sb; EXTRA="--reverse on $BR";; esac
  R=$DS/retctx-snburst-tl-$group-$(date -u +%Y%m%dT%H%M%SZ); [ -n "$DRYRUN" ] && R=$ST/dryrun/$(basename $R)
  $PY derive_dsb_sn_nw.py $STACK $GEN $EXTRA --kinds $KINDS --out $R --note "$NOTE group=$group." > $ST/snburst_tl_${group}_derive.log 2>&1 || fail "derive $group (see $ST/snburst_tl_${group}_derive.log)"
  for m in $R/builds/*/manifest.yaml; do
    grep -q GOMEMLIMIT $m && grep -q RETCTX_REFUSED_CENSUS $m && grep -q RETCTX_REFUSED_RECORDS $m || fail "$m lacks GOMEMLIMIT / census / records"
    ! grep -q reverse_passthrough $m || fail "$m sets reverse_passthrough"
  done
  python3 -c "import json,sys; p=json.load(open('$R/plan.json')); assert p['backend']=='clickhouse' and p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94') and p['seeds']==[1001] and p['generator']['burst_cap']==2.0 and p.get('census') is True, p; print('$group ok', p['ramp_rates'])" || fail "plan check $group"
  echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  log "run bursty $group at mean 7500 (whole logs): $R"
  RETCTX_LOG_TAIL=-1 RETCTX_REFUSED_BIN=on RETCTX_TRACE_SAMPLE_WINDOWS=10 RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random \
    $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/snburst_tl_${group}_run.log 2>&1 < /dev/null || fail "run $group (see $ST/snburst_tl_${group}_run.log)"
  log "done bursty $group"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snburst-tl"; exit 0; }
log "SNBURST-TL COMPLETE"
