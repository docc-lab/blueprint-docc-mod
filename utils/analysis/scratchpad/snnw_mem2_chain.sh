#!/usr/bin/env bash
# Tomislav-RetCtx: continuation of the SN memory-limit round with an extended ramp (user 2026-09-24: push rates higher):
# after the running no-tracing ramp (to 14000) completes: v pb cgpb sb on 500..14000 step 500 + 15000..24000 step 1000,
# then a no-tracing tail 15000..24000. GOGC=off GOMEMLIMIT=1GiB gctrace on every application process. No smoke.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_mem_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PY="/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u"
NT=$(cat $S/snnw_mem_nt_root.txt)
until python3 -c "import json,sys;d=json.load(open('$NT/run-status.json'));sys.exit(0 if d.get('state') in ('complete','failed') else 1)" 2>/dev/null; do sleep 15; done
echo "$(date -u +%H:%M:%S) nt ramp ended: $(python3 -c "import json;print(json.load(open('$NT/run-status.json')).get('state'))")"
sleep 20
M5=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z
OPT=$(cat $S/snnw_opt_src.txt)
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
BASE="--provenance-from $PROV --repetitions 1 --collector admission --reverse on --seconds-per-rate 30 --app-gc-memlimit 1GiB --app-gctrace"
cd /users/tomislav/blueprint-docc-mod/utils
B=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-opt-$(date -u +%Y%m%dT%H%M%SZ); echo $B > $S/snnw_mem_opt_root.txt
$PY derive_dsb_sn_nw.py --source $OPT $BASE --rates $(seq -s ' ' 500 500 14000) $(seq -s ' ' 15000 1000 24000) --out $B --kinds v pb cgpb sb \
  --note "SN no-work, optimized SDK (5f021f53 images), GOGC=off GOMEMLIMIT=1GiB gctrace on every application process; ramp extended to 24000 (500..14000 step 500, 15000..24000 step 1000); matrix settings otherwise. No smoke." > $S/snnw_mem_opt_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run v pb cgpb sb $B"
bash $S/run_only.sh $B
T=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-mem-nttail-$(date -u +%Y%m%dT%H%M%SZ); echo $T > $S/snnw_mem_nttail_root.txt
$PY derive_dsb_sn_nw.py --source $M5 $BASE --rates $(seq -s ' ' 15000 1000 24000) --out $T --kinds nt \
  --note "SN no-work, no-tracing (matrix images) memory-limit tail 15000..24000 continuing $NT. No smoke." > $S/snnw_mem_nttail_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run nt tail $T"
bash $S/run_only.sh $T
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
