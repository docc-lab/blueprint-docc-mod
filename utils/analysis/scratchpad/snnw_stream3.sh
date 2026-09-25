#!/usr/bin/env bash
# Tomislav-RetCtx: stream each new SN memory-limit point (nt, v, pb, cgpb, sb, nt tail) with matrix / default-GC /
# memory-limit no-tracing comparisons, generator cores, errors, and a flag for shortfall or non-monotonic throughput.
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad; seen=$S/snnw_stream2.seen
roots() { for x in snnw_mem_nt_root snnw_mem_opt_root snnw_mem_nttail_root; do cat $S/$x.txt 2>/dev/null; done; }
[ -f $seen ] || for R in $(roots); do find $R/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null; done > $seen
while true; do
  for R in $(roots); do
    for f in $(find $R/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null | sort); do
      grep -qx "$f" $seen && continue
      python3 -c "import json,sys; sys.exit(0 if 'collector_deltas' in json.load(open('$f')) else 1)" 2>/dev/null || continue
      echo $f >> $seen
      python3 - $f $S <<'PY'
import sys, json, glob, os, statistics
f, S = sys.argv[1], sys.argv[2]; d = json.load(open(f))
kind = f.split('/run/')[1].strip('/').split('/')[0].split('-', 1)[1]; rate = int(f.split('rate-')[1][:5])
def res(p):
    return json.load(open(p)) if os.path.exists(p) else None
got, off = d['completed_rps'], d['offered_rps']
err = d.get('non_2xx_3xx') or 0
se = d.get('socket_errors'); err += sum(se.values()) if isinstance(se, dict) else (se or 0)
gen = d['generator_cpu_seconds'] / d['wall_seconds']
line = f"MEMLIMIT {kind} {rate}: got {got:,.0f} ({got/off:.1%}) mean {d['mean_ms']:.1f} ms p99 {d['p99_ms']:.1f} | gen {gen:.1f} cores, err {err}"
M = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
mp = [r for r in (res(f'{m}/run/01-{kind}/rate-{rate:05d}/result.json') for m in M) if r]
if mp: line += f" | matrix got {statistics.mean(p['completed_rps'] for p in mp):,.0f} mean {statistics.mean(p['mean_ms'] for p in mp):.1f}"
if kind != 'nt':
    for x in ('snnw_mem_nt_root', 'snnw_mem_nttail_root'):
        try: nt = res(open(f'{S}/{x}.txt').read().strip() + f'/run/01-nt/rate-{rate:05d}/result.json')
        except OSError: nt = None
        if nt: line += f" | nt-mem got {nt['completed_rps']:,.0f} mean {nt['mean_ms']:.1f}"
if kind in ('pb', 'cgpb', 'sb'):
    try: vv = res(open(f'{S}/snnw_mem_opt_root.txt').read().strip() + f'/run/01-v/rate-{rate:05d}/result.json')
    except OSError: vv = None
    if vv and 'collector_deltas' in vv: line += f" | v-mem got {vv['completed_rps']:,.0f} mean {vv['mean_ms']:.1f}"
if got < 0.97 * off: line += "  << SHORTFALL"
print(line, flush=True)
PY
    done
  done
  grep -q "SNNW MEM CHAIN COMPLETE" $S/snnw_mem_chain.log && { echo "ALL SN RUNS COMPLETE"; exit 0; }
  kill -0 $(cat $S/snnw_mem_chain.pid 2>/dev/null || echo 999999) 2>/dev/null || { echo "MEM CHAIN GONE: $(tail -2 $S/snnw_mem_chain.log | cut -c1-200)"; exit 1; }
  for R in $(roots); do [ -f $R/run-status.json ] && grep -q '"failed"' $R/run-status.json && { echo "RUN FAILED: $R $(head -c 300 $R/run-status.json)"; exit 1; }; done
  sleep 10
done
