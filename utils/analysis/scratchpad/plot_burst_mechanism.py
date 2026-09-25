#!/usr/bin/env python3
"""Tomislav-RetCtx: mechanism figure for one bursty point. Rows: offered rate per 10 s epoch (wrk2
trace, exact); vanilla spans refused per second as a share of arriving spans (memory_limiter
refuse/resume transitions x arrivals, anchored to per-agent refused totals -- the method validated
on the fixed-rate runs); one bridge's refused share split into LP (light) and HP (dark) from the
priority processor's per-second counters (exact). Same x axis, same y scale for the two loss rows."""
import argparse, glob, gzip, json, re
from datetime import datetime, timezone
from pathlib import Path
import matplotlib; matplotlib.use('Agg')
import matplotlib.pyplot as plt

ap=argparse.ArgumentParser()
ap.add_argument('--v', required=True); ap.add_argument('--bridge', required=True); ap.add_argument('--bridge-label', default='CGPB, response path on')
ap.add_argument('--out', default='/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-23/burst-mechanism')
ap.add_argument('--width', type=float, default=2.2); ap.add_argument('--height', type=float, default=None); ap.add_argument('--fontsize', type=float, default=8)
a=ap.parse_args(); fs=a.fontsize; N=600; SPANS_PER_REQ=23

def ts(s): return datetime.strptime(s[:23],'%Y-%m-%dT%H:%M:%S.%f').replace(tzinfo=timezone.utc).timestamp()
def window(P):
    cmd=json.load(open(f'{P}/command.json')); res=json.load(open(f'{P}/result.json'))
    return datetime.fromisoformat(cmd['started']).timestamp(), datetime.fromisoformat(res['finished']).timestamp()
def offered(P):
    R=[None]*N
    for line in open(f'{P}/wrk.stderr'):
        m=re.match(r'burst epoch (\d+) t=[\d.]+s g=[\d.]+ rate=(\d+)',line)
        if m:
            k=int(m.group(1))
            for s in range(k*10, min(N,(k+1)*10)): R[s]=float(m.group(2))
    last=next(x for x in reversed(R) if x is not None)
    return [x if x is not None else last for x in R]
def prom(d):
    out={}
    for f in glob.glob(f'{d}/prometheus-otelcol-*.txt.gz'):
        cid=re.search(r'-ctr-(\w+)\.txt\.gz',f).group(1); m={}
        for line in gzip.open(f,'rt'):
            mm=re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans)\w*(\{[^}]*\})?\s+([0-9.e+]+)',line)
            if mm: m[mm.group(1)]=m.get(mm.group(1),0)+float(mm.group(3))
        out[cid]=m
    return out
def vanilla_series(P, R):
    t0,t1=window(P); before,after=prom(f'{P}/before'),prom(f'{P}/after')
    fleet=[0.0]*N
    for cid in after:
        ref=after[cid].get('receiver_refused_spans',0)-before.get(cid,{}).get('receiver_refused_spans',0)
        if ref<=0: continue
        logf=glob.glob(f'{P}/after/logs-otelcol-*-ctr-{cid}.txt.gz')[0]
        ev=[]
        for line in gzip.open(logf,'rt'):
            if 'memorylimiter' not in line: continue
            m=re.search(r'"cur_mem_mib": (\d+)',line); mem=int(m.group(1)) if m else None; t=ts(line.split('\t')[0])
            if 'Refusing data' in line: ev.append((t,'refuse',mem))
            elif 'Resuming normal' in line: ev.append((t,'resume',mem))
            elif 'after GC' in line: ev.append((t,'gc',mem))
        ev.sort(); ivs=[]; cur=None
        for i,(t,kind,mem) in enumerate(ev):
            if kind=='refuse' and cur is None: cur=t
            elif kind=='resume' and cur is not None: ivs.append((cur,t)); cur=None
            elif kind=='gc' and cur is not None:
                soft = 0.4*256  # admission6040 (60/20) soft = 102.4; admissionotel (80/25) soft = 140.8: read from the config line
                below = mem is not None and mem <= SOFT-1
                if below: ivs.append((cur,t)); cur=None
        if cur is not None: ivs.append((cur,t1))
        w=[0.0]*N
        for s,e in ivs:
            s=max(s,t0); e=min(e,t1)
            for k in range(max(0,int(s-t0)), min(N,int(e-t0)+1)):
                frac=max(0.0,min(e-t0,k+1)-max(s-t0,k)); w[k]+=frac*R[k]
        sw=sum(w)
        if sw>0:
            for k in range(N): fleet[k]+=ref*w[k]/sw
    return [100*fleet[k]/max(1.0,R[k]*SPANS_PER_REQ) for k in range(N)]
def bridge_series(P):
    t0,t1=window(P); hp=[0.0]*N; lp=[0.0]*N; arr=[0.0]*N
    for f in glob.glob(f'{P}/after/logs-otelcol-*.txt.gz'):
        rows=[]
        for line in gzip.open(f,'rt'):
            if 'priority_processor_metrics' not in line: continue
            rows.append((ts(line[:23]), json.loads(line.split('priority_processor_metrics\t',1)[1])))
        rows.sort(key=lambda r:r[0]); prev=None
        for t,j in rows:
            if prev is not None and t0<=t<=t1:
                k=min(N-1,int(t-t0)); dh=j['hp_refused']-prev['hp_refused']; dl=j['lp_refused']-prev['lp_refused']
                da=(j['hp_admitted']-prev['hp_admitted'])+(j['lp_admitted']-prev['lp_admitted'])+dh+dl
                hp[k]+=dh; lp[k]+=dl; arr[k]+=da
            prev=j
    return [100*hp[k]/max(1.0,arr[k]) for k in range(N)], [100*lp[k]/max(1.0,arr[k]) for k in range(N)]

