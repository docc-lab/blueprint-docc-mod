#!/usr/bin/env bash
# Tomislav-RetCtx: SN no-work memory-limit round on the CURRENT best config (user 2026-09-24: ClickHouse, not Jaeger):
# otelcol + ClickHouse gateway (2 CPU), admissionotel500m agents, queue3 collector image, GOMAXPROCS auto, backlog margin,
# gateway LP resource_exhausted, agents otlp compression none; bridges add priority receiver, SDK one-shot HP retry, queue
# stage at agents + gateway (the hotel opt2 best config). GOGC=off GOMEMLIMIT=1GiB gctrace on every application service.
# Optimized-SDK SN images (5f021f53). Rates 1000..17000 step 1000. v first, then pb cgpb sb. No smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/snnw_mem_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
D=$(cat $S/collector_queue3_digest)
COMMON="--source $OPT --provenance-from $PROV --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates $(seq -s ' ' 1000 1000 17000) --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
NOTE="SN no-work, optimized SDK (5f021f53 images), hotel opt2 best config on ClickHouse, GOGC=off GOMEMLIMIT=1GiB gctrace; 1000..17000 step 1000. No smoke."
cd /users/tomislav/blueprint-docc-mod/utils
V=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-memch-v-$(date -u +%Y%m%dT%H%M%SZ); echo $V > $S/snnw_memch_v_root.txt
$PY derive_dsb_sn_nw.py $COMMON --out $V --kinds v --note "$NOTE" > $S/snnw_memch_v_derive.log 2>&1
sleep 1
BB=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-memch-br-$(date -u +%Y%m%dT%H%M%SZ); echo $BB > $S/snnw_memch_br_root.txt
$PY derive_dsb_sn_nw.py $COMMON $BR --out $BB --kinds pb cgpb sb --note "$NOTE" > $S/snnw_memch_br_derive.log 2>&1
echo "$(date -u +%H:%M:%S) derived $V $BB"
bash $S/run_only.sh $V
bash $S/run_only.sh $BB
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
