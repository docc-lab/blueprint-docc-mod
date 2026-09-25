#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-25 ~03:15Z): SPAN-LOSS experiment rerun on SN NO-WORK with the current stack, before the hotel
# no-work bridge rerun. Methodology of the 2026-09-22/23 bursty census campaign (burst4500c):
#  1) vanilla boundary: fixed-rate SUSTAINED points (300 s each, one deployment, ascending 6500..10000 step 500; the 30 s ramp
#     under-reports loss) -> stop at the first point where vanilla loses spans (SDK spans_dropped > 0; vanilla has no retry,
#     so an agent refusal is a lost span); boundary B = the last clean rate below it.
#  2) BURSTY stationary runs at mean B: wrk2 fork -D pareto, alpha 1.5, cap 2.0 (epoch multiplier 0.74..1.47), epoch 10 s,
#     600 s (60 epochs), seed 1001; refused-trace census ON; 5000 uniformly sampled traces per point (10 sub-windows).
#     Vanilla, then the bridges with the response path (rev, depth_cubic), then forward-only (reverse off).
# Stack = the SN no-work n=5 round (admissionotel500m agents, ClickHouse gateway 2 CPU, queue3 collector, GOMAXPROCS auto,
# backlog margin, gateway LP resource_exhausted, agent compression none, GOGC=off GOMEMLIMIT=1GiB gctrace, FINAL SDK images,
# bridges without reverse_passthrough + priority receiver / SDK HP retry / priority queues). Fresh deployment per case. No smoke.
# usage: setsid nohup snburst_chain.sh > snburst_chain.log 2>&1 < /dev/null &      (DRYRUN=1: derive + verify only)
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod; DS=/users/tomislav/deployments/dsb-sn
PY="$REPO/.venv/bin/python -B -u"; DRYRUN=${DRYRUN:-}
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94  # queue3
SRC=$DS/retctx-snnw-final-src-20260924T1609Z
PROV=$DS/retctx-nwe2e-m5-on-r1-20260920T052509Z
fail() { echo "$(date -u +%H:%M:%S) SNBURST3 FAILED: $*"; exit 1; }
log() { echo "$(date -u +%H:%M:%S) $*"; }
STACK="--provenance-from $PROV --source $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --repetitions 1 --seeds 1001"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
ROOTS=$S/snburst3_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$S/snburst3_dryrun_roots.txt; : > $ROOTS
out() { local r=$DS/retctx-snburst3-$1-$(date -u +%Y%m%dT%H%M%SZ); [ -n "$DRYRUN" ] && r=$S/dryrun/$(basename $r); echo $r; }
check() {  # $1 root, $2 census on|off, $3 trace on|off
  for m in $1/builds/*/manifest.yaml; do
    grep -q GOMEMLIMIT $m || fail "$m lacks GOMEMLIMIT"; ! grep -q reverse_passthrough $m || fail "$m sets reverse_passthrough"
    if [ "$2" = on ]; then grep -q RETCTX_REFUSED_CENSUS $m && grep -q RETCTX_REFUSED_RECORDS $m || fail "$m lacks the census / per-trace records switch"; else ! grep -q RETCTX_REFUSED_CENSUS $m || fail "$m has the census switch"; fi
  done
  python3 - $1 $3 <<'PYV' || fail "$1 plan check"
import sys, json
R, trace = sys.argv[1], sys.argv[2]; p = json.load(open(R + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['repetitions'] == 1 and p['seeds'] == [1001], (p['repetitions'], p['seeds'])
assert (p.get('trace_capture') is not False) == (trace == 'on'), p.get('trace_capture')
print(R.rsplit('/', 1)[1], [c['kind'] for c in json.load(open(R + '/cases.json'))], 'rates', p.get('ramp_rates'), 'generator', p.get('generator'))
PYV
}
# Tomislav-RetCtx (user 2026-09-25 ~06:15Z): snburst3 = the bursty span-loss campaign again with a HIGHER burst cap, 2.25 instead
# of 2.0 (same seed 1001 -> same epoch order; realised peak 11,003 req/s = SB's last clean ramp point, 11k; SB degrades by 12k),
# mean still 7500 (vanilla's last clean sustained rate), and WHOLE pod logs kept (RETCTX_LOG_TAIL=-1) so the per-second SDK and
# agent counters cover the full 600 s point for the burst-mechanism timeline. Census + per-trace records on, 5000 uniform samples.
B=$(cat $S/snburst_boundary.txt); [ "$B" = 7500 ] || fail "unexpected boundary $B"
[ -z "$DRYRUN" ] && echo $$ > $S/snburst3_chain.pid
cd $REPO/utils
# ---- 2) bursty runs at mean B ----
GEN="--rates $B --seconds-per-rate 600 --generator pareto --wrk-binary /users/tomislav/DeathStarBench/wrk2/wrk --burst-alpha 1.5 --burst-cap 2.25 --burst-epoch 10s --census"
NOTE="SN no-work BURSTY span-loss run (current stack): mean $B req/s = vanilla's last clean sustained rate, Pareto alpha 1.5 cap 2.25 epoch 10 s (0.71x..1.58x; realised 5309..11003 req/s, peak = SB's last clean ramp point), 600 s, seed 1001; refused-trace census on; whole pod logs; 5000 uniformly sampled traces."
for group in v rev fwd; do
  case $group in v) KINDS=v; EXTRA="--reverse on";; rev) KINDS="pb cgpb sb"; EXTRA="--reverse on $BR";; fwd) KINDS="pb cgpb sb"; EXTRA="--reverse off $BR";; esac
  R=$(out burst-$group)
  $PY derive_dsb_sn_nw.py $STACK $GEN $EXTRA --kinds $KINDS --out $R --note "$NOTE group=$group." > $S/snburst_${group}_derive.log 2>&1 || fail "derive $group (see $S/snburst_${group}_derive.log)"
  check $R on on; echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  log "run bursty $group ($KINDS) at mean $B: $R"
  RETCTX_LOG_TAIL=-1 RETCTX_REFUSED_BIN=on RETCTX_TRACE_SAMPLE_WINDOWS=10 RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random \
    $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/snburst_${group}_run.log 2>&1 < /dev/null || fail "run $group (see $S/snburst_${group}_run.log)"
  log "done bursty $group"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snburst3"; exit 0; }
log "SNBURST3 COMPLETE"
