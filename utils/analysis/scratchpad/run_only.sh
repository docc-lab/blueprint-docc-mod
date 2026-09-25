#!/usr/bin/env bash
# Tomislav-RetCtx: run one derived root WITHOUT the smoke ramp (user 2026-09-23). Usage: run_only.sh <root>
set -euo pipefail
P=$1
cd /users/tomislav/blueprint-docc-mod/utils
echo "$(date -u +%H:%M:%S) run (no smoke) $P"
/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u run_dsb_sn_nw.py run --out $P --skip-smoke
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $P"
