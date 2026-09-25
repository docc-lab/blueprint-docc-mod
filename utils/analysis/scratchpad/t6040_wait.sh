#!/usr/bin/env bash
# Tomislav-RetCtx: start the 60/40 threshold round once the 500m round finishes.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
until grep -q 'C500M COMPLETE\|FAILED' $S/c500m.log; do sleep 60; done
grep -q FAILED $S/c500m.log && { echo "$(date -u +%H:%M:%S) 500m failed; not starting 6040" >> $S/t6040.log; exit 1; }
echo "$(date -u +%H:%M:%S) 500m complete; starting 60/40 round" >> $S/t6040.log
exec $S/t6040.sh
