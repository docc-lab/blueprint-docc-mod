#!/usr/bin/env bash
# Tomislav-RetCtx: SN no-work same-day controls FIRST (the n=5 matrix's own images, commit f57a8699: no-tracing, vanilla),
# then the optimized-SDK bridges (pb cgpb sb) from source retctx-nw-opt-20260924T0145Z. Same matrix settings, no smoke.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_ctrl2_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
M5=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
OPT=$(cat $S/snnw_opt_src.txt)
RATES="$(seq -s ' ' 500 500 14000)"
cd /users/tomislav/blueprint-docc-mod/utils
C=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-ctrl-on-$(date -u +%Y%m%dT%H%M%SZ); echo $C > $S/snnw_ctrl_root.txt
$PY derive_dsb_sn_nw.py --source $M5 --provenance-from $PROV --out $C --kinds nt v --repetitions 1 --collector admission --reverse on --rates $RATES --seconds-per-rate 30 \
  --note "Same-day control for the optimized-SDK SN round: the n=5 matrix's own images (commit f57a8699) for no-tracing and vanilla, same settings. No smoke." > $S/snnw_ctrl_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run control $C"
bash $S/run_only.sh $C
B=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-opt-on-br-$(date -u +%Y%m%dT%H%M%SZ); echo $B > $S/snnw_optbr_root.txt
$PY derive_dsb_sn_nw.py --source $OPT --provenance-from $PROV --out $B --kinds pb cgpb sb --repetitions 1 --collector admission --reverse on --rates $RATES --seconds-per-rate 30 \
  --note "SN no-work on the optimized SDK (commit 5f021f53), bridges; vanilla of this build is retctx-nwe2e-opt-on-20260924T020919Z. Same settings as the n=5 matrix ON rounds. No smoke." > $S/snnw_optbr_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run bridges $B"
bash $S/run_only.sh $B
echo "$(date -u +%H:%M:%S) SNNW CTRL2 CHAIN COMPLETE"
