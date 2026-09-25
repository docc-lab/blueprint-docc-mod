#!/usr/bin/env bash
# Tomislav-RetCtx: after the running bursty rerun completes, rebuild the service images with the
# SDK refused-trace census (prepare + build a new source root), then launch burst4500c.sh from it.
set -u
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
LOG=$S/census_build.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }
COLLECTOR=10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
BACKEND_MANIFEST=/users/tomislav/deployments/dsb-sn/retctx-nw-m5-20260920T045217Z/builds/v/manifest.yaml
until grep -q -E "BURST4500B COMPLETE|FAILED" $S/burst4500b.log; do sleep 30; done
log "bursty rerun finished; building census images"
NEW=/users/tomislav/deployments/dsb-sn/retctx-nw-census-$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p $NEW/logs
echo "$NEW" > $S/census_src_root
log "prepare -> $NEW"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/prepare_dsb_sn_nw.py --out $NEW --collector-image $COLLECTOR --kinds v,pb,cgpb,sb" > $NEW/logs/prepbuild.log 2>&1 \
  || { log "PREPARE FAILED"; exit 1; }
log "prepare done; building images"
sg docker -c "cd $REPO && .venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $NEW --backend-manifest $BACKEND_MANIFEST" >> $NEW/logs/prepbuild.log 2>&1 \
  || { log "IMAGE BUILD FAILED"; exit 1; }
log "images built: $(python3 -c "import json;print(json.load(open('$NEW/image-build-status.json')))")"
log "launching burst4500c"
bash $S/burst4500c.sh
log "CENSUS CHAIN DONE"
