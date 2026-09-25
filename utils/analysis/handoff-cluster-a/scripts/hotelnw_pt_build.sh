#!/usr/bin/env bash
# Tomislav-RetCtx: HotelReservation ZERO-WORK bridge images (pb cgpb sb) WITH the reverse_passthrough SDK support
# (runtime/plugins/otelcol reverse_policy.go; default off, set per run by derive --reverse-passthrough). Same recipe as
# cluster A's hotelnw_chain.sh (prepare_dsb_hotel.py --nowork, then build_dsb_sn_nw.py). Waits for the SN real-work build
# on this node to finish first. Exports the built images (digest-preserving) for cluster A.
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
REPO=/users/tomislav/blueprint-docc-mod
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
while pgrep -f "[s]nrw_build.sh" >/dev/null; do sleep 20; done
SRC=/users/tomislav/deployments/dsb-hotel/retctx-hotelnw-pt-src-$(date -u +%Y%m%dT%H%MZ); echo $SRC > $ST/hotelnw_pt_src_root.txt
mkdir -p $(dirname $SRC)
echo "$(date -u +%H:%M:%S) prepare (zero-work, passthrough SDK) $SRC"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC --nowork --kinds pb,cgpb,sb" > $ST/hotelnw_pt_prepare.log 2>&1 || { echo "BUILD FAILED: prepare"; tail -5 $ST/hotelnw_pt_prepare.log; exit 1; }
STAMP=$(basename $SRC | rev | cut -d- -f1 | rev | tr 'A-Z' 'a-z')
for k in pb cgpb sb; do
  d=$REPO/examples/dsb_hotel/build_${k}_hotelnw_${STAMP}
  a=$(grep -rl "AppendSpanBaggage" $d 2>/dev/null | wc -l); p=$(grep -rl "reverse_passthrough" $d 2>/dev/null | wc -l); w=$(grep -rl "hotelnw" $d 2>/dev/null | wc -l)
  echo "$(date -u +%H:%M:%S) $k: AppendSpanBaggage $a files, reverse_passthrough $p files, hotelnw $w files"
  [ "$a" -gt 0 ] && [ "$p" -gt 0 ] && [ "$w" -gt 0 ] || { echo "BUILD FAILED: $k build lacks the hook / passthrough / hotelnw workflow"; exit 1; }
done
echo "$(date -u +%H:%M:%S) build images"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $SRC --backend-manifest /users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml" > $ST/hotelnw_pt_build.log 2>&1 || { echo "BUILD FAILED: images"; tail -5 $ST/hotelnw_pt_build.log; exit 1; }
python3 - $SRC > $ST/hotelnw_pt_refs.txt <<'PY'
import json, sys, glob
refs = set()
for f in glob.glob(sys.argv[1] + '/builds/*/images.json'):
    refs |= {v['image'] for v in json.load(open(f)).values()}
print('\n'.join(sorted(refs)))
PY
echo "$(date -u +%H:%M:%S) export $(wc -l < $ST/hotelnw_pt_refs.txt) images"
rm -rf $H/images/hotelnw-pt-registry
python3 $H/scripts/registry_copy.py export $H/images/hotelnw-pt-registry $(cat $ST/hotelnw_pt_refs.txt) > $ST/hotelnw_pt_export.log 2>&1 || { echo "BUILD FAILED: export"; exit 1; }
touch $ST/hotelnw_pt_build.done
echo "$(date -u +%H:%M:%S) HOTELNW PT BUILD COMPLETE $SRC"