# soft limit for the vanilla state machine: from the memory_limiter config line
SOFT=140.8
for f in glob.glob(f'{a.v}/after/logs-otelcol-*.txt.gz')[:1]:
    for line in gzip.open(f,'rt'):
        m=re.search(r'"limit_mib": (\d+), "spike_limit_mib": (\d+)',line)
        if m: SOFT=int(m.group(1))-int(m.group(2)); break
R=offered(a.v); vser=vanilla_series(a.v,R); hp,lp=bridge_series(a.bridge)
# Tomislav-RetCtx: aggregate to the 10 s burst epochs (arrival-weighted) so the loss rows share the
# offered-rate step structure instead of showing the 0.1 s refuse/resume duty cycle.
E=10
def epochs(series):
    out=[]
    for k in range(0,N,E):
        w=[R[i]*SPANS_PER_REQ for i in range(k,min(N,k+E))]
        out.append(sum(series[i]*w[i-k] for i in range(k,min(N,k+E)))/max(1.0,sum(w)))
    return out
vE,hpE,lpE=epochs(vser),epochs(hp),epochs(lp); tE=list(range(0,N,E))
def stepx(vals): return tE+[N], vals+[vals[-1]]
t=list(range(N))
import burst_fig_style as st
st.setup()
SM=st.SMALL
# Tomislav-RetCtx (user, 2026-09-23): two rows -- offered rate; one refusal row with vanilla
# (gray) drawn over the bridge (light blue), checkpoint refusals (red) stacked on the bridge's LP.
H=a.height or st.H_BOTTOM
# Tomislav-RetCtx (user 2026-09-23): compressed ~19 %; legend moved to one row above the panels
# (same style as the top-row cells) so it no longer competes with the refusal bars
fig,axes=plt.subplots(2,1,figsize=(a.width,H),sharex=True,gridspec_kw=dict(height_ratios=[1,1.3],hspace=0.18))
ax=axes[0]; ax.step(t,[r/1000 for r in R],where='post',color='#1A1A1A',lw=.8)
ax.axhline(5,color='#888888',lw=.6,ls='--')
ax.set_ylabel('Offered\n(req/s)',fontsize=fs); ax.set_ylim(3,6.6); ax.set_yticks([4,6]); ax.set_yticklabels(['4k','6k'])
tot_hp=tot_hp_ref=0
t0,t1=window(a.bridge)
for f in glob.glob(f'{a.bridge}/after/logs-otelcol-*.txt.gz'):
    rows=sorted((ts(l[:23]), json.loads(l.split('priority_processor_metrics\t',1)[1])) for l in gzip.open(f,'rt') if 'priority_processor_metrics' in l)
    inwin=[j for t,j in rows if t0<=t<=t1]
    if len(inwin)>1:
        first=next((j for t,j in reversed(rows) if t<t0), inwin[0]); last=inwin[-1]
        tot_hp_ref+=last['hp_refused']-first['hp_refused']; tot_hp+=last['hp_admitted']-first['hp_admitted']+last['hp_refused']-first['hp_refused']
ymax=max(max(vE),max(x+y for x,y in zip(hpE,lpE)))*1.35
xs,ly=stepx(lpE); _,hy=stepx([x+y for x,y in zip(lpE,hpE)]); _,vy=stepx(vE)
ax=axes[1]
ax.fill_between(xs,0,ly,color='#9DC3E6',lw=0,step='post',label=f'{a.bridge_label} (LP)')
ax.fill_between(xs,0,vy,color='#7F7F7F',lw=0,alpha=.85,step='post',label='Vanilla')
# checkpoint refusals stacked on the bridge's LP and drawn LAST, so vanilla's gray never hides them
ax.fill_between(xs,ly,hy,color='#B2182B',lw=0,step='post',label='checkpoints' if tot_hp_ref else None)
# fixed 0-100 so every variant of this figure shares one scale
ax.set_ylim(0,100); ax.set_ylabel('Refused (%)',fontsize=fs); ax.set_yticks([0,50,100]); ax.set_xlabel('Time (s)',fontsize=fs,labelpad=1)
MECH_TOP=1-st.LEGEND_IN/H
st.top_legend(fig,[h for h in axes[1].get_legend_handles_labels()[0]],top=MECH_TOP,height=H)
for ax in axes:
    ax.set_xlim(0,600); ax.grid(True,lw=.3,alpha=.35); ax.tick_params(length=2,pad=1.5); ax.set_axisbelow(True)
    for sp in ('top','right'): ax.spines[sp].set_visible(False)
axes[1].set_xticks([0,200,400,600])
fig.subplots_adjust(left=.235,right=.955,top=MECH_TOP,bottom=st.BOTTOM_IN/H)
print(f'checkpoints refused: {tot_hp_ref:,} of {tot_hp:,}; per-epoch HP refused share of arrivals: max {max(hpE):.2f}%, epochs with any: {sum(1 for x in hpE if x>0)}')
Path(a.out).parent.mkdir(parents=True,exist_ok=True)
for ext in ('pdf','png','svg'): fig.savefig(f'{a.out}.{ext}',dpi=300)
print('wrote',a.out,'| vanilla mean refused %.1f%% of arrivals, max %.0f%%; %s LP mean %.1f%% max %.0f%%, HP mean %.3f%% max %.2f%%'%(sum(vser)/N,max(vser),a.bridge_label.split(',')[0],sum(lp)/N,max(lp),sum(hp)/N,max(hp)))
