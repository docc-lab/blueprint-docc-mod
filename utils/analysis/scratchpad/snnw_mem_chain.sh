#!/usr/bin/env bash
# Tomislav-RetCtx: SN no-work with an explicit memory-based GC policy for every application process (GOGC=off,
# GOMEMLIMIT=1GiB, GODEBUG=gctrace=1): no-tracing (matrix images, unchanged code) then v pb cgpb sb (optimized SDK,
# 5f021f53 images). Same matrix settings otherwise. Starts after the running default-GC control completes. No smoke.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_mem_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
CTRL=$(cat $S/snnw_ctrl_root.txt)
# (control stopped by the user; no wait)


M5=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
COMMON="--provenance-from $PROV --repetitions 1 --collector admission --reverse on --rates $(seq -s ' ' 500 500 14000) --seconds-per-rate 30 --app-gc-memlimit 1GiB --app-gctrace"
cd /users/tomislav/blueprint-docc-mod/utils
A=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-nt-$(date -u +%Y%m%dT%H%M%SZ); echo $A > $S/snnw_mem_nt_root.txt
$PY derive_dsb_sn_nw.py --source $M5 $COMMON --out $A --kinds nt --note "SN no-work, no-tracing (matrix images) with GOGC=off GOMEMLIMIT=1GiB gctrace on every application process; matrix settings otherwise. No smoke." > $S/snnw_mem_nt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run nt $A"
bash $S/run_only.sh $A
B=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-opt-$(date -u +%Y%m%dT%H%M%SZ); echo $B > $S/snnw_mem_opt_root.txt
$PY derive_dsb_sn_nw.py --source $OPT $COMMON --out $B --kinds v pb cgpb sb --note "SN no-work, optimized SDK (5f021f53 images) with GOGC=off GOMEMLIMIT=1GiB gctrace on every application process; matrix settings otherwise. No smoke." > $S/snnw_mem_opt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run v pb cgpb sb $B"
bash $S/run_only.sh $B
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
