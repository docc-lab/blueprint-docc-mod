#!/usr/bin/env bash
# Tomislav-RetCtx: resume the hotel probe chain at the smoke step (root already derived).
set -euo pipefail
P=$(cat /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/hotel_probe_root.txt)
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
cd $REPO/utils
echo "$(date -u +%H:%M:%S) smoke (resume) $P"
$PY run_dsb_sn_nw.py smoke --out $P
echo "$(date -u +%H:%M:%S) smoke passed"
$PY run_dsb_sn_nw.py run --out $P
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $P"
