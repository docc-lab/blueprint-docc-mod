#!/usr/bin/env bash
# Tomislav-RetCtx: event stream for a sweep: a live line every ~60 s while measuring, a POINT DONE
# line (with the finished row) when a result.json appears, and the log's FAILED/COMPLETE lines. Exits on COMPLETE/FAILED.
TAG=${1:-sustainotel}
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
source /users/tomislav/blueprint-docc-mod/.venv/bin/activate
seen=$(mktemp)
while true; do
  R=$(tail -1 $S/${TAG}_roots.txt 2>/dev/null)
  if [ -n "$R" ]; then
    for f in $(find $R/run/ -mindepth 3 -maxdepth 3 -path "*/rate-*" -name result.json 2>/dev/null | sort); do
      grep -qx "$f" $seen && continue
      echo "$f" >> $seen
      case=$(basename $(dirname $(dirname $f)) | cut -d- -f2); rate=$(basename $(dirname $f) | sed 's/rate-0*//')
      echo "POINT DONE $case @ $rate:"
      python3 $S/sustain_status.py $TAG 2>/dev/null | grep -A1 "^  $case  *$rate " | sed 's/^/   /'
    done
  fi
  if grep -q -E "COMPLETE|FAILED" $S/$TAG.log 2>/dev/null; then grep -E "COMPLETE|FAILED" $S/$TAG.log | tail -1; exit 0; fi
  python3 $S/live_line.py $TAG 2>/dev/null || echo "$(date -u +%H:%M:%S) live sample failed"
  sleep 50
done
