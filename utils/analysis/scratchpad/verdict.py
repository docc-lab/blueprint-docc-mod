import json, datetime
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/'
SINK=open(S+'sink_root').read().strip()+'/run/01-pb'
ADM=open(S+'nwe2e_campaign_root').read().strip()+'/run/01-pb'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/01-pb'
def pt(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    cp=app=0.
    for uid,v in a['cpu'].items():
        if uid not in b['cpu']: continue
        c=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
        if v['name'].startswith('composepost'): cp=c
        if '-service-' in v['name']: app+=c
    sdk={}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m or n.startswith('otelcol-'): continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics')
        if pb is None: continue
        for k,v in m['_processor_metrics'].items():
            if isinstance(v,(int,float)): sdk[k]=sdk.get(k,0)+v-pb.get(k,0)
    return r, cp, app, sdk, dt
print('PB, offered 8000 and 11000 -- three collector regimes')
print('regime       rate | completed | comp cores | app cores | CPU/req ms | service ms | spans/req | spans sent/s | dropped/s')
for rate in (8000, 11000):
    for label, base in (('sink       ',SINK),('admission  ',ADM),('passthrough',PT)):
        try: r,cp,app,sdk,dt = pt(base, rate)
        except Exception as e: print(f'{label} {rate}: not measured yet'); continue
        comp=r['completed_rps']
        print(f"{label} {rate:5} | {comp:9.0f} | {cp:10.2f} | {app:9.1f} | {1000*app/comp:10.3f} | {1000*r['connections']/comp:10.1f} | {sdk.get('spans_received',0)/dt/comp:9.2f} | {sdk.get('spans_sent',0)/dt:12.0f} | {sdk.get('spans_dropped',0)/dt:9.0f}")
    print()
