#!/usr/bin/env bash
# Tomislav-RetCtx: stationary BURSTY load just below vanilla's earliest span-loss
# rate (6,500 in every environment measured). Mean 6,000 req/s for 300 s, rate
# modulated per 1 s epoch by a truncated Pareto(1.5) capped at 4x and normalised
# to mean 1 -> epochs range 3,500..14,000 (26 percent above 7,000, 9 percent
# above 10,000). Lulls sit under vanilla's cliff; spikes go through it and through
# the bridges' knee (~7,400). Collectors at the 60/40 thresholds (1 CPU, 256Mi).
# Both arms, all kinds. Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/burst6k.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Stationary bursty load: mean 6000 req/s for 300 s, rate modulated per 1 s epoch by truncated Pareto(alpha 1.5, cap 4) normalised to mean 1 (epochs 3500..14000; peak multiplier 2.33). Mean chosen just below the earliest span-loss rate of vanilla (6500 in the 1-core, 500m and 60/40 environments). Collectors admission6040 (1 CPU, 256Mi, GOMEMLIMIT 230MiB; priority soft 40 / hard 60, vanilla memory_limiter 60/20). Generator: patched wrk2 fork -D pareto, seed 1001, burst schedule deterministic in (seed, epoch); realised offered rate = sent_requests/duration. Same images (commit f57a8699)."
for arm in on off; do
  if [ "$arm" = "on" ]; then KINDS="nt v pb cgpb sb"; else KINDS="pb cgpb sb"; fi
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-burst6k-$arm-$STAMP
  log "arm $arm -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds $KINDS --repetitions 1 \
      --collector admission6040 --reverse $arm --rates 6000 --seconds-per-rate 300 \
      --generator pareto --burst-alpha 1.5 --burst-cap 4 --burst-epoch 1s \
      --note '$NOTE arm=$arm.'" >>"$LOG" 2>&1 \
    || { log "arm $arm: DERIVE FAILED"; exit 1; }
  echo "$R" >> "$S/burst6k_roots.txt"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "arm $arm: SMOKE FAILED"; exit 1; }
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "arm $arm: RUN FAILED"; exit 1; }
  log "arm $arm DONE"
done
log "BURST6K COMPLETE"
