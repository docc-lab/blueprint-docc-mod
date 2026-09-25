#!/usr/bin/env python3
"""Tomislav-RetCtx: live status of the sustained-acceptance probe (fixed 3500/4000/4500, 600 s each)."""
import datetime, glob, gzip, json, os, re
import sys
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
TAG=sys.argv[1] if len(sys.argv)>1 else 'sustain'
MiB=2**20
def sdk_delta(B,A):
    t={}
    for pod,s in A.get('sdk',{}).items():
        a=s.get('_processor_metrics',{}); b=B.get('sdk',{}).get(pod,{}).get('_processor_metrics',{})
        for k,v in a.items(): t[k]=t.get(k,0)+v-b.get(k,0)
    return t
def qfull(pt):
    try:
        st=json.load(open(f'{pt}/command.json'))['started'][:19].replace('T',' '); fi=json.load(open(f'{pt}/result.json'))['finished'][:19].replace('T',' ')
    except OSError: return None, None
    q=0; first=None; t0=datetime.datetime.fromisoformat(json.load(open(f'{pt}/command.json'))['started'])
    for f in glob.glob(f'{pt}/after/logs-otelcol-*.txt.gz'):
        for line in gzip.open(f,'rt'):
            if 'queue is full' not in line: continue
            ts=line[:19].replace('T',' ')
            if st<=ts<=fi:
                q+=1
                dt=(datetime.datetime.fromisoformat(line[:23]).replace(tzinfo=datetime.timezone.utc)-t0).total_seconds()
                first=dt if first is None else min(first,dt)
    return q, first
def point(pt):
    try:
        B=json.load(open(f'{pt}/before/snapshot.json')); A=json.load(open(f'{pt}/after/snapshot.json')); d=json.load(open(f'{pt}/result.json'))
    except OSError: return None
    dt=(datetime.datetime.fromisoformat(A['started'])-datetime.datetime.fromisoformat(B['finished'])).total_seconds()
    coll=ws=0.0
    for u,x in A['cpu'].items():
        y=B['cpu'].get(u)
        if y and x['name'].startswith('otelcol-') and dt>0:
            coll=max(coll,(x['cpu_ns']-y['cpu_ns'])/1e9/dt); ws=max(ws,x['working_set_bytes']/MiB)
    s=sdk_delta(B,A); c=d.get('collector_deltas',{})
    acc=c.get('otelcol_receiver_accepted_spans_total',0); ref=c.get('otelcol_receiver_refused_spans_total',0); sent=c.get('otelcol_exporter_sent_spans_total',0)
    secs=max(d.get('wrk_seconds',600),1)
    q,first=qfull(pt)
    # Tomislav-RetCtx: clickhouse backend -> gateway collector counters (snapshot['gateway']) and its own priority log
    gw={}
    if A.get('gateway'):
        ga={}; gb={}
        for pod,m in A['gateway'].items():
            for k,v in m.items():
                if any(w in k for w in ('accepted_spans','refused_spans','sent_spans','send_failed_spans','queue_size')): ga[k]=ga.get(k,0)+v-(0 if 'queue_size' in k else B.get('gateway',{}).get(pod,{}).get(k,0))
        gw=dict(acc=ga.get('otelcol_receiver_accepted_spans_total',0)/secs, ref=ga.get('otelcol_receiver_refused_spans_total',0), sent=ga.get('otelcol_exporter_sent_spans_total',0)/secs,
                fail=ga.get('otelcol_exporter_send_failed_spans_total',0), q=sum(v for k,v in ga.items() if 'queue_size' in k))
        gs={}
        for pod,m in A.get('sdk',{}).items():
            if pod.startswith('otelgw-') and '_processor_metrics' in m:
                b=B.get('sdk',{}).get(pod,{}).get('_processor_metrics',{})
                for k,v in m['_processor_metrics'].items(): gs[k]=gs.get(k,0)+v-b.get(k,0)
        gw['cp']=100*gs.get('hp_refused',0)/max(1,gs.get('hp_admitted',0)+gs.get('hp_refused',0)) if gs else None
        gw['lp']=100*gs.get('lp_refused',0)/max(1,gs.get('lp_admitted',0)+gs.get('lp_refused',0)) if gs else None
        for u,x in A['cpu'].items():
            y=B['cpu'].get(u)
            if y and x['name'].startswith('otelgw-') and dt>0: gw['cpu']=(x['cpu_ns']-y['cpu_ns'])/1e9/dt; gw['ws']=x['working_set_bytes']/MiB
            if y and x['name'].startswith('clickhouse-') and dt>0: gw['ch_cpu']=(x['cpu_ns']-y['cpu_ns'])/1e9/dt
    return dict(gw=gw,offered=d.get('offered_rps',0), realised=d.get('sent_requests',0)/secs, completed=d.get('completed_rps',0), p99=d.get('p99_ms',0),
                coll=coll, ws=ws, cp=100*s.get('cp_dropped',0)/max(s.get('cp_received',1),1), lp=100*s.get('lp_dropped',0)/max(s.get('lp_received',1),1),
                refused=100*ref/max(acc+ref,1), refused_n=ref, acc_s=acc/secs, sent_s=sent/secs, backlog=acc-sent, qfull=q, qfirst=first)
