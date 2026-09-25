import json, glob, os, datetime
from pathlib import Path
R=Path(open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip())
print('now', datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds'), 'root', R)
for n in ('smoke-status.json','run-status.json','monitor-status.json'):
    p=R/n
    if p.exists(): print(n, json.dumps(json.load(open(p)))[:220])
for n in ('run.pid','monitor.pid'):
    p=R/n
    if p.exists():
        pid=int(p.read_text()); print(n, pid, 'ALIVE' if os.path.exists(f'/proc/{pid}') else 'NOT RUNNING')
print('run-complete:', (R/'run-complete.json').exists())
plan=json.load(open(R/'plan.json')); total=len(plan['cases'])*len(plan['ramp_rates'])*plan['repetitions']
tot=0
for case in sorted(p for p in (R/'run').iterdir() if p.is_dir() and '-interrupted-' not in p.name) if (R/'run').exists() else []:
    rows=[json.load(open(p)) for p in sorted(case.glob('rate-*/result.json'))]
    rows=[r for r in rows if 'kind' in r and 'restarts_changed' in r]
    if not rows: print(f'  {case.name}: 0 points (deploy/warmup)'); continue
    tot+=len(rows)
    last=max(rows,key=lambda r:r['offered_rps']); peak=max(rows,key=lambda r:r['completed_rps'])
    ref=sum(r['collector_deltas'].get('otelcol_receiver_refused_spans_total',0) for r in rows)
    print(f"  {case.name}: {len(rows)}/28 complete={(case/'complete.json').exists()} last={last['offered_rps']} peak={peak['completed_rps']:.0f}@{peak['offered_rps']} http_err={sum(r['non_2xx_3xx'] for r in rows)} sock={sum(sum(r['socket_errors'].values()) for r in rows)} restarts={sum(r['restarts_changed'] for r in rows)} refused={ref:.0f}")
print(f'measured points {tot}/{total}')
import shutil
print('disk free: root %.1f GiB, /storage %.1f GiB'%(shutil.disk_usage('/').free/2**30, shutil.disk_usage('/storage').free/2**30))
