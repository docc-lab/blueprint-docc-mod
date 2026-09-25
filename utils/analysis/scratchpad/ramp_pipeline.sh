#!/usr/bin/env bash
# Tomislav-RetCtx: full 28-rate PB ramps on the optimized SDK build, reverse ON
# then reverse OFF, back to back in one session.
#
# Both in one session on purpose. PathBridgeProcessor.OnStart -- forward-path code
# untouched by any of this work -- measured 6.88-7.09s in one batch of runs and
# 11.38-11.87s in another, so comparing a new ON run against an OFF baseline from a
# different sitting is not sound. Reverse on/off is an env var, not a different
# image, so both ramps run the same digests minutes apart.
#
# Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/prof_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/ramp-pipeline.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

# Wait for any run still holding the cluster (the OFF profiling point).
log "waiting for the cluster to be free"
for _ in $(seq 1 240); do
  pgrep -f "run_dsb_sn_nw.py (smoke|run) --out" >/dev/null 2>&1 || break
  sleep 15
done
log "cluster free"

NOTE_COMMON="28-rate PB ramp on the OPTIMIZED SDK build: packed reverse encoding, reverse span state off the OTel span (sharded table beside the processor), PrepareCheckpoint takes and returns the carrier. Admission collectors 1CPU/256Mi, tuned store, CPD 2..6, inverse_depth. ON and OFF ramps run back to back in one session so the comparison is not across sittings."

for rev in on off; do
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-optsdk-$rev-$STAMP
  log "deriving $rev -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds pb --repetitions 1 \
      --collector admission --reverse $rev --note '$NOTE_COMMON reverse=$rev.'" >>"$LOG" 2>&1
  echo "$R" > "$S/ramp_${rev}_root"

  log "$rev: smoke"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "$rev: SMOKE FAILED"; exit 1; }
  log "$rev: run (28 rates)"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "$rev: RUN FAILED"; exit 1; }
  peak=$(python3 - "$R" <<'PY'
import glob, json, sys
best = 0.0
for f in glob.glob(sys.argv[1] + '/run/01-pb/rate-*/result.json'):
    d = json.load(open(f))
    if 'completed_rps' in d:
        best = max(best, d['completed_rps'])
print(f'{best:.0f}')
PY
)
  log "$rev: DONE peak=$peak"
done
log "both ramps complete"