roots=[l.strip() for l in open(f'{S}/{TAG}_roots.txt')] if os.path.exists(f'{S}/{TAG}_roots.txt') else []
log=open(f'{S}/{TAG}.log').read() if os.path.exists(f'{S}/{TAG}.log') else ''
print(f"=== sustained-acceptance probe [{TAG}]  {datetime.datetime.now(datetime.timezone.utc):%H:%M:%S} UTC ===")
for l in log.splitlines():
    if 'FAILED' in l: print('!!! '+l)
if 'COMPLETE' in log: print('*** SUSTAIN COMPLETE ***')
for R in roots:
    print(f"\n--- {os.path.basename(R)} (reverse ON) ---")
    print(f"  {'case':5} {'offered':>7} {'realised':>8} {'compl':>6} {'p99ms':>7} {'coll':>5} {'WS':>4} {'acc/s':>8} {'exp/s':>8} {'backlog':>9} {'cp_drop%':>8} {'lp_drop%':>8} {'refused':>9} {'jaeger qfull (first s)':>22}")
    for case in sorted(glob.glob(f'{R}/run/01-*')):
        kind=case.rsplit('-',1)[1]
        pts=sorted(p for p in glob.glob(f'{case}/rate-*') if os.path.isdir(p))
        if not pts: print(f"  {kind:5} (pending)"); continue
        for pt in pts:
            p=point(pt)
            if not p: print(f"  {kind:5} {os.path.basename(pt)} (running/pending)"); continue
            qf=f"{p['qfull']} ({p['qfirst']:.0f})" if p['qfull'] else '0'
            print(f"  {kind:5} {p['offered']:7.0f} {p['realised']:8.0f} {p['completed']:6.0f} {p['p99']:7.1f} {p['coll']:5.2f} {p['ws']:4.0f} {p['acc_s']:8.0f} {p['sent_s']:8.0f} {p['backlog']:9,.0f} {p['cp']:7.2f}% {p['lp']:7.1f}% {p['refused']:6.2f}% {p['refused_n']:>8,.0f} {qf:>22}")
            g=p.get('gw')
            if g:
                cp=f"{g['cp']:.2f}%" if g.get('cp') is not None else '-'; lp=f"{g['lp']:.1f}%" if g.get('lp') is not None else '-'
                print(f"        gateway: in {g['acc']:,.0f}/s  refused {g['ref']:,.0f} (cp {cp} lp {lp})  -> clickhouse {g['sent']:,.0f}/s  send_failed {g['fail']:,.0f}  queue {g['q']:,.0f}  gw cpu {g.get('cpu',0):.2f} ws {g.get('ws',0):.0f} MiB  clickhouse cpu {g.get('ch_cpu',0):.2f}")
    for f,tagname in ((f'{R}/run-status.json','RUN'),(f'{R}/smoke-status.json','SMOKE')):
        try:
            st=json.load(open(f))
            if st.get('state')!='complete': print(f"  runner: {tagname} case {st.get('case')} stage {st.get('stage')} offered {st.get('offered_rps','-')} state {st.get('state')}"); break
        except OSError: pass
