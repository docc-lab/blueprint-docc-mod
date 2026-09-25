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
fail() { echo "$(date -u +%H:%M:%S) SNBURST FAILED: $*"; exit 1; }
log() { echo "$(date -u +%H:%M:%S) $*"; }
STACK="--provenance-from $PROV --source $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --repetitions 1 --seeds 1001"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
ROOTS=$S/snburst_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$S/snburst_dryrun_roots.txt; : > $ROOTS
out() { local r=$DS/retctx-snburst-$1-$(date -u +%Y%m%dT%H%M%SZ); [ -n "$DRYRUN" ] && r=$S/dryrun/$(basename $r); echo $r; }
check() {  # $1 root, $2 census on|off, $3 trace on|off
  for m in $1/builds/*/manifest.yaml; do
    grep -q GOMEMLIMIT $m || fail "$m lacks GOMEMLIMIT"; ! grep -q reverse_passthrough $m || fail "$m sets reverse_passthrough"
    if [ "$2" = on ]; then grep -q RETCTX_REFUSED_CENSUS $m || fail "$m lacks the census switch"; else ! grep -q RETCTX_REFUSED_CENSUS $m || fail "$m has the census switch"; fi
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
if [ -z "$DRYRUN" ]; then
  echo $$ > $S/snburst_chain.pid
  until grep -q "PASSES CHAIN COMPLETE snnw" $S/snnw_nopt_resume.log; do
    grep -q "PASSES CHAIN FAILED" $S/snnw_nopt_resume.log && fail "SN no-work chain failed"; sleep 15; done
  log "SN no-work chain complete; starting the vanilla boundary sweep"
fi
cd $REPO/utils
# ---- 1) vanilla boundary: sustained 300 s points, stop at the first lossy one ----
RATES="6500 7000 7500 8000 8500 9000 9500 10000"
R=$(out sustain-v)
$PY derive_dsb_sn_nw.py $STACK --kinds v --rates $RATES --seconds-per-rate 300 --no-trace-capture --out $R \
  --note "SN no-work vanilla SUSTAINED boundary sweep (current stack): fixed-rate 300 s points ascending, one deployment; stop at the first point with lost spans; the last clean rate is the mean of the bursty span-loss runs." > $S/snburst_sustain_derive.log 2>&1 || fail "derive sustain (see $S/snburst_sustain_derive.log)"
check $R off off; echo $R >> $ROOTS
if [ -n "$DRYRUN" ]; then B=${B:-8000}; else
  log "run vanilla sustained sweep $R (rates $RATES, 300 s each)"
  setsid $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/snburst_sustain_run.log 2>&1 < /dev/null & RP=$!
  B=""; LAST=""
  while :; do
    sleep 20
    VERDICT=$(python3 - $R <<'PYL'
import glob, gzip, re, json, os, sys
R = sys.argv[1]
def vlast(p):
    try: t = gzip.open(p, 'rt', errors='replace').read()
    except FileNotFoundError: return None
    m = re.findall(r'vanilla_processor_metrics (.*)', t)
    return {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m[-1])} if m else None
case = sorted(glob.glob(f'{R}/run/[0-9][0-9]-v'))
pts = sorted(glob.glob(f'{case[0]}/rate-*')) if case else []
out = []
for i, P in enumerate(pts):
    if not os.path.exists(f'{P}/result.json'): break
    d = json.load(open(f'{P}/result.json')); rec = drop = miss = 0
    for x in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
        a, b = vlast(x), vlast(x.replace('/after/', '/before/'))
        if not a and i + 1 < len(pts): a = vlast(x.replace(f'{P}/after/', f'{pts[i + 1]}/before/'))
        if not a or not b: miss += 1; continue
        rec += a.get('spans_received', 0) - b.get('spans_received', 0); drop += a.get('spans_dropped', 0) - b.get('spans_dropped', 0)
    out.append(f"{d['offered_rps']}:{d['completed_rps']:.0f}:{drop}:{rec}:{miss}")
print(' '.join(out))
PYL
)
    [ "$VERDICT" != "$LAST" ] && { LAST=$VERDICT; log "SUSTAIN points (offered:completed:spans lost:spans:missing snapshots) $VERDICT"; }
    PREV=""; for pt in $VERDICT; do IFS=: read off got lost tot miss <<< "$pt"
      if [ "$lost" -gt 0 ]; then B=$PREV; FIRST=$off; break; fi; PREV=$off; done
    if [ -n "${FIRST:-}" ]; then
      log "vanilla first loses spans at $FIRST (sustained 300 s); boundary B=${B:-none}; stopping the sweep"
      kill -TERM -- -$RP 2>/dev/null || true; sleep 5; kill -KILL -- -$RP 2>/dev/null || true; break; fi
    if ! kill -0 $RP 2>/dev/null; then
      wait $RP || true
      PREV=""; for pt in $VERDICT; do IFS=: read off got lost tot miss <<< "$pt"; PREV=$off; done
      [ -n "$PREV" ] || fail "sustained sweep produced no points (see $S/snburst_sustain_run.log)"
      B=$PREV; log "sweep ran to the end without loss; boundary B=$B (top of the grid)"; break; fi
  done
  [ -n "$B" ] || fail "vanilla lost spans already at the lowest sustained rate; widen the grid downward"
  echo $B > $S/snburst_boundary.txt
fi
# ---- 2) bursty runs at mean B ----
GEN="--rates $B --seconds-per-rate 600 --generator pareto --wrk-binary /users/tomislav/DeathStarBench/wrk2/wrk --burst-alpha 1.5 --burst-cap 2.0 --burst-epoch 10s --census"
NOTE="SN no-work BURSTY span-loss run (current stack): mean $B req/s = vanilla's last clean sustained rate, Pareto alpha 1.5 cap 2.0 epoch 10 s (0.74x..1.47x), 600 s, seed 1001; refused-trace census on; 5000 uniformly sampled traces."
for group in v rev fwd; do
  case $group in v) KINDS=v; EXTRA="--reverse on";; rev) KINDS="pb cgpb sb"; EXTRA="--reverse on $BR";; fwd) KINDS="pb cgpb sb"; EXTRA="--reverse off $BR";; esac
  R=$(out burst-$group)
  $PY derive_dsb_sn_nw.py $STACK $GEN $EXTRA --kinds $KINDS --out $R --note "$NOTE group=$group." > $S/snburst_${group}_derive.log 2>&1 || fail "derive $group (see $S/snburst_${group}_derive.log)"
  check $R on on; echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  log "run bursty $group ($KINDS) at mean $B: $R"
  RETCTX_REFUSED_BIN=on RETCTX_TRACE_SAMPLE_WINDOWS=10 RETCTX_TRACE_SAMPLE_SIZE=5000 RETCTX_TRACE_SAMPLE_ORDER=random \
    $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/snburst_${group}_run.log 2>&1 < /dev/null || fail "run $group (see $S/snburst_${group}_run.log)"
  log "done bursty $group"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE snburst"; exit 0; }
log "SNBURST COMPLETE"
