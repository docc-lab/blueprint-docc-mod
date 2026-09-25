"""Tomislav-RetCtx: SN no-work points so far for one kind vs the n=5 matrix ON-round mean. usage: snnw_live.py root kind [from_rate]"""
import glob, json, statistics, sys, os
R, kind = sys.argv[1], sys.argv[2]; lo = int(sys.argv[3]) if len(sys.argv) > 3 else 0
M = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
for f in sorted(glob.glob(f'{R}/run/*-{kind}/rate-*/result.json')):
    rate = int(f.split('rate-')[1][:5])
    if rate < lo: continue
    d = json.load(open(f))
    mp = [json.load(open(g)) for g in (f'{m}/run/01-{kind}/rate-{rate:05d}/result.json' for m in M) if os.path.exists(g)]
    print(f"{kind} {rate:>5}: got {d['completed_rps']:6,.0f} (matrix {statistics.mean(p['completed_rps'] for p in mp):6,.0f})  mean {d['mean_ms']:7.1f} ms (matrix {statistics.mean(p['mean_ms'] for p in mp):7.1f})  p99 {d['p99_ms']:7.1f} (matrix {statistics.mean(p['p99_ms'] for p in mp):7.1f})")
