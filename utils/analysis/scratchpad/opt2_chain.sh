#!/usr/bin/env bash
# Tomislav-RetCtx: SDK optimization round 1 (wire state beside the span, reflection-free carrier encode/parse):
# full hotel source from the current tree, then gzip-off ramps: SB first, then PB+CGPB, then vanilla. No smoke.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/opt2_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod; PY="$REPO/.venv/bin/python -B -u"
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-opt2src-$(date -u +%Y%m%dT%H%MZ); echo $SRC > $S/opt2_src_root.txt
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
D=$(cat $S/collector_queue3_digest)
cd $REPO
echo "$(date -u +%H:%M:%S) prepare $SRC"
$PY utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC > $S/opt2_prepare.log 2>&1
for k in pb sb v; do n=$(grep -rl "AppendSpanBaggage" $REPO/examples/dsb_hotel/build_${k}_hotel_$(basename $SRC | rev | cut -d- -f1 | rev | tr 'A-Z' 'a-z') 2>/dev/null | wc -l); echo "$(date -u +%H:%M:%S) $k generated files using AppendSpanBaggage: $n"; [ "$n" -gt 0 ] || { echo "CHAIN FAILED: $k lacks the span-baggage hook"; exit 1; }; done
echo "$(date -u +%H:%M:%S) build images"
$PY utils/build_dsb_sn_nw.py --out $SRC --backend-manifest /users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml > $S/opt2_build.log 2>&1
echo "$(date -u +%H:%M:%S) built"
COMMON="--source $SRC --provenance-from $SRC --repetitions 1 --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --rates 10000 12000 14000 16000 18000 20000 22000 --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-pprof"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
cd $REPO/utils
for spec in "sb:sb" "pbcgpb:pb cgpb" "v:v"; do
  name=${spec%%:*}; kinds=${spec#*:}
  R=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-opt2-$name-$(date -u +%Y%m%dT%H%MZ); echo $R > $S/opt2_${name}_root.txt
  extra=$BR; [ "$name" = v ] && extra=""
  $PY derive_dsb_sn_nw.py $COMMON $extra --out $R --kinds $kinds --note "SDK optimization rounds 1+2 (bridge wire state beside the span, no bridge span attributes, wrappers take propagation from the SDK, single-allocation window and export encoding; reflection-free RPC carrier encode + parse), gzip-off best config. No smoke." > $S/opt2_${name}_derive.log 2>&1
  echo "$(date -u +%H:%M:%S) run $name $R"
  bash $S/run_only.sh $R
done
echo "$(date -u +%H:%M:%S) OPT2 CHAIN COMPLETE"
