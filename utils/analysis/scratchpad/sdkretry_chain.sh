#!/usr/bin/env bash
# Tomislav-RetCtx: SDK one-shot HP retry probe. New hotel source root (pb, sb) built with the current
# runtime (sdk_retry.go), then the pdnocpu probe settings + --sdk-retry priority. Usage: sdkretry_chain.sh
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
STAMP=$(date -u +%Y%m%dT%H%MZ)
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-sdkretrysrc-$STAMP
P=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-sdkretry-$STAMP
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
COL_RUN=10.10.1.1:30000/otelcontribcol@sha256:6f56145875453477899d40473d9fff68b57a6632038d84d28e6ecd38fdd685ee
echo "$SRC" > $S/sdkretry_src_root.txt; echo "$P" > $S/sdkretry_root.txt
cd $REPO
echo "$(date -u +%H:%M:%S) prepare $SRC"
$PY utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC --kinds pb,sb > $S/sdkretry_prepare.log 2>&1
stamp=$(echo $STAMP | tr 'A-Z' 'a-z')
for k in pb sb; do
  n=$(grep -rl "BRIDGES_RETRY" $REPO/examples/dsb_hotel/build_${k}_hotel_${stamp} 2>/dev/null | wc -l)
  echo "$(date -u +%H:%M:%S) $k generated files carrying BRIDGES_RETRY: $n"
  [ "$n" -gt 0 ] || { echo "$(date -u +%H:%M:%S) CHAIN FAILED: $k build output lacks sdk_retry.go"; exit 1; }
done
echo "$(date -u +%H:%M:%S) build images"
$PY utils/build_dsb_sn_nw.py --out $SRC --backend-manifest /users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml > $S/sdkretry_build.log 2>&1
echo "$(date -u +%H:%M:%S) derive $P"
cd $REPO/utils
$PY derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $P --kinds pb sb --repetitions 1 \
  --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on \
  --rates 10000 12000 14000 16000 18000 20000 22000 --seconds-per-rate 30 \
  --collector-image $COL_RUN --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted \
  --priority-receiver --sdk-retry priority \
  --note "SDK one-shot HP retry probe: pdnocpu settings (priority processor + pre-decode refusals, backlog margin, gateway LP non-retryable, GOMAXPROCS = CPU limit, no CPU signal, image prio-predecode-20260923) + BRIDGES_RETRY=hp (a refused checkpoint batch is retried once after 300 ms + up to 50 percent jitter; LP batches are dropped locally while any HP retry is pending). Services rebuilt with sdk_retry.go (source root sdkretrysrc). pb sb, 10000..22000 step 2000; comparison = pdnocpu root, vanilla baseline = fixes500m root." > $S/sdkretry_derive.log 2>&1
bash $S/smoke_then_run.sh $P
echo "$(date -u +%H:%M:%S) SDKRETRY CHAIN COMPLETE $P"
