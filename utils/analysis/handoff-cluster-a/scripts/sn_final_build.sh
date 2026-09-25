#!/usr/bin/env bash
# Tomislav-RetCtx: FINAL SDK images for SN (user 2026-09-24: last rebuild): passthrough + bounded refused-trace census, census
# OFF by default. 1) SN REAL-WORK nt v pb cgpb sb (snrw_build.sh recipe); 2) SN NO-WORK v pb cgpb sb (prepare_dsb_sn_nw.py
# without --realwork; nt is untraced and keeps the matrix images). Same backend manifest / provenance / collector as before.
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
REPO=/users/tomislav/blueprint-docc-mod
COLLECTOR=10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
BACKEND_MANIFEST=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z/builds/v/manifest.yaml
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
check() {  # root kinds
  for k in $2; do
    d=$(python3 -c "import json;print([c for c in json.load(open('$1/cases.json')) if c['kind']=='$k'][0]['build'])")
    a=$(grep -rl AppendSpanBaggage $d | wc -l); p=$(grep -rl reverse_passthrough $d | wc -l); c=$(grep -rl RETCTX_REFUSED_CENSUS $d | wc -l)
    echo "$(date -u +%H:%M:%S) $k: AppendSpanBaggage $a, reverse_passthrough $p, RETCTX_REFUSED_CENSUS $c files"
    [ "$a" -gt 0 ] && [ "$p" -gt 0 ] && [ "$c" -gt 0 ] || { echo "BUILD FAILED: $k lacks hook / passthrough / census switch"; exit 1; }
  done
}
for mode in rw nw; do
  NEW=/users/tomislav/deployments/dsb-sn/retctx-sn${mode}-final-src-$(date -u +%Y%m%dT%H%MZ); echo $NEW > $ST/sn${mode}_final_src_root.txt
  mkdir -p $NEW/logs
  if [ $mode = rw ]; then KINDS=nt,v,pb,cgpb,sb; FLAG=--realwork; else KINDS=v,pb,cgpb,sb; FLAG=; fi
  echo "$(date -u +%H:%M:%S) prepare SN $mode ($KINDS) $NEW"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/prepare_dsb_sn_nw.py --out $NEW --collector-image $COLLECTOR --kinds $KINDS $FLAG --provenance $PROV" > $NEW/logs/prepbuild.log 2>&1 || { echo "BUILD FAILED: prepare $mode"; tail -5 $NEW/logs/prepbuild.log; exit 1; }
  check $NEW "v pb cgpb sb"
  echo "$(date -u +%H:%M:%S) build images SN $mode"
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $NEW --backend-manifest $BACKEND_MANIFEST" >> $NEW/logs/prepbuild.log 2>&1 || { echo "BUILD FAILED: images $mode"; tail -5 $NEW/logs/prepbuild.log; exit 1; }
  echo "$(date -u +%H:%M:%S) SN $mode FINAL BUILD COMPLETE $NEW"
done
echo "$(date -u +%H:%M:%S) SN FINAL BUILDS COMPLETE"
