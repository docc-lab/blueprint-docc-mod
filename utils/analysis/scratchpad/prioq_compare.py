"""Tomislav-RetCtx: vanilla (fixes500m) vs pre-decode (pdnocpu) vs + SDK retry (sdkretry) vs + agent strict-priority
queue (prioq), per rate: broken traces (census), checkpoints dropped (SDK), LP lost = SDK LP dropped + agents'
exporter send_failed (gateway permanent LP refusals) + LP evicted from the agents' queue stage (prioq only; from
the last priority_queue_metrics line of each agent's log, after minus before)."""
import glob, gzip, json, os, re, sys
D = '/users/tomislav/deployments/dsb-hotel'; A = D + '/ANALYSIS-2026-09-23'
RUNS = [('v', 'fixes500m', 'retctx-hotel-cubic-fixes500m-20260923T1801Z', '01-v')]
for kind in ('pb', 'sb'):
    RUNS += [(f'{kind}:pd', 'pdnocpu', 'retctx-hotel-cubic-pdnocpu-20260923T1910Z', f'01-{kind}'),
             (f'{kind}:rt', 'sdkretry', 'retctx-hotel-cubic-sdkretry-20260923T1952Z', f'01-{kind}'),
             (f'{kind}:pq', 'prioq', open(os.path.dirname(__file__) + '/prioq_root.txt').read().strip().split('/')[-1], f'01-{kind}')]
def census(tag, root, case, rate):
    f = f'{A}/{tag}/census_outcome.json'
    if not os.path.exists(f): return None
    for v in json.load(open(f)).values():
        if v['point'].rstrip('/') == f'{D}/{root}/run/{case}/rate-{rate}': return v['pct']['broken']
def queue_counter(d, key):
    tot = 0
    for f in glob.glob(f'{d}/logs-otelcol-*.txt.gz'):
        last = None
        for l in gzip.open(f, 'rt'):
            if 'priority_queue_metrics' in l: last = l
        if last:
            m = re.search(rf'"{key}": ?(\d+)', last); tot += int(m.group(1)) if m else 0
    return tot
def loss(root, case, rate):
    P = f'{D}/{root}/run/{case}/rate-{rate}'
    try: B = json.load(open(f'{P}/before/snapshot.json')); Aa = json.load(open(f'{P}/after/snapshot.json'))
    except Exception: return None
    s = {}
    for pod, m in Aa['sdk'].items():
        if '-service-' not in pod: continue
        a = m.get('_processor_metrics', {}); b = B['sdk'].get(pod, {}).get('_processor_metrics', {})
        for k in ('cp_dropped', 'lp_dropped', 'cp_sent', 'lp_sent', 'spans_dropped', 'spans_sent'):
            s[k] = s.get(k, 0) + a.get(k, 0) - b.get(k, 0)
    sf = sum(m.get('otelcol_exporter_send_failed_spans_total', 0) for m in Aa['collectors'].values()) - \
         sum(m.get('otelcol_exporter_send_failed_spans_total', 0) for m in B['collectors'].values())
    ev = queue_counter(f'{P}/after', 'lp_evicted_spans') - queue_counter(f'{P}/before', 'lp_evicted_spans')
    if case == '01-v':
        return None, 100 * (s['spans_dropped'] + sf) / max(1, s['spans_sent'] + s['spans_dropped']), 0
    lpt = max(1, s['lp_sent'] + s['lp_dropped'])
    return 100 * s['cp_dropped'] / max(1, s['cp_sent'] + s['cp_dropped']), 100 * (s['lp_dropped'] + sf + ev) / lpt, 100 * ev / lpt
f = lambda x: '    -' if x is None else f'{x:5.1f}'
cols = [r[0] for r in RUNS]
for title, idx in (('BROKEN TRACES %', 'b'), ('CHECKPOINTS DROPPED %', 0), ('LP LOST % (v: all spans)', 1), ('  of which evicted in agent queue %', 2)):
    print(f'{title:36s} ' + ' '.join(f'{c:>6s}' for c in cols))
    for r in range(10000, 22001, 2000):
        vals = []
        for key, tag, root, case in RUNS:
            if idx == 'b': vals.append(census(tag, root, case, r))
            else:
                l = loss(root, case, r); vals.append(None if l is None else l[idx])
        print(f'{r//1000:>5}k' + ' ' * 31 + ' '.join(f'{f(v):>6s}' for v in vals))
