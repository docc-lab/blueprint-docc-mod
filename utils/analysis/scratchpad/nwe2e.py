import json, glob, os, datetime
root=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_val_root').read().strip()
pt2=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/esfix2_root').read().strip()+'/run/01-sb'
st=json.load(open(root+'/run-status.json')); print('status', st.get('state'), st.get('stage'), st.get('offered_rps'), st['updated'][11:19])
for f in ('run.pid','monitor.pid'):
    pid=int(open(root+'/'+f).read()); print(f, pid, 'ALIVE' if os.path.exists(f'/proc/{pid}') else 'NOT RUNNING')
def counters(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    out={'hp_admitted':0,'lp_admitted':0,'hp_refused':0,'lp_refused':0,'sdk_dropped':0,'sdk_received':0,'cp_dropped':0,'lp_dropped':0,'queue':0}
    for n,m in a['sdk'].items():
        if '_processor_metrics' not in m: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics',{}); pm=m['_processor_metrics']
        if n.startswith('otelcol-'):
            for k in ('hp_admitted','lp_admitted','hp_refused','lp_refused'): out[k]+=pm.get(k,0)-pb.get(k,0)
        else:
            for k,src in (('sdk_dropped','spans_dropped'),('sdk_received','spans_received'),('cp_dropped','cp_dropped'),('lp_dropped','lp_dropped')):
                out[k]+=pm.get(src,0)-pb.get(src,0)
    for name,ma in a['collectors'].items():
        for k,v in ma.items():
            if k.startswith('otelcol_exporter_queue_size'): out['queue']+=v
    return out
rows=[json.load(open(p)) for p in glob.glob(root+'/run/01-sb/rate-*/result.json')]
rows=sorted([r for r in rows if 'kind' in r and 'restarts_changed' in r], key=lambda r:r['offered_rps'])
print(f"points {len(rows)}/28; http_err {sum(r['non_2xx_3xx'] for r in rows)} sock_err {sum(sum(r['socket_errors'].values()) for r in rows)} restarts {sum(r['restarts_changed'] for r in rows)}")
print('offered | coll refused/s | hp_ref/s | lp_ref/s | hp_adm/s | SDK drop/s (cp/lp) | queue | completed | mean/p99 | passthrough completed / mean')
for r in rows[-10:]:
    rate=r['offered_rps']; d=root+f'/run/01-sb/rate-{rate:05d}'; c=counters(d); s=r['wall_seconds']
    ref=r['collector_deltas'].get('otelcol_receiver_refused_spans_total',0)
    try:
        o=json.load(open(f'{pt2}/rate-{rate:05d}/result.json')); oc,om=o['completed_rps'],o['mean_ms']
    except Exception: oc=om=float('nan')
    print(f"{rate:6} | {ref/s:12.0f} | {c['hp_refused']/s:8.0f} | {c['lp_refused']/s:8.0f} | {c['hp_admitted']/s:8.0f} | {c['sdk_dropped']/s:9.0f} ({c['cp_dropped']/s:6.0f}/{c['lp_dropped']/s:7.0f}) | {c['queue']:5.0f} | {r['completed_rps']:7.0f} | {r['mean_ms']:7.1f}/{r['p99_ms']:7.1f} | {oc:7.0f} / {om:7.1f}")
