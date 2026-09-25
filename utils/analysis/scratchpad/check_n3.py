"""Read-only status check for the no-work campaign."""
import json, shutil, subprocess
from datetime import datetime, timezone
from pathlib import Path
R = Path(open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/n3_root').read().strip())
print('now', datetime.now(timezone.utc).isoformat(timespec='seconds'), 'root', R)
for name in ('run-status.json', 'monitor-status.json'):
    p = R / name
    print(name, json.dumps(json.loads(p.read_text())) if p.exists() else 'absent')
for name in ('run.pid', 'monitor.pid'):
    p = R / name
    if p.exists():
        pid = int(p.read_text()); print(name, pid, 'ALIVE' if Path(f'/proc/{pid}').exists() else 'NOT RUNNING')
print('run-complete.json:', (R / 'run-complete.json').exists())
w = subprocess.run(['pgrep', '-a', '-x', 'wrk'], capture_output=True, text=True).stdout.strip()
print('wrk:', w.replace('\n', ' || ') if w else 'none')
plan = json.loads((R / 'plan.json').read_text()); reps = int(plan.get('repetitions', 1)); total_pts = len(plan['cases']) * len(plan['ramp_rates']) * reps
total = 0; latest = None
for case in sorted(p for p in (R / 'run').iterdir() if p.is_dir() and '-interrupted-' not in p.name):
    rows = [json.loads(p.read_text()) for p in sorted(case.glob('rate-*/result.json'))]
    rows = [r for r in rows if 'kind' in r]
    if not rows:
        print(f'  {case.name}: 0 points (deploy/warmup)'); continue
    total += len(rows)
    last = max(rows, key=lambda r: r['offered_rps']); peak = max(rows, key=lambda r: r['completed_rps'])
    if latest is None or last['finished'] > latest['finished']: latest = last
    ref = sum(r['collector_deltas'].get('otelcol_receiver_refused_spans_total', 0) for r in rows)
    print(f"  {case.name}: {len(rows)}/{len(plan['ramp_rates'])} complete={(case/'complete.json').exists()} last={last['offered_rps']} "
          f"peak_completed={peak['completed_rps']:.0f}@{peak['offered_rps']} http_err={sum(r['non_2xx_3xx'] for r in rows)} "
          f"sock_err={sum(sum(r['socket_errors'].values()) for r in rows)} restarts={sum(r['restarts_changed'] for r in rows)} collector_refused={ref:.0f}")
if latest:
    print(f"latest point: {latest['case']} offered {latest['offered_rps']} completed {latest['completed_rps']:.0f}/s mean {latest['mean_ms']:.1f} ms p99 {latest['p99_ms']:.1f} ms conns {latest['connections']} gen_cores {latest['generator_cpu_seconds']/latest['wall_seconds']:.2f}")
print(f'measured points {total} / {total_pts}; ramps complete {sum((c/"complete.json").exists() for c in (R/"run").iterdir() if c.is_dir() and "-interrupted-" not in c.name)} / {len(plan["cases"]) * reps}')
d = shutil.disk_usage('/'); s = shutil.disk_usage('/storage'); print(f'disk free: root {d.free/2**30:.1f} GiB, /storage {s.free/2**30:.1f} GiB')
