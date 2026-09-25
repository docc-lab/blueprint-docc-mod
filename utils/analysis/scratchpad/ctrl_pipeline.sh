#!/usr/bin/env bash
# Tomislav-RetCtx: control ramps -- vanilla and no-tracing on the SAME source tree
# as the optimized PB build, run the same night as the ON/OFF pair.
#
# Why: the optimized-build OFF ramp peaked at 7793 against 8225 for the same
# configuration on 18 Sept, a 5.3% drop, while the three repetitions WITHIN that
# older campaign agreed to 1%. Two explanations fit: day-to-day drift, or the
# optimization regressing something outside the reverse path. Neither vanilla nor
# no-tracing executes any reverse code -- no-tracing has no instrumentation at all --
# so if they are down by a similar margin it is the machine, and if they hold their
# old values the regression is mine.
#
# Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/ctrl_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/ctrl-pipeline.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

log "waiting for control image build in $SRC"
for _ in $(seq 1 240); do
  st=$(python3 -c "import json;print(json.load(open('$SRC/image-build-status.json')).get('state',''))" 2>/dev/null || echo "")
  [ "$st" = "complete" ] && break
  [ "$st" = "failed" ] && { log "BUILD FAILED"; exit 1; }
  sleep 15
done
[ "$st" = "complete" ] || { log "timed out waiting for build (state=$st)"; exit 1; }
log "build complete"

log "waiting for the cluster to be free"
for _ in $(seq 1 240); do
  pgrep -f "run_dsb_sn_nw.py (smoke|run) --out" >/dev/null 2>&1 || break
  sleep 15
done
log "cluster free"

NOTE="Control ramp on the SAME tree as the optimized PB build. Neither kind runs reverse-path code; no-tracing runs no instrumentation at all. Purpose: separate day-to-day drift from a regression introduced outside the reverse path. Admission collectors 1CPU/256Mi, tuned store."

for kind in v nt; do
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-ctrl-$kind-$STAMP
  log "deriving $kind -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds $kind --repetitions 1 \
      --collector admission --reverse off --note '$NOTE kind=$kind.'" >>"$LOG" 2>&1
  echo "$R" > "$S/ctrl_${kind}_root"

  log "$kind: smoke"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "$kind: SMOKE FAILED"; exit 1; }
  log "$kind: run (28 rates)"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "$kind: RUN FAILED"; exit 1; }
  peak=$(python3 - "$R" "$kind" <<'PY'
import glob, json, sys
best = 0.0
for f in glob.glob(f'{sys.argv[1]}/run/01-{sys.argv[2]}/rate-*/result.json'):
    d = json.load(open(f))
    if 'completed_rps' in d:
        best = max(best, d['completed_rps'])
print(f'{best:.0f}')
PY
)
  log "$kind: DONE peak=$peak"
done
log "control ramps complete"
