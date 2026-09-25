"""Tomislav-RetCtx: broken traces (census) and LP lost per rate for each variant, side by side.
LP lost = SDK LP refused + agents' exporter send_failed (gateway permanent LP refusals) + LP evicted from any
queue stage (agents and gateway; last priority_queue_metrics line per collector, after minus before).
Vanilla: all spans lost (no LP/HP split). usage: broken_lp_table.py pb|sb"""
import glob, gzip, json, os, re, sys
D = '/users/tomislav/deployments/dsb-hotel'; A = D + '/ANALYSIS-2026-09-23'; S = os.path.dirname(os.path.abspath(__file__))
k = sys.argv[1]
RUNS = [('vanilla', 'fixes500m', 'retctx-hotel-cubic-fixes500m-20260923T1801Z', '01-v'),
        *([('vanilla no gzip', 'nocomp', open(S + '/nocomp_v_root.txt').read().strip().split('/')[-1], '01-v')] if os.path.exists(S + '/nocomp_v_root.txt') else []),
        ('CPU thr.', 'predecode', 'retctx-hotel-cubic-predecode-20260923T1836Z', f'01-{k}'),
        ('pre-dec.', 'pdnocpu', 'retctx-hotel-cubic-pdnocpu-20260923T1910Z', f'01-{k}'),
        ('+retry', 'sdkretry', 'retctx-hotel-cubic-sdkretry-20260923T1952Z', f'01-{k}'),
        ('+rules', 'prioq3', open(S + '/prioq3_root.txt').read().strip().split('/')[-1], f'01-{k}')]
if k == 'sb' and os.path.exists(S + '/prioq4sb_root.txt'):
    RUNS.append(('+mark', 'prioq4', open(S + '/prioq4sb_root.txt').read().strip().split('/')[-1], '01-sb'))
if k == 'sb' and os.path.exists(S + '/nocomp_sb_root.txt'):
    RUNS.append(('+no gzip', 'nocomp', open(S + '/nocomp_sb_root.txt').read().strip().split('/')[-1], '01-sb'))
if k == 'pb' and os.path.exists(S + '/nocomp_pb_root.txt'):
    RUNS.append(('+no gzip', 'nocomp', open(S + '/nocomp_pb_root.txt').read().strip().split('/')[-1], '01-pb'))
elif k == 'pb' and os.path.exists(S + '/prioq4_root.txt'):
    RUNS.append(('+mark', 'prioq4', open(S + '/prioq4_root.txt').read().strip().split('/')[-1], f'01-{k}'))
def census(tag, root, case, rate):
    f = f'{A}/{tag}/census_outcome.json'
    if not os.path.exists(f): return None
    for v in json.load(open(f)).values():
        if v['point'].rstrip('/') == f'{D}/{root}/run/{case}/rate-{rate}': return v['pct']['broken']
def evicted(d):
    tot = 0
    for f in glob.glob(f'{d}/logs-otel*.txt.gz'):
        last = None
        for l in gzip.open(f, 'rt'):
            if 'priority_queue_metrics' in l: last = l
        if last:
            m = re.search(r'"lp_evicted_spans": ?(\d+)', last); tot += int(m.group(1)) if m else 0
    return tot
def lp_lost(root, case, rate):
    P = f'{D}/{root}/run/{case}/rate-{rate}'
    try: B = json.load(open(f'{P}/before/snapshot.json')); Aa = json.load(open(f'{P}/after/snapshot.json'))
    except Exception: return None
    s = {}
    for pod, m in Aa['sdk'].items():
        if '-service-' not in pod: continue
        a = m.get('_processor_metrics', {}); b = B['sdk'].get(pod, {}).get('_processor_metrics', {})
        for key in ('lp_dropped', 'lp_sent', 'spans_dropped', 'spans_sent'):
            s[key] = s.get(key, 0) + a.get(key, 0) - b.get(key, 0)
    sf = sum(m.get('otelcol_exporter_send_failed_spans_total', 0) for m in Aa['collectors'].values()) - \
         sum(m.get('otelcol_exporter_send_failed_spans_total', 0) for m in B['collectors'].values())
    if case == '01-v':
        return 100 * (s['spans_dropped'] + sf) / max(1, s['spans_sent'] + s['spans_dropped'])
    ev = evicted(f'{P}/after') - evicted(f'{P}/before')
    return 100 * (s['lp_dropped'] + sf + ev) / max(1, s['lp_sent'] + s['lp_dropped'])
f = lambda x: '–' if x is None else (f'{x:.0f}' if x >= 10 else f'{x:.1f}')
print('| rate | ' + ' | '.join(f'{n} broken / LP lost' for n, *_ in RUNS) + ' |')
print('|---|' + '---|' * len(RUNS))
for r in range(10000, 22001, 2000):
    cells = []
    for n, tag, root, case in RUNS:
        b, l = census(tag, root, case, r), lp_lost(root, case, r)
        cells.append(f'{f(b)} / {f(l)}' + (' (all spans)' if case == '01-v' and r == 10000 else ''))
    print(f'| {r//1000}k | ' + ' | '.join(cells) + ' |')
