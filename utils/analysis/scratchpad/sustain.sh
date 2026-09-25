#!/usr/bin/env bash
# Tomislav-RetCtx: SUSTAINED-ACCEPTANCE probe. Fixed-interval arrivals at 3500 / 4000 / 4500 req/s
# for 600 s each, vanilla then cgpb (response path on), same 60/40 collectors and images as the
# stationary runs. Goal: the highest rate with zero refusals AND no growing accepted-minus-exported
# backlog over 10 min; the bursty runs get re-centred just below it.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/sustain.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Sustained-acceptance probe: fixed-interval 3500 4000 4500 req/s for 600 s each, v then cgpb (reverse on), collectors admission6040 (1 CPU, 256Mi, GOMEMLIMIT 230MiB; priority soft 40 / hard 60, vanilla memory_limiter 60/20). Finds the highest rate with zero refusals and no growing exporter backlog over 10 min; the bursty runs are re-centred just below it. Same images (commit f57a8699)."
arm=on
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-sustain-$arm-$STAMP
log "arm $arm -> $R"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
    --source $SRC --out $R --provenance-from $PROV --kinds v cgpb --repetitions 1 \
    --collector admission6040 --reverse $arm --rates 3500 4000 4500 --seconds-per-rate 600 \
    --note '$NOTE'" >>"$LOG" 2>&1 \
  || { log "DERIVE FAILED"; exit 1; }
echo "$R" >> "$S/sustain_roots.txt"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
  || { log "SMOKE FAILED"; exit 1; }
sg docker -c "cd $REPO && RETCTX_TRACE_SAMPLE_WINDOWS=10 .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
  || { log "RUN FAILED"; exit 1; }
log "SUSTAIN COMPLETE"
