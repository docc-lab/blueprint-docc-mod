import json, datetime
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/'
SINK=open(S+'sink_root').read().strip()+'/run/01-pb'
ADM=open(S+'nwe2e_campaign_root').read().strip()+'/run/01-pb'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/01-pb'
def pt(base, rate):
    d=f'{base}/rate-{rate:05d}'
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    s={}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m or n.startswith('otelcol-'): continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics')
        if pb is None: continue
        for k,v in m['_processor_metrics'].items():
            if isinstance(v,(int,float)): s[k]=s.get(k,0)+v-pb.get(k,0)
    coll=json.load(open(d+'/result.json'))['collector_deltas']
    return s, dt, coll
print('PB @ offered 8000 -- why spans were lost, application SDK counters')
for label, base in (('sink       ',SINK),('admission  ',ADM),('passthrough',PT)):
    try: s, dt, coll = pt(base, 8000)
    except Exception as e: print(label,'n/a'); continue
    bat_ok=s.get('batches_sent',0); bat_bad=s.get('batches_dropped',0)
    print(f"{label}: batches ok={bat_ok:7.0f} dropped={bat_bad:7.0f} | reasons: deadline={s.get('send_deadline',0):7.0f} "
          f"unavailable={s.get('send_unavailable',0):7.0f} exhausted={s.get('send_exhausted',0):6.0f} other={s.get('send_other',0):5.0f}")
    print(f"{'':13} spans received={s.get('spans_received',0):10.0f} sent={s.get('spans_sent',0):10.0f} dropped={s.get('spans_dropped',0):10.0f}"
          f" | collector refused={coll.get('otelcol_receiver_refused_spans_total',0):10.0f}")
