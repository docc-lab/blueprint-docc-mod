#!/usr/bin/env python3
"""Tomislav-RetCtx: live status of the 60/40 threshold round, from the per-point
snapshots. Collector CPU is the before/after cpu_ns delta over the window;
cp/lp drops are SDK-side counters; refused% is the collector receiver counter.
"""
import datetime, glob, json, os, sys

S = '/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
MiB = 2**20
BASE = {('pb','on'):3500, ('cgpb','on'):3500, ('sb','on'):2500,
        ('pb','off'):6500, ('cgpb','off'):6000, ('sb','off'):4500, ('v','on'):6500, ('v','off'):6500}
CKPT_BASE = {('pb','on'):9000, ('cgpb','on'):None, ('sb','on'):None,
             ('pb','off'):7500, ('cgpb','off'):7500, ('sb','off'):7500}


def sdk_delta(B, A):
    t = {}
    for pod, s in A.get('sdk', {}).items():
        a = s.get('_processor_metrics', {}); b = B.get('sdk', {}).get(pod, {}).get('_processor_metrics', {})
        for k, v in a.items():
            t[k] = t.get(k, 0) + v - b.get(k, 0)
    return t


def point_row(pt):
    try:
        B = json.load(open(f'{pt}/before/snapshot.json')); A = json.load(open(f'{pt}/after/snapshot.json'))
        d = json.load(open(f'{pt}/result.json'))
    except OSError:
        return None
    dt = (datetime.datetime.fromisoformat(A['started']) - datetime.datetime.fromisoformat(B['finished'])).total_seconds()
    if dt <= 0:
        return None
    coll = 0.0; ws = 0.0
    for u, x in A['cpu'].items():
        y = B['cpu'].get(u)
        if y and x['name'].startswith('otelcol-'):
            coll = max(coll, (x['cpu_ns'] - y['cpu_ns']) / 1e9 / dt)
            ws = max(ws, x['working_set_bytes'] / MiB)
    s = sdk_delta(B, A)
    cp = 100 * s.get('cp_dropped', 0) / max(s.get('cp_received', 1), 1)
    lp = 100 * s.get('lp_dropped', 0) / max(s.get('lp_received', 1), 1)
    tot = 100 * s.get('spans_dropped', 0) / max(s.get('spans_received', 1), 1)
    c = d.get('collector_deltas', {})
    acc = c.get('otelcol_receiver_accepted_spans_total', 0); ref = c.get('otelcol_receiver_refused_spans_total', 0)
    return dict(offered=d['offered_rps'], completed=d.get('completed_rps', 0), coll=coll, ws=ws,
                cp=cp, lp=lp, tot=tot, refused=100 * ref / max(acc + ref, 1), ckpt_lost=s.get('cp_dropped', 0) > 0)


def main():
    roots = [l.strip() for l in open(f'{S}/t6040_roots.txt')] if os.path.exists(f'{S}/t6040_roots.txt') else []
    now = datetime.datetime.now(datetime.timezone.utc).strftime('%H:%M:%S')
    print(f'=== 60/40 round  {now} UTC ===')
    log = open(f'{S}/t6040.log').read() if os.path.exists(f'{S}/t6040.log') else ''
    for l in log.splitlines():
        if 'FAILED' in l:
            print('!!! ' + l)
    if 'T6040 COMPLETE' in log:
        print('*** T6040 COMPLETE ***')
    for R in roots:
        arm = 'ON' if '-on-' in os.path.basename(R) else 'OFF'
        print(f'\n--- arm {arm}: {os.path.basename(R)} ---')
        total = 0; current = None
        for case in sorted(glob.glob(f'{R}/run/01-*')):
            kind = case.rsplit('-', 1)[1]
            pts = sorted(glob.glob(f'{case}/rate-*/result.json'))
            n = len(pts); total += n
            if 0 < n < 28:
                current = case
            rows = [point_row(os.path.dirname(p)) for p in pts]
            rows = [r for r in rows if r]
            onset = next((r['offered'] for r in rows if r['tot'] > 0), None)
            ck = next((r['offered'] for r in rows if r['ckpt_lost']), None)
            base = BASE.get((kind, arm.lower()))
            cb = CKPT_BASE.get((kind, arm.lower()))
            extra = ''
            if kind != 'nt' and n:
                extra = f"  span-loss onset {onset}" + (f" (1-core {base})" if base else '')
                if kind != 'v':
                    extra += f"  ckpt-loss onset {ck}" + (f" (1-core {cb})" if cb is not None else ' (1-core none)')
            print(f'  {kind:5} {n:2}/28{extra}')
        print(f'  points: {total}/{28 * len(glob.glob(f"{R}/run/01-*"))}')
        show = current or (sorted(glob.glob(f'{R}/run/01-*'))[-1] if glob.glob(f'{R}/run/01-*') else None)
        if show:
            kind = show.rsplit('-', 1)[1]
            rows = [point_row(os.path.dirname(p)) for p in sorted(glob.glob(f'{show}/rate-*/result.json'))]
            rows = [r for r in rows if r][-4:]
            if rows:
                print(f"  [{arm} {kind}] {'offered':>7} {'compl':>6} {'coll/1.0':>8} {'WS MiB':>6} {'cp_drop%':>8} {'lp_drop%':>8} {'refused%':>8}")
                for r in rows:
                    print(f"  {'':11} {r['offered']:7} {r['completed']:6.0f} {r['coll']:8.2f} {r['ws']:6.0f} {r['cp']:7.2f}% {r['lp']:7.1f}% {r['refused']:7.1f}%")
    try:
        st = json.load(open(f'{roots[-1]}/run-status.json'))
        print(f"\n  runner: case {st.get('case')} stage {st.get('stage')} state {st.get('state')}")
    except Exception:
        try:
            st = json.load(open(f'{roots[-1]}/smoke-status.json'))
            print(f"\n  runner: SMOKE case {st.get('case')} stage {st.get('stage')} state {st.get('state')}")
        except Exception:
            pass


main()
