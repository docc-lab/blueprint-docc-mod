import json, glob, os, datetime, gzip
root=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/esfix2_root').read().strip()
r1='/users/tomislav/deployments/dsb-sn/retctx-nw-esfix-cpd2-6-inverse-20260916T130634Z'
n3='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z'
st=json.load(open(root+'/run-status.json')); print('status', st.get('state'), st.get('stage'), st.get('offered_rps'), st['updated'][11:19])
for f in ('run.pid','monitor.pid'):
    pid=int(open(root+'/'+f).read()); print(f, pid, 'ALIVE' if os.path.exists(f'/proc/{pid}') else 'NOT RUNNING')
def sdk(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    return sum(m['_processor_metrics'].get('spans_dropped',0)-b['sdk'].get(n,{}).get('_processor_metrics',{}).get('spans_dropped',0) for n,m in a['sdk'].items() if '_processor_metrics' in m)
def backend(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    dt=(datetime.datetime.fromisoformat(a['finished'])-datetime.datetime.fromisoformat(b['finished'])).total_seconds()
    out={}
    for uid,v in a['cpu'].items():
        if uid in b['cpu'] and v['name'].startswith(('jaeger','elastic')): out[v['name'][:3]]=(v['cpu_ns']-b['cpu'][uid]['cpu_ns'])/1e9/dt
    rej=0
    for f in glob.glob(d+'/after/backend-elasticsearch-*.txt.gz'):
        rej=list(json.loads(gzip.open(f,'rt').read())['nodes'].values())[0]['thread_pool']['write']['rejected']
    return out.get('jae',0), out.get('ela',0), rej
def ref(base, rate):
    try:
        o=json.load(open(f'{base}/rate-{rate:05d}/result.json')); d=o['collector_deltas']; s=o['wall_seconds']
        return d.get('otelcol_exporter_sent_spans_total',0)/s, d.get('otelcol_receiver_accepted_spans_total',0)/s, o['completed_rps'], o['mean_ms']
    except Exception: return (float('nan'),)*4
rows=[json.load(open(p)) for p in sorted(glob.glob(root+'/run/01-sb/rate-*/result.json'))]
print(f"points {len(rows)}/28; http_err {sum(r['non_2xx_3xx'] for r in rows)} sock_err {sum(sum(r['socket_errors'].values()) for r in rows)} restarts {sum(r['restarts_changed'] for r in rows)} refused {sum(r['collector_deltas'].get('otelcol_receiver_refused_spans_total',0) for r in rows):.0f}")
print('offered | r2 exported/accepted | r1 exported | n3 exported | r2 sdkdrop | jae/ES cores | rej | completed r2/r1/n3 | mean r2/r1/n3')
for r in rows[-10:]:
    rate=r['offered_rps']; d=r['collector_deltas']; s=r['wall_seconds']; dd=root+f'/run/01-sb/rate-{rate:05d}'
    e1,a1,c1,m1=ref(r1+'/run/01-sb',rate); e3,a3,c3,m3=ref(n3+'/run/03-sb',rate)
    j,e,rej=backend(dd)
    print(f"{rate:6} | {d.get('otelcol_exporter_sent_spans_total',0)/s:7.0f}/{d.get('otelcol_receiver_accepted_spans_total',0)/s:7.0f} | {e1:7.0f} | {e3:7.0f} | {sdk(dd):9.0f} | {j:5.2f}/{e:5.2f} | {rej:4.0f} | {r['completed_rps']:5.0f}/{c1:5.0f}/{c3:5.0f} | {r['mean_ms']:7.1f}/{m1:7.1f}/{m3:7.1f}")
