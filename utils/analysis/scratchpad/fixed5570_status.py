#!/usr/bin/env python3
"""Tomislav-RetCtx: live status of the fixed-rate 5570 control (300 s, no bursts)."""
import base64, datetime, glob, gzip, json, os
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
MiB=2**20
BURSTY={('v','on'):('refused',21.1),('pb','on'):('cp',2.49),('cgpb','on'):('cp',1.71),('sb','on'):('cp',5.16),
        ('pb','off'):('cp',3.47),('cgpb','off'):('cp',3.32),('sb','off'):('cp',2.41)}
def uv(b,i=0):
    r=s=0
    while True:
        c=b[i];i+=1;r|=(c&0x7f)<<s
        if c<0x80: return r,i
        s+=7
def tag(sp,k): return next((t for t in sp['tags'] if t['key']==k),None)
def eligible(t):
    present={s['spanID'] for s in t['spans']}; anchors=set(); rec=set()
    for s in t['spans']:
        br=tag(s,'_br')
        if br:
            raw=base64.b64decode(br['value']); _,i=uv(raw); a=raw[i:i+8]
            if any(a): anchors.add(a.hex())
        rc=tag(s,'_rc')
        if rc:
            v=rc['value']; raw=base64.urlsafe_b64decode(v+'='*(-len(v)%4)); i=1
            while i<len(raw):
                tg=raw[i];i+=1; ln,i=uv(raw,i); body=raw[i:i+ln]; i+=ln
                j=1 if tg&0x08 else 0; rec.add(body[j:j+8].hex())
    return not (anchors-present-rec)
def sdk_delta(B,A):
    t={}
    for pod,s in A.get('sdk',{}).items():
        a=s.get('_processor_metrics',{}); b=B.get('sdk',{}).get(pod,{}).get('_processor_metrics',{})
        for k,v in a.items(): t[k]=t.get(k,0)+v-b.get(k,0)
    return t
def point(pt, kind):
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
    acc=c.get('otelcol_receiver_accepted_spans_total',0); ref=c.get('otelcol_receiver_refused_spans_total',0)
    out=dict(realised=d.get('sent_requests',0)/max(d.get('wrk_seconds',300),1), completed=d.get('completed_rps',0), p99=d.get('p99_ms',0),
             coll=coll, ws=ws, cp=100*s.get('cp_dropped',0)/max(s.get('cp_received',1),1), lp=100*s.get('lp_dropped',0)/max(s.get('lp_received',1),1),
             refused=100*ref/max(acc+ref,1), elig=None, spread=None, n=0)
    try:
        data=json.load(gzip.open(f'{pt}/settled-traces.json.gz'))['data']
        t0=datetime.datetime.fromisoformat(d['started']).timestamp()
        offs=sorted(min(sp['startTime'] for sp in t['spans'])/1e6-t0 for t in data)
        if kind=='v': el=sum(1 for t in data if len(t['spans'])==23)
        else: el=sum(1 for t in data if eligible(t))
        out.update(elig=100*el/max(len(data),1), n=len(data), spread=(offs[0],offs[-1]) if offs else None)
    except Exception: pass
    return out
roots=[l.strip() for l in open(f'{S}/fixed5570_roots.txt')] if os.path.exists(f'{S}/fixed5570_roots.txt') else []
log=open(f'{S}/fixed5570.log').read() if os.path.exists(f'{S}/fixed5570.log') else ''
print(f"=== fixed 5570 control  {datetime.datetime.now(datetime.timezone.utc):%H:%M:%S} UTC ===")
for l in log.splitlines():
    if 'FAILED' in l: print('!!! '+l)
if 'FIXED5570 COMPLETE' in log: print('*** FIXED5570 COMPLETE ***')
for R in roots:
    arm='on' if '-on-' in os.path.basename(R) else 'off'
    print(f"\n--- arm {arm.upper()}: {os.path.basename(R)} ---")
    print(f"  {'case':5} {'realised':>8} {'compl':>6} {'p99ms':>7} {'coll':>5} {'WS':>4} {'cp_drop%':>8} {'lp_drop%':>8} {'refused%':>8} | {'bursty':>12} | {'eligible':>9} {'trace offsets':>15}")
    for case in sorted(glob.glob(f'{R}/run/01-*')):
        kind=case.rsplit('-',1)[1]; p=point(f'{case}/rate-05570', kind)
        if not p: print(f"  {kind:5} (pending)"); continue
        bk,bv=BURSTY.get((kind,arm),('?',float('nan')))
        bursty=f"{bk} {bv:.2f}%"
        el=f"{p['elig']:.0f}% (n={p['n']})" if p['elig'] is not None else '-'
        sp=f"{p['spread'][0]:.0f}..{p['spread'][1]:.0f}s" if p['spread'] else '-'
        print(f"  {kind:5} {p['realised']:8.0f} {p['completed']:6.0f} {p['p99']:7.1f} {p['coll']:5.2f} {p['ws']:4.0f} {p['cp']:7.2f}% {p['lp']:7.1f}% {p['refused']:7.1f}% | {bursty:>12} | {el:>9} {sp:>15}")
    for f,tagname in ((f'{R}/run-status.json','RUN'),(f'{R}/smoke-status.json','SMOKE')):
        try:
            st=json.load(open(f))
            if st.get('state')!='complete': print(f"  runner: {tagname} case {st.get('case')} stage {st.get('stage')} state {st.get('state')}"); break
        except OSError: pass
