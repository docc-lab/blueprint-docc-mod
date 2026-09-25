#!/usr/bin/env bash
# Tomislav-RetCtx: full hotel source root (nt v pb cgpb sb) with the current runtime (sdk_retry.go etc.), then
# vanilla + PB runs with agent gzip off (PB with the prioq4 settings), no smoke. Usage: fullsrc_chain.sh
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod; PY="$REPO/.venv/bin/python -B -u"
STAMP=$(date -u +%Y%m%dT%H%MZ)
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-fullsrc-$STAMP; echo $SRC > $S/fullsrc_root.txt
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
D=$(cat $S/collector_queue3_digest)
cd $REPO
echo "$(date -u +%H:%M:%S) prepare $SRC"
$PY utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC > $S/fullsrc_prepare.log 2>&1
echo "$(date -u +%H:%M:%S) build images"
$PY utils/build_dsb_sn_nw.py --out $SRC --backend-manifest /users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml > $S/fullsrc_build.log 2>&1
echo "$(date -u +%H:%M:%S) built"
COMMON="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates 10000 12000 14000 16000 18000 20000 22000 --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none"
cd $REPO/utils
V=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-nocomp-v-$(date -u +%Y%m%dT%H%MZ); echo $V > $S/nocomp_v_root.txt
$PY derive_dsb_sn_nw.py $COMMON --out $V --kinds v --note "Vanilla baseline with agents OTLP exporter compression none (fair comparison with the gzip-off bridges runs); same collectors as fixes500m (memory_limiter agents 500m, 2-CPU gateway), image prio-queue3-20260923. No smoke." > $S/nocomp_v_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run vanilla $V"
bash $S/run_only.sh $V
PB=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-nocomp-pb-$(date -u +%Y%m%dT%H%MZ); echo $PB > $S/nocomp_pb_root.txt
$PY derive_dsb_sn_nw.py $COMMON --out $PB --kinds pb --priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue --note "PB with the prioq4 settings + agents compression none. No smoke." > $S/nocomp_pb_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run pb $PB"
bash $S/run_only.sh $PB
echo "$(date -u +%H:%M:%S) FULLSRC CHAIN COMPLETE"
