#!/usr/bin/env python3
"""Tomislav-RetCtx: one compact live line: runner stage + fleet (agents) and gateway rates over a 10 s sample."""
import json, re, subprocess, sys, time, collections, os
TAG=sys.argv[1] if len(sys.argv)>1 else 'sustainotel'; GAP=10
# Tomislav-RetCtx: a root path may be passed instead of a tag; namespace from NS (default dsb-sn)
NS=os.environ.get('NS','dsb-sn'); MODE=os.environ.get('MODE','run')
S='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
def k(*a):
    try: return subprocess.run(['kubectl',*a],capture_output=True,text=True,timeout=30).stdout
    except Exception: return ''
try:
    R=TAG if TAG.startswith('/') else open(f'{S}/{TAG}_roots.txt').read().split()[-1]; st=json.load(open(f'{R}/{MODE}-status.json'))
    stage=f"{st.get('case','-')}/{st.get('stage','-')}@{st.get('offered_rps','-')} {st.get('state','')}"
except Exception: stage='(no runner status)'
pods=json.loads(k('get','pods','-n',NS,'-o','json') or '{"items":[]}')['items']
ag=[(p['metadata']['name'],p['status'].get('podIP')) for p in pods if p['metadata']['name'].startswith('otelcol-') and p['status'].get('phase')=='Running']
gw=[(p['metadata']['name'],p['status'].get('podIP')) for p in pods if p['metadata']['name'].startswith('otelgw-') and p['status'].get('phase')=='Running']
def sample(lst):
    m=collections.Counter()
    for name,ip in lst:
        for line in k('get','--raw',f'/api/v1/namespaces/{NS}/pods/{name}:8888/proxy/metrics').splitlines():
            mm=re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans|exporter_sent_spans|exporter_queue_size|process_cpu_seconds|process_runtime_heap_alloc_bytes)\w*(\{[^}]*\})?\s+([0-9.e+]+)',line)
            if mm: m[mm.group(1)]+=float(mm.group(3))
    return m
a1,g1=sample(ag),sample(gw); t0=time.time(); time.sleep(GAP); a2,g2=sample(ag),sample(gw); dt=time.time()-t0
def r(b,a,kk): return (b[kk]-a[kk])/dt
acc,ref,sent=r(a2,a1,'receiver_accepted_spans'),r(a2,a1,'receiver_refused_spans'),r(a2,a1,'exporter_sent_spans')
line=f"{time.strftime('%H:%M:%S',time.gmtime())} {stage} | agents in {acc:,.0f}/s ref {ref:,.0f}/s ({100*ref/max(1,acc+ref):.1f}%) out {sent:,.0f}/s q {a2['exporter_queue_size']:.0f}"
if gw:
    line+=f" | gw in {r(g2,g1,'receiver_accepted_spans'):,.0f}/s ref {r(g2,g1,'receiver_refused_spans'):,.0f}/s out {r(g2,g1,'exporter_sent_spans'):,.0f}/s q {g2['exporter_queue_size']:.0f} cpu {r(g2,g1,'process_cpu_seconds'):.2f} heap {g2['process_runtime_heap_alloc_bytes']/2**20:.0f}MiB"
print(line, flush=True)
