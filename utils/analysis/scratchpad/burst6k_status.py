#!/usr/bin/env python3
"""Tomislav-RetCtx: live status of the stationary bursty run (mean 6000, Pareto 1.5/4, 300 s)."""
import datetime, glob, json, os, re, statistics
S = '/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
MiB = 2**20

def sdk_delta(B, A):
    t = {}
    for pod, s in A.get('sdk', {}).items():
        a = s.get('_processor_metrics', {}); b = B.get('sdk', {}).get(pod, {}).get('_processor_metrics', {})
        for k, v in a.items():
            t[k] = t.get(k, 0) + v - b.get(k, 0)
    return t

def point(pt):
    try:
        B = json.load(open(f'{pt}/before/snapshot.json')); A = json.load(open(f'{pt}/after/snapshot.json'))
        d = json.load(open(f'{pt}/result.json'))
    except OSError:
        return None
    dt = (datetime.datetime.fromisoformat(A['started']) - datetime.datetime.fromisoformat(B['finished'])).total_seconds()
    coll = ws = 0.0
    for u, x in A['cpu'].items():
        y = B['cpu'].get(u)
        if y and x['name'].startswith('otelcol-') and dt > 0:
            coll = max(coll, (x['cpu_ns'] - y['cpu_ns']) / 1e9 / dt); ws = max(ws, x['working_set_bytes'] / MiB)
    s = sdk_delta(B, A)
    c = d.get('collector_deltas', {})
    acc = c.get('otelcol_receiver_accepted_spans_total', 0); ref = c.get('otelcol_receiver_refused_spans_total', 0)
    rates = []
    try:
        for l in open(f'{pt}/wrk.stderr'):
            m = re.search(r'burst epoch \d+ t=[\d.]+s g=[\d.]+ rate=(\d+)', l)
            if m: rates.append(int(m.group(1)))
    except OSError:
        pass
    return dict(realised=d.get('sent_requests', 0) / max(d.get('wrk_seconds', 300), 1), completed=d.get('completed_rps', 0),
                coll=coll, ws=ws, cp=100 * s.get('cp_dropped', 0) / max(s.get('cp_received', 1), 1),
                lp=100 * s.get('lp_dropped', 0) / max(s.get('lp_received', 1), 1),
                refused=100 * ref / max(acc + ref, 1), epochs=len(rates),
                emin=min(rates) if rates else 0, emed=statistics.median(rates) if rates else 0, emax=max(rates) if rates else 0,
                p99=d.get('p99_ms', 0))

roots = [l.strip() for l in open(f'{S}/burst6k_roots.txt')] if os.path.exists(f'{S}/burst6k_roots.txt') else []
log = open(f'{S}/burst6k.log').read() if os.path.exists(f'{S}/burst6k.log') else ''
print(f"=== bursty 6k run  {datetime.datetime.now(datetime.timezone.utc):%H:%M:%S} UTC ===")
for l in log.splitlines():
    if 'FAILED' in l: print('!!! ' + l)
if 'BURST6K COMPLETE' in log: print('*** BURST6K COMPLETE ***')
for R in roots:
    arm = 'ON' if '-on-' in os.path.basename(R) else 'OFF'
    print(f"\n--- arm {arm}: {os.path.basename(R)} ---")
    print(f"  {'case':5} {'realised':>8} {'compl':>6} {'p99ms':>7} {'coll/1.0':>8} {'WS':>5} {'cp_drop%':>8} {'lp_drop%':>8} {'refused%':>8} {'epoch rate min/med/max':>22}")
    for case in sorted(glob.glob(f'{R}/run/01-*')):
        kind = case.rsplit('-', 1)[1]
        p = point(f'{case}/rate-06000')
        if not p:
            print(f"  {kind:5} (pending)"); continue
        print(f"  {kind:5} {p['realised']:8.0f} {p['completed']:6.0f} {p['p99']:7.1f} {p['coll']:8.2f} {p['ws']:5.0f} {p['cp']:7.2f}% {p['lp']:7.1f}% {p['refused']:7.1f}% {p['emin']:>6}/{p['emed']:>5.0f}/{p['emax']:<6}({p['epochs']})")
    for f, tag in ((f'{R}/run-status.json', 'RUN'), (f'{R}/smoke-status.json', 'SMOKE')):
        try:
            st = json.load(open(f))
            if st.get('state') != 'complete':
                print(f"  runner: {tag} case {st.get('case')} stage {st.get('stage')} state {st.get('state')}"); break
        except OSError:
            pass
