import glob, gzip, re, statistics
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
def samples(base, rates):
    out=[]
    for rate in rates:
        for f in glob.glob(f'{base}/rate-{rate:05d}/after/logs-composepost*.txt.gz'):
            for line in gzip.open(f,'rt'):
                if '_processor_metrics' not in line: continue
                d=dict(re.findall(r'(\w+)=(-?\d+)', line))
                if 'hp_buffer_depth' in d: out.append({k:int(v) for k,v in d.items()})
    return out
rates=(8000,9000,10000,11000,12000,13000,14000)
print('composepost SDK, samples over offered 8000-14000')
for kind in ('v','pb','sb'):
    for label, base in (('admission  ',ADM),('passthrough',PT)):
        v=samples(f'{base}/01-{kind}', rates)
        if not v: print(f'{kind:4} {label}: no samples'); continue
        hp=sorted(x['hp_buffer_depth'] for x in v); lp=sorted(x['lp_buffer_depth'] for x in v)
        last=v[-1]
        cpr,cpd = last.get('cp_received',0), last.get('cp_dropped',0)
        lpr,lpd = last.get('lp_received',0), last.get('lp_dropped',0)
        print(f'{kind:4} {label}: n={len(v):3} hp_buf max {hp[-1]:6} p50 {hp[len(hp)//2]:6} | lp_buf max {lp[-1]:6} p50 {lp[len(lp)//2]:6}'
              f' | lifetime cp_dropped {100*cpd/cpr if cpr else 0:5.2f}%  lp_dropped {100*lpd/lpr if lpr else 0:5.1f}%')
