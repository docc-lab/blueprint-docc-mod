#!/usr/bin/env bash
# Tomislav-RetCtx: SN memory-limit round, no-tracing first (user 2026-09-24: keep running no-tracing):
# nt tail 14500..30000 step 500 continuing retctx-nwe2e-mem-nt-20260924T025903Z, then v pb cgpb sb on 500..24000 step 500.
# GOGC=off GOMEMLIMIT=1GiB gctrace on every application process. No smoke.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/snnw_mem_chain.pid
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
M5=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
BASE="--provenance-from $PROV --repetitions 1 --collector admission --reverse on --seconds-per-rate 30 --app-gc-memlimit 1GiB --app-gctrace"
cd /users/tomislav/blueprint-docc-mod/utils
T=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-nttail-$(date -u +%Y%m%dT%H%M%SZ); echo $T > $S/snnw_mem_nttail_root.txt
$PY derive_dsb_sn_nw.py --source $M5 $BASE --rates $(seq -s ' ' 14500 500 30000) --out $T --kinds nt \
  --note "SN no-work, no-tracing (matrix images) memory-limit tail 14500..30000 continuing retctx-nwe2e-mem-nt-20260924T025903Z. No smoke." > $S/snnw_mem_nttail_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run nt tail $T"
bash $S/run_only.sh $T
B=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-opt-$(date -u +%Y%m%dT%H%M%SZ); echo $B > $S/snnw_mem_opt_root.txt
$PY derive_dsb_sn_nw.py --source $OPT $BASE --rates $(seq -s ' ' 500 500 24000) --out $B --kinds v pb cgpb sb \
  --note "SN no-work, optimized SDK (5f021f53 images), GOGC=off GOMEMLIMIT=1GiB gctrace on every application process; ramp 500..24000 step 500; matrix settings otherwise. No smoke." > $S/snnw_mem_opt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run v pb cgpb sb $B"
bash $S/run_only.sh $B
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
