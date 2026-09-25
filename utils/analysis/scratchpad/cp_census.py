#!/usr/bin/env python3
"""Tomislav-RetCtx: per-collector HP/LP refusals, refuse-all seconds and heap peaks over a point (bridges: priority log)."""
import gzip, glob, json, re, sys
from datetime import datetime, timezone
MiB=2**20
for P in sys.argv[1:]:
    cmd=json.load(open(f'{P}/command.json')); res=json.load(open(f'{P}/result.json'))
    t0=datetime.fromisoformat(cmd['started']).timestamp(); t1=datetime.fromisoformat(res['finished']).timestamp()
    pods=json.load(open(f'{P}/after/pods.json'))['items']; node_of={it['metadata']['name']:it['spec']['nodeName'] for it in pods}
    def podname(f): return re.sub(r'^logs-','',f.rsplit('/',1)[1]).replace('.txt.gz','')
    print(f"== {P.split('/')[-3]} {P.split('/')[-1]}  ({P.split('/')[3]})")
    T=dict(hr=0,ha=0)
    for f in sorted(glob.glob(f'{P}/after/logs-otel*.txt.gz'), key=lambda f: node_of.get(podname(f),'')):
        name=podname(f); rows=[]
        for line in gzip.open(f,'rt'):
            if 'priority_processor_metrics' not in line: continue
            t=datetime.strptime(line[:23],'%Y-%m-%dT%H:%M:%S.%f').replace(tzinfo=timezone.utc).timestamp(); rows.append((t,json.loads(line.split('priority_processor_metrics\t',1)[1])))
        rows.sort(key=lambda r:r[0]); inwin=[(t,j) for t,j in rows if t0<=t<=t1]
        if not inwin: continue
        prev=next((j for t,j in reversed(rows) if t<t0), inwin[0][1]); hr=ha=lr=la=0; soft_s=shed_s=0; peak=0; first_hp=None
        for t,j in inwin:
            dh=j['hp_refused']-prev['hp_refused']; hr+=dh; ha+=j['hp_admitted']-prev['hp_admitted']; lr+=j['lp_refused']-prev['lp_refused']; la+=j['lp_admitted']-prev['lp_admitted']; prev=j
            if dh>0 and first_hp is None: first_hp=t-t0
            if j['state'] in ('soft','hard'): soft_s+=1
            if j['state']=='shedlp': shed_s+=1
            peak=max(peak,j['alloc_bytes']/MiB)
        kind='gateway' if name.startswith('otelgw') else 'agent'
        if kind=='agent': T['hr']+=hr; T['ha']+=ha
        flag=' <--' if hr>0 else ''
        print(f"  {kind:7s} {node_of.get(name,'?'):7s} hp_ref {hr:>7,} of {hr+ha:>10,} ({100*hr/max(1,hr+ha):5.2f}%)  lp_ref {lr:>10,} of {lr+la:>10,} ({100*lr/max(1,lr+la):4.1f}%)  shedlp {shed_s:3d}s refuse-all {soft_s:3d}s  heap peak {peak:5.0f} MiB  first hp loss {('%.0f s'%first_hp) if first_hp is not None else '-'}{flag}")
    print(f"  AGENTS hp_refused {T['hr']:,} of {T['ha']+T['hr']:,} = {100*T['hr']/max(1,T['ha']+T['hr']):.3f}%")
