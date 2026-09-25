#!/usr/bin/env bash
# Tomislav-RetCtx: trace-validity census over the whole n=5 matrix.
#
# For every ramp point of every configuration, what fraction of captured traces
# is still usable? Vanilla's rule is "no span lost"; a bridge's rule is "no
# checkpoint lost AND every severed fragment reattached to its true nearest
# surviving ancestor", with reconstruction done by the bridges repo's own
# pb0 / cgp0 / sb3.
#
# Pinned to one CPU per physical core (never an SMT sibling), per the repo's
# native-evaluation rule. Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
BIN=/users/tomislav/bridges/bin/dsb_validity
OUT=$S/validity
CPUS=$(cat $S/phys_cpus.txt)
LOG=$S/validity-all.log
mkdir -p "$OUT"
log() { echo "$(date -u +%H:%M:%S) $*" >> "$LOG"; }

log "start; cpus=$CPUS"
n=0
total=$(wc -l < $S/matrix_roots.txt)
while read -r R; do
  [ -d "$R/run" ] || { log "skip (no run dir): $R"; continue; }
  n=$((n+1))
  name=$(basename "$R")
  for case in $(ls "$R/run"); do
    kind=${case#*-}
    [ "$kind" = "nt" ] && continue          # no tracing: no traces to score
    f="$OUT/$name.$case.json"
    [ -s "$f" ] && { log "have $name/$case"; continue; }
    log "root $n/$total $name case $case"
    taskset -c "$CPUS" "$BIN" --root "$R" --case "$case" --workers 20 --out "$f" \
      >> "$OUT/$name.$case.stderr" 2>&1 || { log "FAILED $name/$case"; rm -f "$f"; continue; }
    log "done  $name/$case -> $f"
  done
done < $S/matrix_roots.txt
log "VALIDITY COMPLETE"
