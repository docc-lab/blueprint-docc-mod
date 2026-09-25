#!/usr/bin/env bash
# Tomislav-RetCtx: stream each new SN point (control, then memory-limit runs) with matrix / default-GC comparisons.
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad; seen=$(mktemp)
find $(cat $S/snnw_ctrl_root.txt)/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null > $seen
while true; do
  for R in $(cat $S/snnw_ctrl_root.txt 2>/dev/null) $(cat $S/snnw_mem_nt_root.txt 2>/dev/null) $(cat $S/snnw_mem_opt_root.txt 2>/dev/null) $(cat $S/snnw_mem_nttail_root.txt 2>/dev/null); do
    for f in $(find $R/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null | sort); do
      grep -qx "$f" $seen && continue
      python3 -c "import json,sys; sys.exit(0 if 'collector_deltas' in json.load(open('$f')) else 1)" 2>/dev/null || continue
      echo $f >> $seen
      python3 - $f <<'PY'
import sys, json, glob, os, statistics
f = sys.argv[1]; d = json.load(open(f))
kind = f.split('/run/')[1].strip('/').split('/')[0].split('-', 1)[1]; rate = int(f.split('rate-')[1][:5])
tag = 'CONTROL old-img default-GC' if 'ctrl-on' in f else 'MEMLIMIT'
M = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
mp = [json.load(open(g)) for g in (f'{m}/run/01-{kind}/rate-{rate:05d}/result.json' for m in M) if os.path.exists(g)]
line = f"{tag} {kind} {rate}: got {d['completed_rps']:,.0f} mean {d['mean_ms']:.1f} ms p99 {d['p99_ms']:.1f}"
if mp: line += f" | matrix got {statistics.mean(p['completed_rps'] for p in mp):,.0f} mean {statistics.mean(p['mean_ms'] for p in mp):.1f}"
alt = {'v': '/users/tomislav/deployments/dsb-sn/retctx-nwe2e-opt-on-20260924T020919Z/run/01-v',
       'nt': glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-ctrl-on-*/run/01-nt')[0]}.get(kind)
if alt and os.path.exists(f'{alt}/rate-{rate:05d}/result.json') and alt not in f:
    n = json.load(open(f'{alt}/rate-{rate:05d}/result.json')); line += f" | same code default-GC today got {n['completed_rps']:,.0f} mean {n['mean_ms']:.1f}"
print(line, flush=True)
PY
    done
  done
  grep -q "SNNW MEM CHAIN COMPLETE" $S/snnw_mem_chain.log && { echo "ALL SN RUNS COMPLETE"; exit 0; }
  kill -0 $(cat $S/snnw_mem_chain.pid 2>/dev/null || echo 999999) 2>/dev/null || { echo "MEM CHAIN GONE: $(tail -2 $S/snnw_mem_chain.log | cut -c1-200)"; exit 1; }
  sleep 10
done
