#!/usr/bin/env python3
"""Tomislav-RetCtx: ACTUAL trace samples spread across a finished 300 s point,
pulled from the case's live Jaeger before the case is torn down. 10 sub-windows
x 10 traces, each query bounded by start/end so it cannot collapse onto the end
of the window. Writes <point>/settled-traces-spread.json.gz.
"""
import datetime, glob, gzip, json, os, subprocess, sys, time, urllib.parse, urllib.request
S = '/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
LOG = f'{S}/spread_sample.log'
def log(m): open(LOG, 'a').write(f'{datetime.datetime.utcnow():%H:%M:%S} {m}\n')
def jaeger_ip():
    out = subprocess.run(['kubectl', 'get', 'pods', '-n', 'dsb-sn', '-o', 'json'], capture_output=True, text=True).stdout
    for p in json.loads(out)['items']:
        if p['metadata']['name'].startswith('jaeger-') and p['status'].get('podIP'):
            return p['status']['podIP']
def get(url):
    with urllib.request.urlopen(url, timeout=30) as r:
        return json.loads(r.read())
def sample(point, windows=10, per=10):
    d = json.load(open(f'{point}/result.json'))
    t0 = int(datetime.datetime.fromisoformat(d['started']).timestamp() * 1e6)
    t1 = int(datetime.datetime.fromisoformat(d['finished']).timestamp() * 1e6)
    ip = jaeger_ip()
    if not ip:
        log(f'{point}: no jaeger pod'); return False
    base = f'http://{ip}:16686/api'
    services = get(base + '/services').get('data', [])
    svc = next((s for s in services if 'wrk2api' in s), None)
    if not svc:
        log(f'{point}: no wrk2api service in jaeger'); return False
    data, queries = [], []
    step = (t1 - t0) // windows
    for i in range(windows):
        q = {'service': svc, 'limit': per, 'lookback': '1h', 'start': t0 + i * step,
             'end': t1 if i == windows - 1 else t0 + (i + 1) * step}
        queries.append(q)
        part = get(base + '/traces?' + urllib.parse.urlencode(q))
        data.extend(part.get('data', []))
    with gzip.open(f'{point}/settled-traces-spread.json.gz', 'wt') as f:
        json.dump({'data': data, 'errors': None}, f)
    json.dump({'finished': datetime.datetime.utcnow().isoformat(), 'windows': windows, 'per_window': per,
               'queries': queries, 'traces': len(data)}, open(f'{point}/settled-traces-spread-query.json', 'w'), indent=1)
    log(f'{point}: {len(data)} traces across {windows} windows'); return True
def main(roots_file):
    done = set()
    while True:
        roots = [l.strip() for l in open(roots_file)] if os.path.exists(roots_file) else []
        for R in roots:
            for pt in sorted(glob.glob(f'{R}/run/01-*/rate-*')):
                if pt in done or not os.path.exists(f'{pt}/result.json'): continue
                try:
                    if sample(pt): done.add(pt)
                    else: done.add(pt)
                except Exception as e:
                    log(f'{pt}: error {e}')
                    done.add(pt)
        if any('COMPLETE' in l or 'FAILED' in l for l in open(f'{S}/burst6k.log')) and len(done) >= 3:
            log('done'); return
        time.sleep(5)
main(sys.argv[1])
