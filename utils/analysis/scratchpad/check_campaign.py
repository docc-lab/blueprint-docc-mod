"""Read-only status check for the RetCtx DSB campaign. Sends no load, touches no cluster."""
import gzip, json, shutil, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path

R = Path('/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z')
print('now', datetime.now(timezone.utc).isoformat(timespec='seconds'))

for name in ('run-status.json', 'monitor-status.json'):
    print(name, json.dumps(json.loads((R / name).read_text())))
prog = json.loads((R / 'progress.json').read_text())
prog.pop('latest_point', None); prog.pop('runner', None)
print('progress', json.dumps(prog))
for name in ('run.pid', 'monitor.pid'):
    pid = int((R / name).read_text())
    p = Path(f'/proc/{pid}/cmdline')
    print(name, pid, 'ALIVE' if p.exists() else 'NOT RUNNING', p.read_text().replace('\0', ' ')[:110] if p.exists() else '')
print('run-complete.json exists:', (R / 'run-complete.json').exists())

# wrk processes (should be at most one, owned by the runner, during measuring)
try:
    out = subprocess.run(['pgrep', '-a', '-x', 'wrk'], capture_output=True, text=True).stdout.strip()
except Exception as e:
    out = f'pgrep failed: {e}'
print('wrk processes:', out.replace('\n', ' || ') if out else 'none')

# per-case summary
total = 0
print('cases:')
for case in sorted((R / 'run').iterdir()):
    if '-interrupted-' in case.name or not case.is_dir():
        continue
    rows = []
    for path in sorted(case.glob('rate-*/result.json')):
        row = json.loads(path.read_text())
        if 'kind' in row:
            rows.append(row)
    if not rows:
        print(f'  {case.name}: 0 points (deploying/seeding/warmup)')
        continue
    total += len(rows)
    last = max(rows, key=lambda r: r['offered_rps'])
    peak = max(rows, key=lambda r: r['completed_rps'])
    drain = case / 'trace-drain.json'
    drain_s = ''
    if drain.exists():
        d = json.loads(drain.read_text())
        drain_s = f" drain_empty={d.get('queues_empty')}"
    print(f"  {case.name}: {len(rows)}/16 complete={(case/'complete.json').exists()} last={last['offered_rps']} "
          f"peak_completed={peak['completed_rps']:.1f}@{peak['offered_rps']} http_err={sum(r['non_2xx_3xx'] for r in rows)} "
          f"restarts={sum(r['restarts_changed'] for r in rows)}{drain_s}")
    if rows[0]['kind'] == 'v':
        first_v = None; peak_v = 0.0; vnodes = set()
        for point in sorted(case.glob('rate-*')):
            rp = point / 'result.json'
            if not rp.exists() or 'kind' not in json.loads(rp.read_text()):
                continue
            offered = json.loads(rp.read_text())['offered_rps']
            try:
                b = json.loads((point/'before/snapshot.json').read_text())['collectors']
                a = json.loads((point/'after/snapshot.json').read_text())['collectors']
                pods = {pp['metadata']['name']: pp['spec']['nodeName'] for pp in json.loads((point/'after/pods.json').read_text())['items']}
            except Exception:
                continue
            acc = ref = 0
            for name, av in a.items():
                bv = b.get(name)
                if not bv: continue
                dr = av.get('otelcol_receiver_refused_spans_total', 0) - bv.get('otelcol_receiver_refused_spans_total', 0)
                da = av.get('otelcol_receiver_accepted_spans_total', 0) - bv.get('otelcol_receiver_accepted_spans_total', 0)
                if dr < 0 or da < 0: continue
                acc += da; ref += dr
                if dr > 0: vnodes.add(pods.get(name, name))
            if ref and first_v is None: first_v = offered
            if acc + ref: peak_v = max(peak_v, 100*ref/(acc+ref))
        print(f'      receiver span refusals: first={first_v} peak%={peak_v:.2f} nodes={sorted(vnodes)}')
    # HP refusal summary for bridge cases from raw priority logs
    if rows[0]['kind'] != 'v':
        first_hp = None; peak_pct = 0.0; nodes = set()
        for point in sorted(case.glob('rate-*')):
            rp = point / 'result.json'
            if not rp.exists() or 'kind' not in json.loads(rp.read_text()):
                continue
            offered = json.loads(rp.read_text())['offered_rps']
            pods_path = point / 'after/pods.json'
            if not pods_path.exists():
                continue
            pods = json.loads(pods_path.read_text())['items']
            adm = ref = 0
            for pod in pods:
                nm = pod['metadata']['name']
                if not nm.startswith('otelcol-'):
                    continue
                samples = []
                ok = True
                for side in ('before', 'after'):
                    f = point / side / f'logs-{nm}.txt.gz'
                    if not f.exists():
                        ok = False; break
                    with gzip.open(f, 'rt') as s:
                        lines = s.read().splitlines()
                    line = next((l for l in reversed(lines) if 'priority_processor_metrics' in l), None)
                    if line is None:
                        ok = False; break
                    samples.append(json.loads(line[line.index('{'):]))
                if not ok:
                    continue
                dr = samples[1]['hp_refused'] - samples[0]['hp_refused']
                da = samples[1]['hp_admitted'] - samples[0]['hp_admitted']
                if dr < 0 or da < 0:
                    continue
                adm += da; ref += dr
                if dr:
                    nodes.add(pod['spec']['nodeName'])
            if ref and first_hp is None:
                first_hp = offered
            if adm + ref:
                peak_pct = max(peak_pct, 100 * ref / (adm + ref))
        print(f'      HP refusals: first={first_hp} peak%={peak_pct:.2f} nodes={sorted(nodes)}')
print('measured points', total, '/ 320')
du = shutil.disk_usage('/'); ds = shutil.disk_usage('/storage')
print(f'disk free: root {du.free/2**30:.1f} GiB, /storage {ds.free/2**30:.1f} GiB')
