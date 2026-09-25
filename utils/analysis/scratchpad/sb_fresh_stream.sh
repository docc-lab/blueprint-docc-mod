#!/usr/bin/env bash
# Tomislav-RetCtx: stream the fresh-deploy SB points next to the first ramp (fresh deploy) and the second ramp (warm).
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad; seen=$(mktemp); last=$(date +%s); prev=""
while true; do
  R=$(cat $S/sb_fresh_root.txt 2>/dev/null)
  if [ -n "$R" ]; then
    st=$(python3 -c "import json;d=json.load(open('$R/run-status.json'));print(d.get('state'),d.get('stage'),d.get('offered_rps'))" 2>/dev/null)
    [ "$st" != "$prev" ] && { case "$st" in *measuring*) ;; *) echo "STAGE $(date -u +%H:%M:%S) fresh SB: $st";; esac; prev=$st; }
    for f in $(find $R/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null | sort); do
      grep -qx "$f" $seen && continue
      python3 -c "import json,sys; sys.exit(0 if 'collector_deltas' in json.load(open('$f')) else 1)" 2>/dev/null || continue
      echo $f >> $seen; last=$(date +%s)
      python3 - $f $S <<'PY'
import sys, json, os
f, S = sys.argv[1:3]; d = json.load(open(f)); r = d['offered_rps']
B = open(f'{S}/hotelnw_br_root.txt').read().strip()
def g(p):
    try: x = json.load(open(p)); return f"{x['mean_ms']:.1f}/{x['p99_ms']:.1f}"
    except Exception: return '-'
print(f"FRESH sb {r}: got {d['completed_rps']:,.0f} mean {d['mean_ms']:.1f} p50 {d['p50_ms']:.1f} p99 {d['p99_ms']:.1f} | first ramp {g(f'{B}/run/01-sb/rate-{r:05d}/result.json')} | warm ramp2 {g(f'{B}/run/01-sb-ramp2/rate-{r:05d}/result.json')}", flush=True)
PY
    done
  fi
  [ $(( $(date +%s) - last )) -ge 150 ] && { echo "STATUS $(date -u +%H:%M:%S) fresh SB: ${prev:-waiting}"; last=$(date +%s); }
  grep -q "SB FRESH COMPLETE" $S/sb_fresh_chain.log 2>/dev/null && { echo "SB FRESH COMPLETE"; exit 0; }
  grep -q "CHAIN FAILED\|Traceback" $S/sb_fresh_chain.log 2>/dev/null && { echo "FRESH CHAIN PROBLEM: $(tail -3 $S/sb_fresh_chain.log)"; exit 1; }
  sleep 10
done
