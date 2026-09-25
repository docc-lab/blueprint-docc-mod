import json, glob, os, datetime
root=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_val2_root').read().strip()
a256=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_val_root').read().strip()+'/run/01-sb'
pt=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/esfix2_root').read().strip()+'/run/01-sb'
st=json.load(open(root+'/run-status.json')); print('status', st.get('state'), st.get('stage'), st.get('offered_rps'), st['updated'][11:19])
for f in ('run.pid','monitor.pid'):
    try:
        pid=int(open(root+'/'+f).read()); print(f, pid, 'ALIVE' if os.path.exists(f'/proc/{pid}') else 'NOT RUNNING')
    except Exception as e: print(f, e)
def info(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    o={'hp_refused':0,'lp_refused':0,'hp_admitted':0,'sdk_drop':0,'cp_drop':0,'queue':0,'cp':0.,'app':0.,'collmax':0.}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics',{}); pm=m['_processor_metrics']
        if n.startswith('otelcol-'):
            for k in ('hp_refused','lp_refused','hp_admitted'): o[k]+=pm.get(k,0)-pb.get(k,0)
        else:
            o['sdk_drop']+=pm.get('spans_dropped',0)-pb.get('spans_dropped',0)
            o['cp_drop']+=pm.get('cp_dropped',0)-pb.get('cp_dropped',0)
    for uid,v in a['cpu'].items():
        if uid not in b['cpu']: continue
        c=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
        if v['name'].startswith('composepost'): o['cp']=c
        if '-service-' in v['name']: o['app']+=c
        if v['name'].startswith('otelcol-'): o['collmax']=max(o['collmax'],c)
    for name,ma in a['collectors'].items():
        for k,v in ma.items():
            if k.startswith('otelcol_exporter_queue_size'): o['queue']+=v
    o['dt']=dt; return o
rows=[json.load(open(p)) for p in glob.glob(root+'/run/01-sb/rate-*/result.json')]
rows=sorted([r for r in rows if 'kind' in r and 'restarts_changed' in r], key=lambda r:r['offered_rps'])
print(f"points {len(rows)}/28; http_err {sum(r['non_2xx_3xx'] for r in rows)} sock_err {sum(sum(r['socket_errors'].values()) for r in rows)} restarts {sum(r['restarts_changed'] for r in rows)}")
print('offered | refused/s | hp_ref | lp_ref/s | sdk_drop/s (cp) | q | composepost | app cores | completed | mean/p99 | 256Mi compl | passthru compl')
for r in rows[-10:]:
    rate=r['offered_rps']; d=root+f'/run/01-sb/rate-{rate:05d}'; o=info(d); s=r['wall_seconds']
    ref=r['collector_deltas'].get('otelcol_receiver_refused_spans_total',0)
    def comp(base):
        try: return json.load(open(f'{base}/rate-{rate:05d}/result.json'))['completed_rps']
        except Exception: return float('nan')
    print(f"{rate:6} | {ref/s:9.0f} | {o['hp_refused']:6.0f} | {o['lp_refused']/o['dt']:8.0f} | {o['sdk_drop']/o['dt']:9.0f} ({o['cp_drop']:.0f}) | {o['queue']:3.0f} | {o['cp']:5.2f} | {o['app']:6.1f} | {r['completed_rps']:7.0f} | {r['mean_ms']:7.1f}/{r['p99_ms']:7.1f} | {comp(a256):7.0f} | {comp(pt):7.0f}")
