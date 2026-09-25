import json, datetime, sys
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
def sdk(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    app={}; coll={}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics')
        if pb is None: continue
        tgt = coll if n.startswith('otelcol-') else app
        for k,v in m['_processor_metrics'].items():
            if isinstance(v,(int,float)): tgt[k]=tgt.get(k,0)+v-pb.get(k,0)
    return app, coll, dt
print('SDK + collector counters at offered 6000 (rep 1)')
for kind in ('v','pb','sb'):
    for label, base in (('admission  ',ADM),('passthrough',PT)):
        try: app, coll, dt = sdk(f'{base}/01-{kind}/rate-06000')
        except Exception as e: print(kind,label,'n/a',e); continue
        keys=('spans_received','spans_sent','spans_dropped','send_unavailable','send_deadline',
              'send_exhausted','send_other','hp_buffer_depth','lp_buffer_depth','batches_sent','batches_dropped')
        print(f"{kind:5} {label} app: " + ' '.join(f'{k}={app.get(k,0)/ (dt if k.endswith(("sent","received","dropped","unavailable","deadline","exhausted","other")) else 1):.0f}' for k in keys))
        if coll:
            print(f"{'':5} {'':11} coll: " + ' '.join(f'{k}={coll.get(k,0)/dt:.1f}/s' for k in ('hp_admitted','hp_refused','lp_admitted','lp_refused','gc_count')))
