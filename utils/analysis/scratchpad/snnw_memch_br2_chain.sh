#!/usr/bin/env bash
# Tomislav-RetCtx: SN bridges on the hotel opt2 best config + ClickHouse + memory-limit GC, with the depth_cubic reverse
# policy (user 2026-09-24: consistent with hotel). cpd 2..6 (SN depth). Rates 1000..17000 step 1000. pb cgpb sb. No smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/snnw_mem_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
D=$(cat $S/collector_queue3_digest)
COMMON="--source $OPT --provenance-from $PROV --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates $(seq -s ' ' 1000 1000 17000) --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --reverse-policy depth_cubic"
cd /users/tomislav/blueprint-docc-mod/utils
BB=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-memch-br-$(date -u +%Y%m%dT%H%M%SZ)
$PY derive_dsb_sn_nw.py $COMMON $BR --out $BB --kinds pb cgpb sb --note "SN no-work, optimized SDK (5f021f53 images), hotel opt2 best config on ClickHouse, depth_cubic reverse policy (cpd 2..6), GOGC=off GOMEMLIMIT=1GiB gctrace; 1000..17000 step 1000. No smoke." > $S/snnw_memch_br_derive.log 2>&1
for k in pb cgpb sb; do grep -q "reverse_policy: depth_cubic" $BB/builds/$k/manifest.yaml || { echo "CHAIN FAILED: $k manifest lacks depth_cubic"; exit 1; }; grep -q -i elasticsearch $BB/builds/$k/manifest.yaml && { echo "CHAIN FAILED: $k manifest has elasticsearch"; exit 1; }; done
echo $BB > $S/snnw_memch_br_root.txt
echo "$(date -u +%H:%M:%S) derived + verified (depth_cubic, clickhouse) $BB"
bash $S/run_only.sh $BB
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
