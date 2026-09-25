#!/usr/bin/env bash
# Tomislav-RetCtx: smoke then run one derived root. Usage: smoke_then_run.sh <root>
set -euo pipefail
P=$1
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
cd $REPO/utils
echo "$(date -u +%H:%M:%S) smoke $P"
$PY run_dsb_sn_nw.py smoke --out $P
echo "$(date -u +%H:%M:%S) smoke passed"
$PY run_dsb_sn_nw.py run --out $P
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $P"
