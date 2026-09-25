#!/usr/bin/env bash
# Tomislav-RetCtx: span-loss chain event stream: new chain-log lines (runs, done, COMPLETE/FAILED) and, every ~2 min while a
# bursty point measures, one live line (current epoch rate from wrk's burst trace + agent/gateway accepted/refused over 10 s).
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
source /users/tomislav/blueprint-docc-mod/.venv/bin/activate
n=$(wc -l < $S/snburst3_chain.log)
while true; do
  m=$(wc -l < $S/snburst3_chain.log)
  [ "$m" -gt "$n" ] && { sed -n "$((n + 1)),${m}p" $S/snburst3_chain.log | grep -E "run |done |COMPLETE|FAILED|boundary"; n=$m; }
  grep -q -E "SNBURST3 (COMPLETE|FAILED)" $S/snburst3_chain.log && exit 0
  R=$(tail -1 $S/snburst3_roots.txt)
  P=$(for d in $(ls -d $R/run/*/rate-* 2>/dev/null); do [ -e $d/wrk.stderr ] && [ ! -e $d/result.json ] && echo $d; done | tail -1)
  if [ -n "$P" ] && [ ! -e $P/result.json ] && [ -e $P/wrk.stderr ]; then
    ep=$(grep "^burst epoch" $P/wrk.stderr | tail -1 | awk '{print "epoch "$3" "$4" x"substr($5,3)" -> "substr($6,6)" req/s"}')
    case=$(basename $(dirname $P))
    echo "LIVE $(basename $R | cut -d- -f3-4) $case: $ep | $(python3 $S/live_line.py $R 2>/dev/null | cut -d'|' -f2-)"
  fi
  sleep 110
done
