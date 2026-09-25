#!/usr/bin/env bash
# Tomislav-RetCtx: functional test of the per-kind plateau stop + adaptive knee windows (SB no-work hotel, 18k..26k).
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt); D=$(cat $S/collector_queue3_digest)
BASE="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
cd /users/tomislav/blueprint-docc-mod/utils
R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-prototest-$(date -u +%Y%m%dT%H%M%SZ); echo $R > $S/proto_test_root.txt
$PY derive_dsb_sn_nw.py $BASE $BR --plateau-stop --knee-windows --rates $(seq -s ' ' 18000 1000 40000) --out $R --kinds sb --note "Protocol test: per-kind plateau stop + adaptive knee windows. No smoke." > $S/proto_test_derive.log 2>&1
python3 -c "import json;p=json.load(open('$R/plan.json'));print('plan', p.get('plateau_stop'), p.get('knee_windows'))"
bash $S/run_only.sh $R
echo "$(date -u +%H:%M:%S) PROTO TEST COMPLETE"
