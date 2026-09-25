import json, glob, gzip
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
def depths(base, rates):
    out=[]
    for rate in rates:
        for f in glob.glob(f'{base}/rate-{rate:05d}/after/logs-composepost*.txt.gz'):
            for line in gzip.open(f,'rt'):
                i=line.find('{')
                if i<0 or 'hp_buffer_depth' not in line: continue
                try: m=json.loads(line[i:])
                except ValueError: continue
                out.append((m.get('hp_buffer_depth',0), m.get('lp_buffer_depth',0)))
    return out
rates=(8000,9000,10000,11000,12000,13000,14000)
for kind in ('pb','sb'):
    for label, base in (('admission  ',ADM),('passthrough',PT)):
        v=depths(f'{base}/01-{kind}', rates)
        if not v: print(kind,label,'no samples'); continue
        hp=sorted(x for x,_ in v); lp=sorted(y for _,y in v)
        print(f'{kind:4} {label}: n={len(v):4} | hp max {hp[-1]:6} p90 {hp[int(.9*len(hp))]:6} | lp max {lp[-1]:6} p90 {lp[int(.9*len(lp))]:6}')
