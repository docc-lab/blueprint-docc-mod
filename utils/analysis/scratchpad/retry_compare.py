"""Tomislav-RetCtx: SDK-retry probe vs pdnocpu vs vanilla (fixes500m), per rate: broken traces (census),
checkpoints dropped (SDK), LP lost (SDK + agents' exporter drops; vanilla: all spans lost)."""
import glob, json, os
D = '/users/tomislav/deployments/dsb-hotel'; A = D + '/ANALYSIS-2026-09-23'
ROOTS = {'v': ('fixes500m', 'retctx-hotel-cubic-fixes500m-20260923T1801Z', '01-v'),
         'pb0': ('pdnocpu', 'retctx-hotel-cubic-pdnocpu-20260923T1910Z', '01-pb'),
         'pb1': ('sdkretry', 'retctx-hotel-cubic-sdkretry-20260923T1952Z', '01-pb'),
         'sb0': ('pdnocpu', 'retctx-hotel-cubic-pdnocpu-20260923T1910Z', '01-sb'),
         'sb1': ('sdkretry', 'retctx-hotel-cubic-sdkretry-20260923T1952Z', '01-sb')}
def census(tag, root, case, rate):
    f = f'{A}/{tag}/census_outcome.json'
    if not os.path.exists(f): return None
    for v in json.load(open(f)).values():
        if v['point'].rstrip('/') == f'{D}/{root}/run/{case}/rate-{rate}': return v['pct']['broken']
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
    if case == '01-v':
        return None, 100 * (s['spans_dropped'] + sf) / max(1, s['spans_sent'] + s['spans_dropped'])
    return (100 * s['cp_dropped'] / max(1, s['cp_sent'] + s['cp_dropped']),
            100 * (s['lp_dropped'] + sf) / max(1, s['lp_sent'] + s['lp_dropped']))
f = lambda x, n=1: '   -' if x is None else f'{x:5.{n}f}'
print('rate  | BROKEN TRACES %            v    PB pd  PB rt   SB pd  SB rt | CHECKPOINTS DROPPED %  PB pd  PB rt   SB pd  SB rt | LP LOST % (v: spans)  v    PB pd  PB rt   SB pd  SB rt')
for r in range(10000, 22001, 2000):
    br = {k: census(*ROOTS[k], r) for k in ROOTS}; ls = {k: loss(*ROOTS[k][1:], r) for k in ROOTS}
    cp = {k: (ls[k][0] if ls[k] else None) for k in ROOTS}; lp = {k: (ls[k][1] if ls[k] else None) for k in ROOTS}
    print(f"{r//1000:>3}k  |                        {f(br['v'])}  {f(br['pb0'])}  {f(br['pb1'])}   {f(br['sb0'])}  {f(br['sb1'])} |"
          f"                        {f(cp['pb0'])}  {f(cp['pb1'])}   {f(cp['sb0'])}  {f(cp['sb1'])} |"
          f"                      {f(lp['v'])}  {f(lp['pb0'])}  {f(lp['pb1'])}   {f(lp['sb0'])}  {f(lp['sb1'])}")
