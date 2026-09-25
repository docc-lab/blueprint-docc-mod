#!/usr/bin/env bash
# Tomislav-RetCtx: CONTROL for the bursty 6k run. Same mean the bursty run
# realised (5,570 req/s), same 300 s duration, same 60/40 collectors, but a
# FIXED-interval arrival process (the historical generator). Separates "five
# minutes at ~5,600" from "bursts": if vanilla's 21 percent and pb-on's
# late checkpoint refusals reappear here, duration is the cause; if they do
# not, burstiness is. Detached-safe: launch under setsid AFTER burst6k finishes.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/fixed5570.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Fixed-interval control for the bursty 6k run: 5570 req/s (the realised mean of the Pareto run) for 300 s, historical wrk2 generator, collectors admission6040 (1 CPU, 256Mi, GOMEMLIMIT 230MiB; priority soft 40 / hard 60, vanilla memory_limiter 60/20). Isolates run duration from burstiness. Same images (commit f57a8699)."
for arm in on off; do
  if [ "$arm" = "on" ]; then KINDS="v pb cgpb"; else KINDS="pb cgpb"; fi
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-fixed5570-$arm-$STAMP
  log "arm $arm -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds $KINDS --repetitions 1 \
      --collector admission6040 --reverse $arm --rates 5570 --seconds-per-rate 300 \
      --note '$NOTE arm=$arm.'" >>"$LOG" 2>&1 \
    || { log "arm $arm: DERIVE FAILED"; exit 1; }
  echo "$R" >> "$S/fixed5570_roots.txt"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "arm $arm: SMOKE FAILED"; exit 1; }
  sg docker -c "cd $REPO && RETCTX_TRACE_SAMPLE_WINDOWS=10 .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "arm $arm: RUN FAILED"; exit 1; }
  log "arm $arm DONE"
done
log "FIXED5570 COMPLETE"
