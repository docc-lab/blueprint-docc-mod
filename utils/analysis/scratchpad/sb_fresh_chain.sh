#!/usr/bin/env bash
# Tomislav-RetCtx: fresh-deployment test (user 2026-09-24): does the knee-region p99 spike come back after a FRESH deploy?
# Same stack and derive flags as hotelnw_chain2.sh, SB only, 1000..27000 step 1000, normal runner path (teardown, deploy,
# warm-up, points). Waits for the second-ramp script on the live deployment to finish first.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/sb_fresh_chain.pid
until grep -q "RAMP2 DONE" $S/full_ramp_sb.log; do sleep 3; done
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
SRC=$(cat $S/hotelnw_src_root.txt); D=$(cat $S/collector_queue3_digest)
BASE="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
cd /users/tomislav/blueprint-docc-mod/utils
R=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-sbfresh-$(date -u +%Y%m%dT%H%M%SZ); echo $R > $S/sb_fresh_root.txt
$PY derive_dsb_sn_nw.py $BASE $BR --rates $(seq -s ' ' 1000 1000 27000) --out $R --kinds sb --note "Fresh-deployment test: SB no-work hotel, identical to retctx-hotelnw-mem-br-20260924T071346Z sb, 1k..27k. No smoke." > $S/sb_fresh_derive.log 2>&1
grep -q "reverse_policy: depth_cubic" $R/builds/sb/manifest.yaml && grep -q -i clickhouse $R/builds/sb/manifest.yaml || { echo "CHAIN FAILED: manifest check"; exit 1; }
echo "$(date -u +%H:%M:%S) run fresh SB $R"
bash $S/run_only.sh $R
echo "$(date -u +%H:%M:%S) SB FRESH COMPLETE"
