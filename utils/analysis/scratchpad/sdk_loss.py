#!/usr/bin/env python3
"""Tomislav-RetCtx: per point: SDK checkpoint/LP drops, agents' exporter drops (gateway permanent
LP refusals), pre-decode refusals, gateway + node-1 CPU and heap vs shed line. usage: sdk_loss.py <case dir>"""
import json, glob, sys, datetime, gzip
M=2**20
for Pt in sorted(glob.glob(sys.argv[1]+'/rate-*')):
    try: B=json.load(open(f'{Pt}/before/snapshot.json')); A=json.load(open(f'{Pt}/after/snapshot.json')); json.load(open(f'{Pt}/result.json'))['collector_deltas']
    except Exception: continue
    node={p['metadata']['name']:p['spec']['nodeName'] for p in json.load(open(f'{Pt}/after/pods.json'))['items']}
    dt=(datetime.datetime.fromisoformat(A['started'])-datetime.datetime.fromisoformat(B['finished'])).total_seconds()
    sdk={'cp_dropped':0,'lp_dropped':0,'cp_sent':0,'lp_sent':0}; per={}
    for pod,s in A['sdk'].items():
        if '-service-' not in pod: continue
        a=s.get('_processor_metrics',{}); b=B['sdk'].get(pod,{}).get('_processor_metrics',{})
        for k in sdk: sdk[k]+=a.get(k,0)-b.get(k,0)
        d=a.get('cp_dropped',0)-b.get('cp_dropped',0)
        if d: per[pod.split('-service')[0]]=int(d)
    sf=sum(m.get('otelcol_exporter_send_failed_spans_total',0) for m in A['collectors'].values())-sum(m.get('otelcol_exporter_send_failed_spans_total',0) for m in B['collectors'].values())
    cpu=lambda pred: next(((x['cpu_ns']-B['cpu'][u]['cpu_ns'])/1e9/dt for u,x in A['cpu'].items() if u in B['cpu'] and pred(x)), 0)
    gw=cpu(lambda x: x['name'].startswith('otelgw-')); n1=cpu(lambda x: x['name'].startswith('otelcol-') and x['node']=='node-1')
    cpt=sdk['cp_sent']+sdk['cp_dropped']; lpt=sdk['lp_sent']+sdk['lp_dropped']
    print(f"{Pt[-5:]}: checkpoints dropped {sdk['cp_dropped']:>7,.0f} of {cpt:>9,.0f} ({100*sdk['cp_dropped']/max(1,cpt):.2f}%) {per or ''} | LP lost {100*(sdk['lp_dropped']+sf)/max(1,lpt):5.1f}% (SDK {100*sdk['lp_dropped']/max(1,lpt):4.1f} + gw {100*sf/max(1,lpt):4.1f}) | cpu gw {gw:.2f} node-1 {n1:.2f}")
