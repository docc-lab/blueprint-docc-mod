#!/usr/bin/env python3
"""Tomislav-RetCtx: 10 s-interval series of fleet accept/refuse/export rates, Jaeger save rate,
Jaeger queue length, ES write pool, and per-interval Jaeger queue-full rejections."""
import json, re, subprocess, sys, time, collections
N=int(sys.argv[1]) if len(sys.argv)>1 else 6; GAP=10
def k(*a): return subprocess.run(['kubectl',*a],capture_output=True,text=True,timeout=60).stdout
pods=json.loads(k('get','pods','-A','-o','json'))['items']
ns=next(p['metadata']['namespace'] for p in pods if p['metadata']['name'].startswith('otelcol-'))
cols=[p['metadata']['name'] for p in pods if p['metadata']['name'].startswith('otelcol-') and p['status'].get('phase')=='Running']
jaeger=next(p['metadata']['name'] for p in pods if p['metadata']['name'].startswith('jaeger-') and p['status'].get('phase')=='Running')
es=next(p['metadata']['name'] for p in pods if p['metadata']['name'].startswith('elasticsearch-') and p['status'].get('phase')=='Running')
def coll():
    m=collections.Counter()
    for name in cols:
        for line in k('get','--raw',f'/api/v1/namespaces/{ns}/pods/{name}:8888/proxy/metrics').splitlines():
            mm=re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans|exporter_sent_spans|exporter_queue_size)\w*(\{[^}]*\})?\s+([0-9.e+]+)',line)
            if mm: m[mm.group(1)]+=float(mm.group(3))
    return m
def jg():
    m=collections.Counter()
    for line in k('get','--raw',f'/api/v1/namespaces/{ns}/pods/{jaeger}:14269/proxy/metrics').splitlines():
        mm=re.match(r'(jaeger_collector_spans_saved_by_svc_total|jaeger_collector_queue_length|jaeger_collector_spans_dropped_total|jaeger_collector_spans_rejected_total|jaeger_collector_spans_received_total)(\{[^}]*\})?\s+([0-9.e+]+)',line)
        if mm: m[mm.group(1)]+=float(mm.group(3))
    return m
def esw():
    try:
        j=json.loads(k('get','--raw',f'/api/v1/namespaces/{ns}/pods/{es}:9200/proxy/_nodes/stats/thread_pool'))
        w=next(iter(j['nodes'].values()))['thread_pool']['write']; return w['active'],w['queue'],w['rejected'],w['completed']
    except Exception as e: return ('?','?','?','?')
def qfull_since(sec):
    return sum(k('logs',c,'-n',ns,f'--since={sec}s').count('queue is full') for c in cols)
print(f"{'t':>3s} {'acc/s':>7s} {'ref/s':>7s} {'sent/s':>7s} {'backlog':>10s} {'coll q':>6s} | {'jaeger saved/s':>14s} {'jq len':>6s} {'j drop':>6s} | {'es act':>6s} {'es q':>4s} {'es rej':>6s} {'es compl/s':>10s} | {'qfull/10s':>9s}")
c0=coll(); j0=jg(); e0=esw(); t0=time.time()
for i in range(N):
    time.sleep(GAP)
    c1=coll(); j1=jg(); e1=esw(); dt=time.time()-t0; t0=time.time()
    acc=(c1['receiver_accepted_spans']-c0['receiver_accepted_spans'])/dt; ref=(c1['receiver_refused_spans']-c0['receiver_refused_spans'])/dt; sent=(c1['exporter_sent_spans']-c0['exporter_sent_spans'])/dt
    saved=(j1['jaeger_collector_spans_saved_by_svc_total']-j0['jaeger_collector_spans_saved_by_svc_total'])/dt
    esc=(e1[3]-e0[3])/dt if isinstance(e1[3],(int,float)) and isinstance(e0[3],(int,float)) else '?'
    print(f"{(i+1)*GAP:3d} {acc:7.0f} {ref:7.0f} {sent:7.0f} {c1['receiver_accepted_spans']-c1['exporter_sent_spans']:10,.0f} {c1['exporter_queue_size']:6.0f} | {saved:14.0f} {j1['jaeger_collector_queue_length']:6.0f} {j1['jaeger_collector_spans_dropped_total']:6.0f} | {e1[0]:>6} {e1[1]:>4} {e1[2]:>6} {esc if esc=='?' else f'{esc:.1f}':>10} | {qfull_since(GAP):9d}")
    c0,j0,e0=c1,j1,e1
