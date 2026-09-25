import json, datetime
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/'
SINK=open(S+'sink_root').read().strip()+'/run/01-pb'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/01-pb'
def cpost(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    m={}
    for n,mm in a['sdk'].items():
        if not n.startswith('composepost') or '_processor_metrics' not in mm: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics',{})
        for k,v in mm['_processor_metrics'].items():
            if isinstance(v,(int,float)): m[k]=v-pb.get(k,0)
    return r['completed_rps'], m, dt
print('composepost @ offered 8000 -- full span accounting per second')
for label, base in (('sink       ',SINK),('passthrough',PT)):
    c,m,dt = cpost(base, 8000)
    recv=m.get('spans_received',0)/dt; flu=m.get('spans_flushed',0)/dt
    sent=m.get('spans_sent',0)/dt; drop=m.get('spans_dropped',0)/dt
    print(f'{label}: received={recv:8.0f} flushed={flu:8.0f} sent={sent:8.0f} dropped={drop:8.0f} | attempted(sent+drop)={sent+drop:8.0f} | received-attempted={recv-sent-drop:8.0f}')
    print(f'{"":13} batches sent={m.get("batches_sent",0)/dt:6.1f}/s dropped={m.get("batches_dropped",0)/dt:6.1f}/s | deadline={m.get("send_deadline",0)/dt:6.1f}/s unavail={m.get("send_unavailable",0)/dt:6.1f}/s')
    print(f'{"":13} buffer depth hp={m.get("hp_buffer_depth",0):7.0f} lp={m.get("lp_buffer_depth",0):7.0f}')
