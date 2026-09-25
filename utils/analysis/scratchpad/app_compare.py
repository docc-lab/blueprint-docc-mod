"""Tomislav-RetCtx: per rate, completed rps / mean / p99 / frontend CPU for several run case dirs.
usage: app_compare.py label=case_dir ..."""
import sys, json, datetime, os
runs = [a.split('=', 1) for a in sys.argv[1:]]
for r in range(10000, 22001, 2000):
    for label, R in runs:
        P = f'{R}/rate-{r}'
        if not os.path.exists(f'{P}/after/snapshot.json'):
            continue
        d = json.load(open(f'{P}/result.json'))
        B = json.load(open(f'{P}/before/snapshot.json')); A = json.load(open(f'{P}/after/snapshot.json'))
        dt = (datetime.datetime.fromisoformat(A['started']) - datetime.datetime.fromisoformat(B['finished'])).total_seconds()
        cpu = {x['name'].split('-service')[0]: (x['cpu_ns'] - B['cpu'][u]['cpu_ns']) / 1e9 / dt for u, x in A['cpu'].items() if u in B['cpu'] and '-service-' in x['name']}
        print(f"{label:12s} {r//1000:>2}k: got {d['completed_rps']:7,.0f} rps  mean {d['mean_ms']:7.1f} ms  p99 {d['p99_ms']:7.1f} ms | frontend {cpu.get('frontend',0):.2f} search {cpu.get('search',0):.2f} cores")
