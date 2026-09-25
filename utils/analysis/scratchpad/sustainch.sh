#!/usr/bin/env bash
# Tomislav-RetCtx: SUSTAINED-ACCEPTANCE sweep on the CLICKHOUSE backend (gateway collector on
# node-9 with the same priority processor, ClickHouse store, Jaeger-API shim). Fixed-interval
# 4000/5000/6000/7000 req/s for 600 s each, v then cgpb (response path on), admission6040 agents.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
SRC=$(cat $S/matrix_src_root)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z
LOG=$S/sustainch.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
NOTE="Sustained-acceptance sweep on the ClickHouse backend: agents (admission6040, 1 CPU, 256Mi, priority 40/60 or memory_limiter 60/20) export to a node-9 gateway collector (4 CPU, 2Gi, same processor, batch 20000/1s) which writes to ClickHouse 25.8 (16 CPU, 64Gi) via the clickhouse exporter; Jaeger v1 and Elasticsearch removed; a Jaeger-API shim serves trace queries from ClickHouse. Fixed-interval 4000 5000 6000 7000 req/s for 600 s each, v then cgpb (reverse on). Goal: highest rate with zero agent refusals and no growing backlog anywhere in the path."
arm=on
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-sustainch-$arm-$STAMP
log "arm $arm -> $R"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/derive_dsb_sn_nw.py \
    --source $SRC --out $R --provenance-from $PROV --kinds v cgpb --repetitions 1 \
    --collector admission6040 --reverse $arm --backend clickhouse --rates 4000 5000 6000 7000 --seconds-per-rate 600 \
    --note '$NOTE'" >>"$LOG" 2>&1 \
  || { log "DERIVE FAILED"; exit 1; }
echo "$R" >> "$S/sustainch_roots.txt"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R" >"$R/logs/smoke.log" 2>&1 \
  || { log "SMOKE FAILED"; exit 1; }
sg docker -c "cd $REPO && RETCTX_TRACE_SAMPLE_WINDOWS=10 .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" >"$R/logs/run.log" 2>&1 \
  || { log "RUN FAILED"; exit 1; }
log "SUSTAINCH COMPLETE"
