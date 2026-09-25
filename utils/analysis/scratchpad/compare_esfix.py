import json, glob
root=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/esfix_root').read().strip()
n3='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run/03-sb'
st=json.load(open(root+'/run-status.json')); print('status', st.get('state'), st.get('stage'), st.get('offered_rps'), st['updated'][11:19])
import os
for f in ('run.pid','monitor.pid'):
    pid=int(open(root+'/'+f).read()); print(f, pid, 'ALIVE' if os.path.exists(f'/proc/{pid}') else 'NOT RUNNING')
def sdk_drop(dirn):
    b=json.load(open(dirn+'/before/snapshot.json')); a=json.load(open(dirn+'/after/snapshot.json'))
    return sum(m['_processor_metrics'].get('spans_dropped',0)-b['sdk'].get(n,{}).get('_processor_metrics',{}).get('spans_dropped',0) for n,m in a['sdk'].items() if '_processor_metrics' in m)
rows=[json.load(open(p)) for p in sorted(glob.glob(root+'/run/01-sb/rate-*/result.json'))]
print(f'points {len(rows)}/28; http_err {sum(r["non_2xx_3xx"] for r in rows)} sock_err {sum(sum(r["socket_errors"].values()) for r in rows)} restarts {sum(r["restarts_changed"] for r in rows)} refused {sum(r["collector_deltas"].get("otelcol_receiver_refused_spans_total",0) for r in rows):.0f}')
print('offered | esfix exported/accepted | n3 exported/accepted | esfix sdk_drop | completed esfix/n3 | mean esfix/n3 | p99 esfix/n3')
for r in rows[-8:]:
    rate=r['offered_rps']; d=r['collector_deltas']; s=r['wall_seconds']
    ref=json.load(open(f"{n3}/rate-{rate:05d}/result.json")); dr=ref['collector_deltas']; sr=ref['wall_seconds']
    print(f"{rate:6} | {d.get('otelcol_exporter_sent_spans_total',0)/s:7.0f}/{d.get('otelcol_receiver_accepted_spans_total',0)/s:7.0f} | {dr.get('otelcol_exporter_sent_spans_total',0)/sr:7.0f}/{dr.get('otelcol_receiver_accepted_spans_total',0)/sr:7.0f} | {sdk_drop(root+f'/run/01-sb/rate-{rate:05d}'):8.0f} | {r['completed_rps']:5.0f}/{ref['completed_rps']:5.0f} | {r['mean_ms']:7.1f}/{ref['mean_ms']:7.1f} | {r['p99_ms']:7.1f}/{ref['p99_ms']:7.1f}")
