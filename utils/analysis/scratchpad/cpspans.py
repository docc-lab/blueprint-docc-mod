import json, datetime
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/'
SINK=open(S+'sink_root').read().strip()+'/run/01-pb'
ADM=open(S+'nwe2e_campaign_root').read().strip()+'/run/01-pb'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/01-pb'
def cpost(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    cpu=0.
    for uid,v in a['cpu'].items():
        if uid in b['cpu'] and v['name'].startswith('composepost'):
            cpu=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
    m={}
    for n,mm in a['sdk'].items():
        if not n.startswith('composepost') or '_processor_metrics' not in mm: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics',{})
        for k,v in mm['_processor_metrics'].items():
            if isinstance(v,(int,float)): m[k]=v-pb.get(k,0)
    c=r['completed_rps']
    return c, cpu, m.get('spans_received',0)/dt, m.get('spans_sent',0)/dt, dt
print('PB, composepost only')
print('rate | regime      | completed | cpu cores | ms/req | spans created/s | /req | spans SENT/s | sent/req')
for rate in (8000,10000,12000):
    for label, base in (('sink       ',SINK),('admission  ',ADM),('passthrough',PT)):
        try: c,cpu,recv,sent,dt = cpost(base, rate)
        except Exception: continue
        print(f'{rate:5}| {label} | {c:9.0f} | {cpu:9.2f} | {1000*cpu/c:6.3f} | {recv:15.0f} | {recv/c:4.2f} | {sent:12.0f} | {sent/c:8.2f}')
    print()
