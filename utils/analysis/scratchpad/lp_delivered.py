"""Tomislav-RetCtx: LP actually written by the gateway during the point: gateway exporter sent spans minus checkpoint
spans the SDKs delivered (cp_sent), as % of LP offered (SDK lp_sent + lp_dropped), same before->after window for all
variants. Excludes backlog the gateway wrote after the point ended."""
import glob, gzip, json, re, sys
D = '/users/tomislav/deployments/dsb-hotel'
def gw(P, which):
    f = glob.glob(f'{P}/{which}/gateway-*.txt.gz')[0]; tot = 0
    for l in gzip.open(f, 'rt'):
        if l.startswith('otelcol_exporter_sent_spans_total'): tot += float(l.split()[-1])
    return tot
for root in sys.argv[2:]:
    out = []
    for r in range(10000, 22001, 2000):
        P = f'{D}/{root}/run/01-{sys.argv[1]}/rate-{r}'
        try:
            B = json.load(open(f'{P}/before/snapshot.json')); A = json.load(open(f'{P}/after/snapshot.json'))
        except Exception:
            out.append('   -'); continue
        s = {}
        for pod, m in A['sdk'].items():
            if '-service-' not in pod: continue
            a = m.get('_processor_metrics', {}); b = B['sdk'].get(pod, {}).get('_processor_metrics', {})
            for k in ('cp_sent', 'lp_sent', 'lp_dropped'): s[k] = s.get(k, 0) + a.get(k, 0) - b.get(k, 0)
        lp_out = gw(P, 'after') - gw(P, 'before') - s['cp_sent']
        out.append(f"{100 * lp_out / max(1, s['lp_sent'] + s['lp_dropped']):4.0f}")
    print(f"{root.split('cubic-')[1][:9]:10s} LP written in-window %: " + ' '.join(out))
