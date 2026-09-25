#!/usr/bin/env bash
# Tomislav-RetCtx: BURSTY stationary runs on the ClickHouse backend with the gateway capped at 2 CPU
# and the OTel Helm-default memory percentages (admissionotel). Mean 4500 req/s = vanilla's last
# clean fixed-rate point on this pipeline (5000 refused 18.8 percent). Pareto bursts alpha 1.5,
# cap 2.0 (multiplier 0.74..1.47 -> 3300..6600 req/s, under the bridges knee), epoch 10 s so one
# burst is long enough to fill collector headroom, 600 s = 60 epochs. ON arm v pb cgpb sb, then OFF pb cgpb sb.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/burst4500.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Bursty stationary run on the ClickHouse backend: agents admissionotel (1 CPU, 256Mi, priority 55/80 or memory_limiter 80/25) export to a node-9 gateway collector capped at 2 CPU (2Gi, same processor, batch 20000/1s, clickhouse exporter), ClickHouse 25.8 store, Jaeger-API shim. Mean 4500 req/s (vanilla last clean fixed point; 5000 refused 18.8 percent), Pareto bursts alpha 1.5 cap 2.0 epoch 10 s (0.74x..1.47x = 3300..6600 req/s), 600 s. Hypothesis: bursts push vanilla over its boundary and it loses all classes; bridges shed LP through bursts and keep checkpoints."
for arm in on off; do
  if [ "$arm" = "on" ]; then KINDS="v pb cgpb sb"; else KINDS="pb cgpb sb"; fi
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-burst4500-$arm-$STAMP
  log "arm $arm -> $R"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
      --source $SRC --out $R --provenance-from $PROV --kinds $KINDS --repetitions 1 \
      --collector admissionotel --reverse $arm --backend clickhouse --gateway-cpu 2 \
      --rates 4500 --seconds-per-rate 600 \
      --generator pareto --wrk-binary /users/tomislav/DeathStarBench/wrk2/wrk --burst-alpha 1.5 --burst-cap 2.0 --burst-epoch 10s \
      --note '$NOTE arm=$arm.'" >>"$LOG" 2>&1 \
    || { log "arm $arm: DERIVE FAILED"; exit 1; }
  echo "$R" >> "$S/burst4500_roots.txt"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
    || { log "arm $arm: SMOKE FAILED"; exit 1; }
  sg docker -c "cd $REPO && RETCTX_TRACE_SAMPLE_WINDOWS=10 .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
    || { log "arm $arm: RUN FAILED"; exit 1; }
  log "arm $arm DONE"
done
log "BURST4500 COMPLETE"
