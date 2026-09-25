import json, datetime
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/'
SINK=open(S+'sink_root').read().strip()+'/run/01-pb'
PT='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/01-pb'
def per_service(base, rate):
    d=f'{base}/rate-{rate:05d}'
    r=json.load(open(d+'/result.json'))
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    out={}
    for uid,v in a['cpu'].items():
        if uid not in b['cpu']: continue
        c=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
        n=v['name']
        key = n.split('-service-')[0] if '-service-' in n else ('otelcol' if n.startswith('otelcol-') else None)
        if key: out[key]=out.get(key,0)+c
    return r['completed_rps'], out
rate=8000
cs, S_ = per_service(SINK, rate); cp, P_ = per_service(PT, rate)
print(f'PB @ offered {rate}:  sink completed {cs:.0f}   passthrough completed {cp:.0f}')
print('service        | sink cores | pass cores | sink ms/req | pass ms/req | delta ms/req')
tot_s=tot_p=0
for k in sorted(set(S_)|set(P_), key=lambda k: -(S_.get(k,0))):
    ms_s=1000*S_.get(k,0)/cs; ms_p=1000*P_.get(k,0)/cp
    if S_.get(k,0)<0.15 and P_.get(k,0)<0.15: continue
    tot_s+=ms_s; tot_p+=ms_p
    print(f'{k:14} | {S_.get(k,0):10.2f} | {P_.get(k,0):10.2f} | {ms_s:11.3f} | {ms_p:11.3f} | {ms_s-ms_p:+12.3f}')
print(f'{"TOTAL":14} |            |            | {tot_s:11.3f} | {tot_p:11.3f} | {tot_s-tot_p:+12.3f}')
