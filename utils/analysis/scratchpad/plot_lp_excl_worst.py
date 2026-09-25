#!/usr/bin/env python3
"""Tomislav-RetCtx: non-checkpoint (LP) spans refused by the bridges' agents, excluding each
configuration's worst agent. Per configuration: bar = aggregate LP refused / LP arriving over the
seven other agents; dots = each of those agents; open marker = the excluded worst agent, labelled.
Source: the priority processor's per-second counters in every agent's log over the measured point."""
import argparse, glob, gzip, json, re
from datetime import datetime, timezone
from pathlib import Path
import matplotlib; matplotlib.use('Agg')
import matplotlib.pyplot as plt
ap=argparse.ArgumentParser(); ap.add_argument('--on',required=True); ap.add_argument('--off',required=True)
ap.add_argument('--out',default='/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-23/lp-loss-excluding-worst-agent')
ap.add_argument('--width',type=float,default=3.33); ap.add_argument('--height',type=float,default=2.1); ap.add_argument('--fontsize',type=float,default=7)
a=ap.parse_args(); fs=a.fontsize
NODE_SVC={'node-1':'composepost','node-2':'hometimeline','node-3':'usermention','node-4':'socialgraph','node-5':'post-storage','node-6':'media','node-7':'wrk2api','node-8':'user'}
def ts(s): return datetime.strptime(s[:23],'%Y-%m-%dT%H:%M:%S.%f').replace(tzinfo=timezone.utc).timestamp()
def per_agent(P):
    cmd=json.load(open(f'{P}/command.json')); res=json.load(open(f'{P}/result.json'))
    t0=datetime.fromisoformat(cmd['started']).timestamp(); t1=datetime.fromisoformat(res['finished']).timestamp()
    node_of={it['metadata']['name']:it['spec']['nodeName'] for it in json.load(open(f'{P}/after/pods.json'))['items']}
    out={}
    for f in glob.glob(f'{P}/after/logs-otelcol-*.txt.gz'):
        name=re.sub(r'^logs-','',f.rsplit('/',1)[1]).replace('.txt.gz',''); node=node_of.get(name,'?')
        rows=sorted((ts(l[:23]),json.loads(l.split('priority_processor_metrics\t',1)[1])) for l in gzip.open(f,'rt') if 'priority_processor_metrics' in l)
        inwin=[j for t,j in rows if t0<=t<=t1]
        if len(inwin)<2: continue
        first=next((j for t,j in reversed(rows) if t<t0), inwin[0]); last=inwin[-1]
        lr=last['lp_refused']-first['lp_refused']; la=last['lp_admitted']-first['lp_admitted']
        out[node]=(lr, lr+la)
    return out
def per_agent_all(P):
    """Tomislav-RetCtx (user: 'overall' for vanilla): all spans refused per agent, from each agent's
    receiver counters (prometheus, after - before); vanilla has no priority classes."""
    node_of={it['metadata']['name']:it['spec']['nodeName'] for it in json.load(open(f'{P}/after/pods.json'))['items']}
    def prom(d):
        out={}
        for f in glob.glob(f'{d}/prometheus-otelcol-*.txt.gz'):
            name=re.sub(r'^prometheus-','',f.rsplit('/',1)[1]).replace('.txt.gz',''); m={}
            for line in gzip.open(f,'rt'):
                mm=re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans)\w*(\{[^}]*\})?\s+([0-9.e+]+)',line)
                if mm: m[mm.group(1)]=m.get(mm.group(1),0)+float(mm.group(3))
            out[name]=m
        return out
    b,af=prom(f'{P}/before'),prom(f'{P}/after'); out={}
    for name,m in af.items():
        acc=m.get('receiver_accepted_spans',0)-b.get(name,{}).get('receiver_accepted_spans',0)
        ref=m.get('receiver_refused_spans',0)-b.get(name,{}).get('receiver_refused_spans',0)
        out[node_of.get(name,'?')]=(ref, acc+ref)
    return out
