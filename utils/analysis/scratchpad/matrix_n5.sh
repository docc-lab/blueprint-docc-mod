#!/usr/bin/env bash
# Tomislav-RetCtx: the full no-work matrix at n=5, on one build, interleaved.
#
# Eight configurations: pb/cgpb/sb with the response path on and off, plus vanilla
# and no-tracing, which have only one configuration each because the runner forces
# REVERSE_TRUSS=off for every non-bridge kind.
#
# Reverse on/off is campaign-level (set_reverse patches the deployments), so a
# single campaign cannot hold both. Running "all ON" then "all OFF" would put the
# two arms hours apart, and the measured night-to-night drift is 4-5% -- several
# times the ~1% spread between repetitions inside one campaign. So instead this
# alternates single-repetition campaigns:
#
#   A: nt v pb cgpb sb, reverse on   (nt and v run reverse-off regardless: their only config)
#   B: pb cgpb sb,      reverse off
#   repeat A,B five times -> n=5 for all eight configurations, every kind spread
#   across the whole run so drift lands on all arms equally rather than on one.
#
# ~40 ramps, roughly 19 hours. Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/matrix-n5.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

SRC=$(cat $S/matrix_src_root)
log "waiting for the unified image build in $SRC"
for _ in $(seq 1 400); do
  st=$(python3 -c "import json;print(json.load(open('$SRC/image-build-status.json')).get('state',''))" 2>/dev/null || echo "")
  [ "$st" = "complete" ] && break
  [ "$st" = "failed" ] && { log "BUILD FAILED"; exit 1; }
  sleep 20
done
[ "$st" = "complete" ] || { log "timed out waiting for build (state=$st)"; exit 1; }
log "build complete; starting matrix"

NOTE="Full no-work matrix at n=5 on one build (commit f57a8699: packed reverse encoding, reverse state off the span). Single-repetition campaigns alternating reverse on/off so both arms see the same drift; night-to-night drift measured at 4-5 percent against ~1 percent within-campaign spread. Admission collectors 1CPU/256Mi, tuned store, CPD 2..6, inverse_depth."

for round in 1 2 3 4 5; do
  for arm in on off; do
    if [ "$arm" = "on" ]; then KINDS="nt v pb cgpb sb"; else KINDS="pb cgpb sb"; fi
    STAMP=$(date -u +%Y%m%dT%H%M%SZ)
    R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-$arm-r$round-$STAMP
    log "round $round arm $arm -> $R"
    sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
        --source $SRC --out $R --provenance-from $PROV --kinds $KINDS --repetitions 1 \
        --collector admission --reverse $arm --note '$NOTE round=$round arm=$arm.'" >>"$LOG" 2>&1 \
      || { log "round $round $arm: DERIVE FAILED"; exit 1; }
    echo "$R" >> "$S/matrix_roots.txt"

    sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
      || { log "round $round $arm: SMOKE FAILED"; exit 1; }
    sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
      || { log "round $round $arm: RUN FAILED"; exit 1; }
    log "round $round arm $arm DONE: $(python3 - "$R" <<'PY'
import glob, json, os, sys
out = []
for case in sorted(os.listdir(sys.argv[1] + '/run')):
    best = 0.0
    for f in glob.glob(f'{sys.argv[1]}/run/{case}/rate-*/result.json'):
        d = json.load(open(f))
        if 'completed_rps' in d:
            best = max(best, d['completed_rps'])
    if best:
        out.append(f'{case[3:]}={best:.0f}')
print(' '.join(out))
PY
)"
  done
done
log "MATRIX COMPLETE"
