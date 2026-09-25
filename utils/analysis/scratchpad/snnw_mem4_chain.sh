#!/usr/bin/env bash
# Tomislav-RetCtx: SN memory-limit round, bridges after the no-tracing knee (nt: 18000-18500; user stopped nt at 20000):
# v pb cgpb sb on 500..20000 step 500. GOGC=off GOMEMLIMIT=1GiB gctrace on every application process. No smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/snnw_mem_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
BASE="--provenance-from $PROV --repetitions 1 --collector admission --reverse on --seconds-per-rate 30 --app-gc-memlimit 1GiB --app-gctrace"
cd /users/tomislav/blueprint-docc-mod/utils
B=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-opt-$(date -u +%Y%m%dT%H%M%SZ); echo $B > $S/snnw_mem_opt_root.txt
$PY derive_dsb_sn_nw.py --source $OPT $BASE --rates $(seq -s ' ' 500 500 20000) --out $B --kinds v pb cgpb sb \
  --note "SN no-work, optimized SDK (5f021f53 images), GOGC=off GOMEMLIMIT=1GiB gctrace on every application process; ramp 500..20000 step 500; matrix settings otherwise. No smoke." > $S/snnw_mem_opt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run v pb cgpb sb $B"
bash $S/run_only.sh $B
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
