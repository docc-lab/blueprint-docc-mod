#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK traced images (v pb cgpb sb) from A's tree = passthrough SDK + the bounded
# refused-trace census fix (bounded) + census OFF by default (user 2026-09-24: no census during performance runs). Same recipe as
# hotelnw_chain.sh / hotelnw_pt_build.sh (prepare_dsb_hotel.py --nowork, build_dsb_sn_nw.py). nt is untraced: unchanged.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-fix-src-$(date -u +%Y%m%dT%H%MZ); echo $SRC > $S/hotelnw_fix_src_root.txt
cd $REPO
echo "$(date -u +%H:%M:%S) prepare (zero-work, passthrough + census fix) $SRC"
.venv/bin/python -B -u utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC --nowork --kinds v,pb,cgpb,sb > $S/hotelnw_fix_prepare.log 2>&1 || { echo "BUILD FAILED: prepare"; tail -5 $S/hotelnw_fix_prepare.log; exit 1; }
STAMP=$(basename $SRC | rev | cut -d- -f1 | rev | tr 'A-Z' 'a-z')
for k in v pb cgpb sb; do
  d=$REPO/examples/dsb_hotel/build_${k}_hotelnw_${STAMP}
  a=$(grep -rl "AppendSpanBaggage" $d | wc -l); p=$(grep -rl "reverse_passthrough" $d | wc -l); c=$(grep -rl "RETCTX_REFUSED_CENSUS" $d | wc -l); w=$(grep -rl "hotelnw" $d | wc -l)
  echo "$(date -u +%H:%M:%S) $k: AppendSpanBaggage $a, reverse_passthrough $p, refusedDedupWindow/RETCTX_REFUSED_CENSUS $c, hotelnw $w files"
  [ "$a" -gt 0 ] && [ "$p" -gt 0 ] && [ "$c" -gt 0 ] && [ "$w" -gt 0 ] || { echo "BUILD FAILED: $k build lacks hook / passthrough / census fix / hotelnw"; exit 1; }
done
echo "$(date -u +%H:%M:%S) build images"
.venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $SRC --backend-manifest /users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml > $S/hotelnw_fix_build.log 2>&1 || { echo "BUILD FAILED: images"; tail -5 $S/hotelnw_fix_build.log; exit 1; }
echo "$(date -u +%H:%M:%S) HOTELNW FIX BUILD COMPLETE $SRC"
