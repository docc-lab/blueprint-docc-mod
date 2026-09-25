#!/usr/bin/env bash
# Tomislav-RetCtx: same-day controls for the SN no-work optimized-SDK round: the n=5 matrix's OWN images (source
# retctx-nw-m5-20260920T045217Z, commit f57a8699) for vanilla and no-tracing, same settings, after the current run ends.
set -euo pipefail
echo $$ > /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_ctrl_chain.pid
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
CUR=$(cat $S/snnw_opt_root.txt)
until python3 -c "import json,sys;d=json.load(open('$CUR/run-status.json'));sys.exit(0 if d.get('state') in ('complete','failed') else 1)" 2>/dev/null; do sleep 20; done
echo "$(date -u +%H:%M:%S) current run ended: $(python3 -c "import json;print(json.load(open('$CUR/run-status.json')).get('state'))")"
M5=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
cd /users/tomislav/blueprint-docc-mod
git stash list >/dev/null
# derive checks the CURRENT tree's application hashes against the provenance campaign; the matrix images are reused by digest
R=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-ctrl-on-$(date -u +%Y%m%dT%H%M%SZ); echo $R > $S/snnw_ctrl_root.txt
cd utils
/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u derive_dsb_sn_nw.py --source $M5 --provenance-from $PROV --out $R --kinds v nt --repetitions 1 --collector admission --reverse on \
  --rates $(seq -s ' ' 500 500 14000) --seconds-per-rate 30 \
  --note "Same-day control for the optimized-SDK SN round: the n=5 matrix's own images (commit f57a8699) for vanilla and no-tracing, same settings. No smoke." > $S/snnw_ctrl_derive.log 2>&1
echo "$(date -u +%H:%M:%S) run $R"
bash $S/run_only.sh $R
echo "$(date -u +%H:%M:%S) SNNW CTRL CHAIN COMPLETE"
