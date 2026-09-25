import json, gzip, sys
d=sys.argv[1]
def sample(p):
    lines=gzip.open(p,'rt').read().splitlines()
    l=next(x for x in reversed(lines) if 'priority_processor_metrics' in x)
    return json.loads(l[l.index('{'):])
pods=json.load(open(d+'/after/pods.json'))['items']
best=None
for pod in pods:
    n=pod['metadata']['name']
    if not n.startswith('otelcol-'): continue
    o=sample(d+'/before/logs-'+n+'.txt.gz'); w=sample(d+'/after/logs-'+n+'.txt.gz')
    x={k: w[k]-o[k] for k in ('hp_admitted','hp_refused','lp_admitted','lp_refused')}
    tot=sum(x.values()); ref=x['hp_refused']+x['lp_refused']
    rate=100*ref/tot if tot else 0
    if best is None or rate>best[1]: best=(pod['spec']['nodeName'],rate,x,tot)
node,rate,x,tot=best
hp=x['hp_admitted']+x['hp_refused']; lp=x['lp_admitted']+x['lp_refused']
print('worst collector = %s' % node)
print('  checkpoints : %9d seen, %9d refused -> %5.1f%% dropped' % (hp, x['hp_refused'], 100*x['hp_refused']/hp))
print('  ordinary    : %9d seen, %9d refused -> %5.1f%% dropped' % (lp, x['lp_refused'], 100*x['lp_refused']/lp))
print('  all spans   : %9d seen, %9d refused -> %5.1f%% dropped' % (tot, x['hp_refused']+x['lp_refused'], rate))
print('  ordinary share of this collector traffic: %.1f%%' % (100*lp/tot))