CONF=[('pb','ON'),('pb','OFF'),('cgpb','ON'),('cgpb','OFF'),('sb','ON'),('sb','OFF')]
LABEL={'pb':'PB','cgpb':'CGPB','sb':'SB'}
rows=[]
for kind,arm in CONF:
    root=a.on if arm=='ON' else a.off; P=f'{root}/run/01-{kind}/rate-04500'
    agents=per_agent(P)
    frac={n:100*r/max(1,t) for n,(r,t) in agents.items()}
    worst=max(frac,key=frac.get); rest={n:v for n,v in frac.items() if n!=worst}
    agg=100*sum(r for n,(r,t) in agents.items() if n!=worst)/max(1,sum(t for n,(r,t) in agents.items() if n!=worst))
    allagg=100*sum(r for r,t in agents.values())/max(1,sum(t for r,t in agents.values()))
    rows.append(dict(kind=kind,arm=arm,worst=worst,worst_frac=frac[worst],rest=rest,agg=agg,allagg=allagg))
    print(f"{LABEL[kind]:4s} {arm:3s} all agents {allagg:5.1f}%  excl. worst ({worst} {NODE_SVC[worst]} {frac[worst]:.1f}%) -> {agg:5.1f}%   others: "+' '.join(f"{n[-1]}:{v:.0f}" for n,v in sorted(rest.items())))
agents=per_agent_all(f'{a.on}/run/01-v/rate-04500')
frac={n:100*r/max(1,t) for n,(r,t) in agents.items()}
worst=max(frac,key=frac.get); rest={n:v for n,v in frac.items() if n!=worst}
vrow=dict(kind='v',arm='-',worst=worst,worst_frac=frac[worst],rest=rest,
          agg=100*sum(r for n,(r,t) in agents.items() if n!=worst)/max(1,sum(t for n,(r,t) in agents.items() if n!=worst)),
          allagg=100*sum(r for r,t in agents.values())/max(1,sum(t for r,t in agents.values())))
print(f"Vanilla (all spans) all agents {vrow['allagg']:5.1f}%  excl. worst ({worst} {NODE_SVC[worst]} {frac[worst]:.1f}%) -> {vrow['agg']:5.1f}%   others: "+' '.join(f"{n[-1]}:{v:.0f}" for n,v in sorted(rest.items())))
import burst_fig_style as st
st.setup()
fig,ax=plt.subplots(figsize=(st.W,st.H_TOP))
ys=list(range(len(st.ORDER)))[::-1]
byk={f"{r['kind']}-{r['arm'].lower()}":r for r in rows}
for y,key in zip(ys,st.ORDER):
    if key=='v':
        r=vrow; c='#7F7F7F'; off=False
    else:
        r=byk[key]; c=st.BRIDGE[r['kind']]; off=r['arm']=='OFF'
    ax.barh(y,r['agg'],height=.62,color=c,alpha=.35 if off else .6,lw=0,hatch='////' if off else None,edgecolor=c)
    ax.scatter(list(r['rest'].values()),[y]*len(r['rest']),s=6,color=c,edgecolor='white',linewidth=.3,zorder=3)
    ax.scatter([r['worst_frac']],[y],s=11,facecolor='none',edgecolor=st.BROKEN,linewidth=.7,zorder=3)
    # Tomislav-RetCtx (user): right column = excluding the worst agent, (all agents) in parentheses
    ax.text(102,y,f"{r['agg']:.0f} ({r['allagg']:.0f})",va='center',ha='left',fontsize=st.SMALL,color='#333333')
    # vanilla has no priority classes: its row is ALL spans refused (user notes this in the caption)

ax.set_xlabel('LP spans refused (%)',labelpad=1)
st.finish_bars(ax,fig,ys)
from matplotlib.lines import Line2D
from matplotlib.patches import Patch
st.top_legend(fig,[Patch(facecolor='#999999',alpha=.55,label='7 agents'),
                   Line2D([],[],marker='o',ls='none',ms=3,color='#999999',label='each'),
                   Line2D([],[],marker='o',ls='none',ms=3.8,markerfacecolor='none',markeredgecolor=st.BROKEN,label='worst')])
st.save(fig,a.out)
