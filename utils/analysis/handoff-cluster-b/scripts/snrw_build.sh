#!/usr/bin/env bash
# Tomislav-RetCtx: SN REAL-WORK source root (prepare_dsb_sn_nw.py --realwork): nt v pb cgpb sb from the current tree
# (SDK 79e9e9fa + A's uncommitted tooling), plan / monitor from the Sept 20 matrix root, then image build + push to this
# cluster's registry. Same recipe as cluster A's snrw_build.sh (A's attempt failed on a corrupted carrier_json.go, fixed).
# usage: bash -l snrw_build.sh   (needs Go on PATH, docker group)  -> prints SNRW BUILD COMPLETE <root> / BUILD FAILED
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state; mkdir -p $ST
REPO=/users/tomislav/blueprint-docc-mod
COLLECTOR=10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
BACKEND_MANIFEST=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z/builds/v/manifest.yaml
PROV=/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z
NEW=/users/tomislav/deployments/dsb-sn/retctx-snrw-src-$(date -u +%Y%m%dT%H%MZ); echo $NEW > $ST/snrw_src_root.txt
mkdir -p $NEW/logs
echo "$(date -u +%H:%M:%S) go $(go version | awk '{print $3}'); prepare $NEW"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/prepare_dsb_sn_nw.py --out $NEW --collector-image $COLLECTOR --kinds nt,v,pb,cgpb,sb --realwork --provenance $PROV" > $NEW/logs/prepbuild.log 2>&1 || { echo "BUILD FAILED: prepare (tail $NEW/logs/prepbuild.log)"; tail -5 $NEW/logs/prepbuild.log; exit 1; }
for k in pb sb v; do n=$(grep -rl "AppendSpanBaggage" $(python3 -c "import json;print([c for c in json.load(open('$NEW/cases.json')) if c['kind']=='$k'][0]['build'])") 2>/dev/null | wc -l); echo "$(date -u +%H:%M:%S) $k files with AppendSpanBaggage: $n"; [ "$n" -gt 0 ] || { echo "BUILD FAILED: $k lacks the span-baggage hook"; exit 1; }; done
n=$(grep -rl "socialnetwork\." $(python3 -c "import json;print([c for c in json.load(open('$NEW/cases.json')) if c['kind']=='v'][0]['build'])") 2>/dev/null | wc -l); echo "$(date -u +%H:%M:%S) v build references socialnetwork workflow in $n files"; [ "$n" -gt 0 ] || { echo "BUILD FAILED: not the real-work workflow"; exit 1; }
echo "$(date -u +%H:%M:%S) build images"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $NEW --backend-manifest $BACKEND_MANIFEST" >> $NEW/logs/prepbuild.log 2>&1 || { echo "BUILD FAILED: images (tail $NEW/logs/prepbuild.log)"; tail -5 $NEW/logs/prepbuild.log; exit 1; }
echo "$(date -u +%H:%M:%S) SNRW BUILD COMPLETE $NEW"
