"""Read-only status for the rebuilt-SB campaign (no-work root + real-work root)."""
import json, shutil, subprocess
from datetime import datetime, timezone
from pathlib import Path
S = Path('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad')
NW = Path((S/'sb2_nw_root').read_text().strip()); E2E = Path((S/'sb2_e2e_root').read_text().strip())
print('now', datetime.now(timezone.utc).isoformat(timespec='seconds'))
print('orchestrator:', (NW/'orchestrator-status.json').read_text().strip(), '| pid alive:', Path(f"/proc/{(NW/'orchestrator.pid').read_text().strip()}").exists())
w = subprocess.run(['pgrep', '-a', '-x', 'wrk'], capture_output=True, text=True).stdout.strip(); print('wrk:', w.split('\n')[0][:90] if w else 'none')
for label, R in (('NW-SB2', NW), ('E2E-SB2', E2E)):
    print(f'== {label}: {R}')
    for name in ('prepare-status.json', 'image-build-status.json', 'smoke-status.json', 'run-status.json', 'monitor-status.json'):
        p = R/name
        if p.exists():
            d = json.loads(p.read_text()); d.pop('previous', None); print(f'  {name}: {json.dumps(d)[:220]}')
    for name in ('run.pid', 'monitor.pid'):
        p = R/name
        if p.exists(): pid = p.read_text().strip(); print(f'  {name} {pid}', 'ALIVE' if Path(f'/proc/{pid}').exists() else 'exited')
    plan = json.loads((R/'plan.json').read_text())
    total_pts = len(plan.get('cases', plan.get('variants', []))) * plan.get('repetitions', 1) * len(plan['ramp_rates'])
    total = 0; latest = None
    if (R/'run').exists():
        for case in sorted(p for p in (R/'run').iterdir() if p.is_dir() and '-interrupted-' not in p.name):
            rows = [json.loads(p.read_text()) for p in sorted(case.glob('rate-*/result.json'))]; rows = [r for r in rows if 'kind' in r]
            if not rows: print(f'  {case.name}: 0 points (deploy/seed/warmup)'); continue
            total += len(rows); last = max(rows, key=lambda r: r['offered_rps']); peak = max(rows, key=lambda r: r['completed_rps'])
            if latest is None or last['finished'] > latest['finished']: latest = last
            ref = sum(r['collector_deltas'].get('otelcol_receiver_refused_spans_total', 0) for r in rows)
            print(f"  {case.name}: {len(rows)}/{len(plan['ramp_rates'])} complete={(case/'complete.json').exists()} last={last['offered_rps']} peak_completed={peak['completed_rps']:.0f}@{peak['offered_rps']} http_err={sum(r['non_2xx_3xx'] for r in rows)} restarts={sum(r['restarts_changed'] for r in rows)} receiver_refused={ref:.0f}")
    if latest: print(f"  latest: {latest.get('case', latest['kind'])} offered {latest['offered_rps']} completed {latest['completed_rps']:.0f}/s mean {latest['mean_ms']:.1f} ms p99 {latest['p99_ms']:.1f} ms")
    print(f'  measured points {total} / {total_pts}')
d = shutil.disk_usage('/'); s = shutil.disk_usage('/storage'); print(f'disk free: root {d.free/2**30:.1f} GiB, /storage {s.free/2**30:.1f} GiB')
