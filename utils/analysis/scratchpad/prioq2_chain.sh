#!/usr/bin/env bash
# Tomislav-RetCtx: after the agents-only strict-priority probe completes, derive + smoke + run the probe with the
# strict-priority queue stage on the agents AND the gateway. Usage: prioq2_chain.sh
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PREV=$(cat $S/prioq_root.txt)
until [ -n "${NOWAIT:-}" ] || grep -q "CHAIN COMPLETE" $S/prioq.log; do
  if ! pgrep -f "smoke_then_run.sh $PREV" >/dev/null; then echo "$(date -u +%H:%M:%S) CHAIN FAILED: previous probe ended without completing"; exit 1; fi
  sleep 15
done
SRC=$(cat $S/sdkretry_src_root.txt); D=$(cat $S/collector_queue_digest)
P=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-prioq2-$(date -u +%Y%m%dT%H%MZ); echo $P > $S/prioq2_root.txt
cd /users/tomislav/blueprint-docc-mod/utils
echo "$(date -u +%H:%M:%S) derive $P"
/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $P --kinds pb sb --repetitions 1 \
  --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates 10000 12000 14000 16000 18000 20000 22000 --seconds-per-rate 30 \
  --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --priority-receiver --sdk-retry priority \
  --priority-queue --gateway-priority-queue \
  --note "Strict-priority queue on agents AND gateway: prioq settings (sdkretry settings + agents priority -> batch -> priority/queue, otlp sending_queue off) + gateway priority -> batch (one batcher per priority) -> priority/queue (dispatch_workers 4 = former ClickHouse num_consumers) with the ClickHouse sending_queue off; HP exported first across all agents, queued LP evicted to admit checkpoints at soft/hard. Image prio-queue-20260923. pb sb, 10000..22000 step 2000; comparison = prioq (agents only), sdkretry, pdnocpu; vanilla baseline = fixes500m root." > $S/prioq2_derive.log 2>&1
bash $S/smoke_then_run.sh $P
echo "$(date -u +%H:%M:%S) PRIOQ2 CHAIN COMPLETE $P"
