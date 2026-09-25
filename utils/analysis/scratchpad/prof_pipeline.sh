#!/usr/bin/env bash
# Tomislav-RetCtx: carry the profiling pipeline from image build through capture
# without a human in the loop. Detached-safe (run under setsid): waits for the
# build, derives the run root, starts the run, and arms the multi-service capture.
#
# Usage: prof_pipeline.sh <tag>      e.g. prof_pipeline.sh opt2
set -eu
TAG=$1
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
REPO=/users/tomislav/blueprint-docc-mod
LOG=$S/pipeline-$TAG.log
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

SRC=$(cat $S/prof_src_root)
log "waiting for image build in $SRC"
for _ in $(seq 1 240); do
  st=$(python3 -c "import json;print(json.load(open('$SRC/image-build-status.json')).get('state',''))" 2>/dev/null || echo "")
  [ "$st" = "complete" ] && break
  [ "$st" = "failed" ] && { log "BUILD FAILED"; exit 1; }
  sleep 15
done
[ "$st" = "complete" ] || { log "timed out waiting for build (state=$st)"; exit 1; }
log "build complete; deriving run root"

R=$(sg docker -c "cd $REPO && .venv/bin/python -B $S/prof_setup.py on $TAG" 2>>"$LOG" | tail -1)
[ -n "$R" ] || { log "derive produced no root"; exit 1; }
echo "$R" > "$S/prof_${TAG}_root"
log "root $R"

setsid nohup sg docker -c "cd $REPO && .venv/bin/python -B -u utils/run_dsb_sn_nw.py smoke --out $R && .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out $R" \
  > "$R/logs/chain.log" 2>&1 < /dev/null &
log "run chain started"

sleep 3
setsid nohup "$S/capture_multi.sh" "$R" "$S/prof4-$TAG" 45 \
  composepost-service-pb:16100 text-service-pb:16101 user-service-pb:16102 post-storage-service-pb:16103 \
  > /dev/null 2>&1 < /dev/null &
log "capture armed"
