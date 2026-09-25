#!/usr/bin/env bash
# Tomislav-RetCtx: after the hotel image build completes: derive a capacity-probe root (nt = the
# highest wall, sb = the heaviest bridge), smoke it, run it. Sets the ramp grid for the n=3 campaign.
set -euo pipefail
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-20260923T1321Z
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
PY="$REPO/.venv/bin/python -B -u"
while true; do
  st=$(python3 -c "import json;print(json.load(open('$SRC/image-build-status.json'))['state'])")
  [ "$st" = complete ] && break
  [ "$st" = failed ] && { echo "CHAIN FAILED: image build failed"; exit 1; }
  sleep 10
done
echo "$(date -u +%H:%M:%S) build complete"
P=/users/tomislav/deployments/dsb-hotel/retctx-hotel-cubic-probe-$(date -u +%Y%m%dT%H%MZ)
echo "$P" > $S/hotel_probe_root.txt
cd $REPO/utils
$PY derive_dsb_sn_nw.py --source $SRC --provenance-from $SRC --out $P --kinds nt sb --repetitions 1 \
  --collector admission --reverse on --rates $(seq 2000 2000 30000) --seconds-per-rate 30 \
  --note "HotelReservation capacity probe: nt and sb, 2000..30000 step 2000, 30 s, admission 50/70, Jaeger/ES, CPD 2..4 depth_cubic. Sets the ramp grid for the n=3 campaign."
echo "$(date -u +%H:%M:%S) derived $P"
$PY run_dsb_sn_nw.py smoke --out $P
echo "$(date -u +%H:%M:%S) smoke passed"
$PY run_dsb_sn_nw.py run --out $P
echo "$(date -u +%H:%M:%S) CHAIN COMPLETE $P"
