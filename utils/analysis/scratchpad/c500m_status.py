#!/usr/bin/env python3
"""Tomislav-RetCtx: live view of the 500m collector round, straight from the
per-point snapshots. Collector CPU is the before/after cpu_ns delta over the
elapsed window, so it is comparable to the analyzer's max_pod_cpu_cores.
"""
import datetime, glob, json, os, sys

R = open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/c500m_roots.txt').read().split()[-1]


def snap(path):
    try:
        return json.load(open(path))
    except OSError:
        return None


def cores(point):
    before, after = snap(f'{point}/before/snapshot.json'), snap(f'{point}/after/snapshot.json')
    if not before or not after:
        return None, None
    t0 = datetime.datetime.fromisoformat(before['finished'])
    t1 = datetime.datetime.fromisoformat(after['started'])
    dt = (t1 - t0).total_seconds()
    if dt <= 0:
        return None, None
    best, ws = 0.0, 0.0
    for uid, a in after['cpu'].items():
        if not a['name'].startswith('otelcol-'):
            continue
        b = before['cpu'].get(uid)
        if not b:
            continue
        best = max(best, (a['cpu_ns'] - b['cpu_ns']) / 1e9 / dt)
        ws = max(ws, a['working_set_bytes'] / 2**20)
    return best, ws


print(f"{'case':8} {'offered':>8} {'completed':>10} {'collCPU':>8} {'collMiB':>8} "
      f"{'accepted':>11} {'refused':>10} {'refused%':>9}")
rows = 0
for point in sorted(glob.glob(f'{R}/run/*/rate-*')):
    try:
        d = json.load(open(f'{point}/result.json'))
    except OSError:
        continue
    cd = d.get('collector_deltas', {})
    acc = cd.get('otelcol_receiver_accepted_spans_total', 0)
    ref = cd.get('otelcol_receiver_refused_spans_total', 0)
    cpu, ws = cores(point)
    print(f"{d.get('case', '?'):8} {d['offered_rps']:8} {d.get('completed_rps', 0):10.0f} "
          f"{(cpu if cpu is not None else float('nan')):8.2f} {(ws or 0):8.0f} "
          f"{acc:11.0f} {ref:10.0f} {100 * ref / max(acc + ref, 1):8.1f}%")
    rows += 1
print(f'\n{rows}/224 points  ({datetime.datetime.now(datetime.timezone.utc):%H:%M:%S} UTC)')
