# Tomislav-RetCtx: per point, gateway work: spans received (accepted) and exported per second, CPU
# seconds per 1k exported spans, from the before/after gateway scrapes. usage: gw_work.py <case dir>
import glob, gzip, re, sys, json
def scrape(f):
    out = {}
    for l in gzip.open(f, 'rt'):
        if l.startswith('#'): continue
        m = re.match(r'(\w+)(\{[^}]*\})?\s+([-0-9.eE+]+)', l)
        if m: out[m.group(1)] = out.get(m.group(1), 0) + float(m.group(3))
    return out
for P in sorted(glob.glob(sys.argv[1] + '/rate-*')):
    try:
        b = scrape(glob.glob(P + '/before/gateway-*.txt.gz')[0]); a = scrape(glob.glob(P + '/after/gateway-*.txt.gz')[0])
    except IndexError:
        continue
    d = {k: a.get(k, 0) - b.get(k, 0) for k in a}
    r = json.load(open(P + '/result.json')); secs = r.get('duration_s') or r.get('seconds') or 30
    acc = d.get('otelcol_receiver_accepted_spans_total', 0); sent = d.get('otelcol_exporter_sent_spans_total', 0)
    cpu = d.get('otelcol_process_cpu_seconds_total', 0)
    print(f"{P[-5:]}: gw accepted {acc/secs:8,.0f}/s  exported {sent/secs:8,.0f}/s  cpu {cpu/secs:.2f} cores  {1000*cpu/max(1,sent):.3f} cpu-s per 1k exported  (window {secs}s)")
