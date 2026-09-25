#!/usr/bin/env python3
"""Tomislav-RetCtx: LIVE mid-point view of the deployed collectors: two prometheus samples via the
API-server proxy (accepted/refused/sent per collector, rates over the gap), plus counts of Jaeger
queue-full rejections and memory_limiter/priority refusals in the last N minutes of each log."""
import json, re, subprocess, sys, time, collections
GAP=float(sys.argv[1]) if len(sys.argv)>1 else 10.0; SINCE=sys.argv[2] if len(sys.argv)>2 else '5m'
def k(*a): return subprocess.run(['kubectl',*a],capture_output=True,text=True,timeout=60).stdout
pods=json.loads(k('get','pods','-A','-o','json'))['items']
cols=[(p['metadata']['namespace'],p['metadata']['name'],p['spec'].get('nodeName','?')) for p in pods if p['metadata']['name'].startswith('otelcol-') and p['status'].get('phase')=='Running']
if not cols: print('no running otelcol pods'); sys.exit()
def sample():
    out={}
    for ns,name,node in cols:
        txt=k('get','--raw',f'/api/v1/namespaces/{ns}/pods/{name}:8888/proxy/metrics'); m=collections.Counter()
        for line in txt.splitlines():
            mm=re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans|exporter_sent_spans|exporter_send_failed_spans|exporter_queue_size)\w*(\{[^}]*\})?\s+([0-9.e+]+)',line)
            if mm: m[mm.group(1)]+=float(mm.group(3))
        out[name]=m
    return out
a=sample(); time.sleep(GAP); b=sample()
ns=cols[0][0]; print(f'namespace {ns}; {len(cols)} collectors; rates over {GAP:.0f} s; log window {SINCE}')
print(f"{'node':7s} {'acc/s':>8s} {'ref/s':>8s} {'sent/s':>8s} {'refused%':>8s} {'queue':>5s} {'acc total':>11s} {'ref total':>11s} | {'qfull':>5s} {'refusing':>8s} {'resume':>6s} {'hp_ref>0 s':>10s} {'state now':>9s}")
T=collections.Counter()
for ns,name,node in sorted(cols,key=lambda c:c[2]):
    d={kk:b[name][kk]-a[name][kk] for kk in ('receiver_accepted_spans','receiver_refused_spans','exporter_sent_spans')}
    acc=d['receiver_accepted_spans']/GAP; ref=d['receiver_refused_spans']/GAP; sent=d['exporter_sent_spans']/GAP
    T['acc']+=acc; T['ref']+=ref; T['sent']+=sent; T['acct']+=b[name]['receiver_accepted_spans']; T['reft']+=b[name]['receiver_refused_spans']
    log=k('logs',name,'-n',ns,f'--since={SINCE}')
    qf=log.count('queue is full'); rf=log.count('Refusing data'); rs=log.count('Resuming normal')
    hp=0; state='-'
    for line in log.splitlines():
        if 'priority_processor_metrics' in line:
            j=json.loads(line.split('priority_processor_metrics\t',1)[1]); state=j['state']
    # per-second hp_refused increments
    prev=None
    for line in log.splitlines():
        if 'priority_processor_metrics' in line:
            j=json.loads(line.split('priority_processor_metrics\t',1)[1])
            if prev is not None and j['hp_refused']>prev: hp+=1
            prev=j['hp_refused']
    T['qf']+=qf
    print(f"{node:7s} {acc:8.0f} {ref:8.0f} {sent:8.0f} {100*ref/max(1,acc+ref):8.2f} {b[name]['exporter_queue_size']:5.0f} {b[name]['receiver_accepted_spans']:11,.0f} {b[name]['receiver_refused_spans']:11,.0f} | {qf:5d} {rf:8d} {rs:6d} {hp:10d} {state:>9s}")
print(f"{'FLEET':7s} {T['acc']:8.0f} {T['ref']:8.0f} {T['sent']:8.0f} {100*T['ref']/max(1,T['acc']+T['ref']):8.2f} {'':5s} {T['acct']:11,.0f} {T['reft']:11,.0f} | {T['qf']:5d}")
