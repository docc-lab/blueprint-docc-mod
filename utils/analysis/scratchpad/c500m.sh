#!/usr/bin/env bash
# Tomislav-RetCtx: same matrix, collectors at 500m CPU instead of 1 core.
# Memory budget and every processor threshold are unchanged, so CPU is the only
# variable against the n=5 admission matrix. One round, both arms.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/c500m.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Collector CPU probe: identical to the n=5 admission matrix except collectors run 500m CPU instead of 1 core. Memory 256Mi / GOMEMLIMIT 230MiB and all priority thresholds unchanged, so CPU is the only variable. Same images (commit f57a8699)."

for arm in on off; do
  if [ "$arm" = "on" ]; then KINDS="nt v pb cgpb sb"; else KINDS="pb cgpb sb"; fi
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-c500m-$arm-$STAMP
  log "arm $arm -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds $KINDS --repetitions 1 \
      --collector admission500m --reverse $arm --note '$NOTE arm=$arm.'" >>"$LOG" 2>&1 \
    || { log "arm $arm: DERIVE FAILED"; exit 1; }
  echo "$R" >> "$S/c500m_roots.txt"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "arm $arm: SMOKE FAILED"; exit 1; }
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "arm $arm: RUN FAILED"; exit 1; }
  log "arm $arm DONE"
done
log "C500M COMPLETE"
