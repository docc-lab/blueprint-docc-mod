import json, datetime
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
def pt(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    rt={}; sdk={}
    for n,m in a['sdk'].items():
        for grp,key in (('rt','BRIDGES_RT'),('sdk','_processor_metrics')):
            if key not in m or n.startswith('otelcol-'): continue
            pb=b['sdk'].get(n,{}).get(key,{})
            tgt = rt if grp=='rt' else sdk
            for k,v in m[key].items():
                if isinstance(v,(int,float)): tgt[k]=tgt.get(k,0)+v-pb.get(k,0)
    return r, rt, sdk, dt
print('CONNECTION LIMIT CHECK: is completed == connections / service_time?')
print('kind rate | regime      | completed | conns | implied service time us | spans/req | RT leaf_rejects/s | RT received/s | RT checkpoints/s')
for kind in ('pb','sb'):
    for rate in (8000,11000):
        for label, base in (('admission  ',ADM),('passthrough',PT)):
            try: r, rt, sdk, dt = pt(f'{base}/01-{kind}', rate)
            except Exception as e: continue
            comp=r['completed_rps']; conns=r['connections']
            spr = sdk.get('spans_received',0)/dt/comp if comp else 0
            print(f"{kind:4} {rate:5} | {label} | {comp:9.0f} | {conns:5} | {1e6*conns/comp:23.0f} | {spr:9.2f} | {rt.get('leaf_rejects',0)/dt:17.0f} | {rt.get('received',0)/dt:13.0f} | {rt.get('checkpoints',0)/dt:16.0f}")
    print()
