import json, datetime, glob
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
def point(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    cp=app=coll=0.
    for uid,v in a['cpu'].items():
        if uid not in b['cpu']: continue
        c=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
        if v['name'].startswith('composepost'): cp=c
        if '-service-' in v['name']: app+=c
        if v['name'].startswith('otelcol-'): coll+=c
    s={}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m or n.startswith('otelcol-'): continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics')
        if pb is None: continue
        for k,val in m['_processor_metrics'].items():
            if isinstance(val,(int,float)): s[k]=s.get(k,0)+val-pb.get(k,0)
    return r, cp, app, coll, s, dt
print('kind rate | regime      | completed | comp cores | app cores | coll cores | spans_recv/s | batches/s | buf hp/lp | drops/s')
for kind, rates in (('v',(9000,10000,11000)), ('pb',(8000,9000,11000)), ('sb',(8000,11000,13000))):
    for rate in rates:
        for label, base in (('admission  ',ADM),('passthrough',PT)):
            try: r,cp,app,coll,s,dt = point(f'{base}/01-{kind}', rate)
            except Exception: continue
            bat=(s.get('batches_sent',0)+s.get('batches_dropped',0))/dt
            print(f"{kind:4} {rate:5} | {label} | {r['completed_rps']:9.0f} | {cp:10.2f} | {app:9.1f} | {coll:10.2f} | {s.get('spans_received',0)/dt:12.0f} | {bat:9.1f} | {s.get('hp_buffer_depth',0):5.0f}/{s.get('lp_buffer_depth',0):<5.0f} | {s.get('spans_dropped',0)/dt:7.0f}")
    print()
