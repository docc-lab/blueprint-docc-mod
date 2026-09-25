import json, glob, gzip, re
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
# raw (non-delta) buffer depths reported by composepost in the after-snapshot log window
def depths(base, rate):
    d=f'{base}/rate-{rate:05d}/after'
    out=[]
    for f in glob.glob(d+'/logs-composepost*.txt.gz'):
        for line in gzip.open(f,'rt'):
            if '_processor_metrics' in line:
                try: m=json.loads(line[line.index('{'):])
                except ValueError: continue
                if 'hp_buffer_depth' in m: out.append((m.get('hp_buffer_depth',0), m.get('lp_buffer_depth',0)))
    return out
for kind in ('pb','sb'):
    for label, base in (('admission',ADM),('passthrough',PT)):
        allv=[]
        for rate in (8000,9000,10000,11000,12000,13000,14000):
            allv += depths(f'{base}/01-{kind}', rate)
        if not allv: print(kind,label,'no samples'); continue
        hp=[a for a,b in allv]; lp=[b for a,b in allv]
        print(f'{kind:4} {label:12}: samples={len(allv)} hp max={max(hp):6} lp max={max(lp):6} | lp p90={sorted(lp)[int(.9*len(lp))]:6}')
