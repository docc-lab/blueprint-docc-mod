#!/usr/bin/env bash
# Tomislav-RetCtx (2026-09-24 23:4xZ): resume SN no-work bridges (pb cgpb sb, no passthrough) on the SAME verified root after
# PB repetition 1's warm-up hit a startup race (wrk2api -> composepost "connection refused" in the first second, 70 non-2xx;
# the runner aborts on any warm-up error; nothing had been measured). The runner sets the failed case aside
# (-interrupted-) and starts the group from a fresh deployment. Same plan / manifests as verified by snnw_nopt_chain.sh.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod; PY="$REPO/.venv/bin/python -B -u"
R=$(grep -- '-snnw-nopt-br-' $S/snnw_nopt_roots.txt | tail -1)
echo "$(date -u +%H:%M:%S) resume SN no-work pb cgpb sb x 5 reps (no passthrough) $R"
cd $REPO/utils
$PY run_dsb_sn_nw.py run --out $R --skip-smoke > $S/snnw_nopt_br_run2.log 2>&1 || { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: run br resume (see $S/snnw_nopt_br_run2.log)"; exit 1; }
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE snnw no-work n=5 (bridges resumed after a warm-up startup race)"
