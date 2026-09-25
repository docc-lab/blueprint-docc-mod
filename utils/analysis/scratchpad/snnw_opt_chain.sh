#!/usr/bin/env bash
# Tomislav-RetCtx: SN no-work round on the optimized SDK (commit 5f021f53), same settings as the n=5 matrix ON rounds
# (source built like retctx-nw-m5-20260920T045217Z; derive like retctx-nwe2e-m5-on-r*): v pb cgpb sb, no smoke.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_opt_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod; PY="$REPO/.venv/bin/python -B -u"
COLLECTOR=10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
BACKEND_MANIFEST=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z/builds/v/manifest.yaml
NEW=/users/tomislav/deployments/dsb-sn/retctx-nw-opt-$(date -u +%Y%m%dT%H%MZ); echo $NEW > $S/snnw_opt_src.txt
mkdir -p $NEW/logs
cd $REPO
echo "$(date -u +%H:%M:%S) prepare $NEW ($(git rev-parse --short HEAD))"
$PY utils/prepare_dsb_sn_nw.py --out $NEW --collector-image $COLLECTOR --kinds v,pb,cgpb,sb > $NEW/logs/prepbuild.log 2>&1
echo "$(date -u +%H:%M:%S) build images"
$PY utils/build_dsb_sn_nw.py --out $NEW --backend-manifest $BACKEND_MANIFEST >> $NEW/logs/prepbuild.log 2>&1
echo "$(date -u +%H:%M:%S) built"
R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-opt-on-$(date -u +%Y%m%dT%H%MZ); echo $R > $S/snnw_opt_root.txt
cd $REPO/utils
$PY derive_dsb_sn_nw.py --source $NEW --provenance-from $NEW --out $R --kinds v pb cgpb sb --repetitions 1 --collector admission --reverse on \
  --rates $(seq -s ' ' 500 500 14000) --seconds-per-rate 30 \
  --note "SN no-work on the optimized SDK (commit 5f021f53: bridge wire state beside the span, no bridge span attributes, reflection-free RPC carrier). Same settings as the n=5 matrix ON rounds (retctx-nwe2e-m5-on-r*): admission collectors (image c16141e6), tuned Jaeger/ES store, reverse on, 500..14000 step 500, 30 s. No smoke." > $S/snnw_opt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run $R"
bash $S/run_only.sh $R
echo "$(date -u +%H:%M:%S) SNNW OPT CHAIN COMPLETE"
