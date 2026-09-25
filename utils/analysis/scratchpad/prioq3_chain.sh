#!/usr/bin/env bash
# Tomislav-RetCtx: build prio-queue2 (intake rule + ultrasoft eviction), validate agent+gateway configs, derive the
# prioq3 root (strict-priority queues on agents AND gateway, same settings as prioq2) and run it WITHOUT a smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo "$(date -u +%H:%M:%S) build image"
bash $S/collector_queue2_build.sh > $S/collector_queue2_build.log 2>&1
D=$(cat $S/collector_queue2_digest)
docker run --rm -v $S/cfg_gw_queue.yaml:/c.yaml:ro --entrypoint /otelcontribcol $D validate --config=/c.yaml
docker run --rm -v $S/cfg_agent_queue.yaml:/c.yaml:ro --entrypoint /otelcontribcol $D validate --config=/c.yaml
echo "$(date -u +%H:%M:%S) image $D validated"
SRC=$(cat $S/sdkretry_src_root.txt)
P=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-prioq3-$(date -u +%Y%m%dT%H%MZ); echo $P > $S/prioq3_root.txt
cd /users/tomislav/blueprint-docc-mod/utils
/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $P --kinds pb sb --repetitions 1 \
  --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates 10000 12000 14000 16000 18000 20000 22000 --seconds-per-rate 30 \
  --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --priority-receiver --sdk-retry priority \
  --priority-queue --gateway-priority-queue \
  --note "Strict-priority queues on agents AND gateway (prioq2 settings) + two rules in the queue stage: (1) new LP refused before decode while any LP batch waits in that hop queue; (2) queued LP evicted at and above the ultrasoft line. Image prio-queue2-20260923. No smoke (run --skip-smoke). pb sb, 10000..22000 step 2000; comparison = prioq2, sdkretry, pdnocpu; vanilla baseline = fixes500m root." > $S/prioq3_derive.log 2>&1
echo "$(date -u +%H:%M:%S) derived $P"
bash $S/run_only.sh $P
