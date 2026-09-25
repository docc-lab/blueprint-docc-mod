import json, statistics
from collections import defaultdict
import matplotlib; matplotlib.use('Agg')
import matplotlib.pyplot as plt
ROOT='/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z'
COLORS={'nt':'#7A7A7A','v':'#333333','pb':'#2878B5','cgpb':'#D36B23','sb':'#33945B'}
LABELS={'nt':'No tracing','v':'Vanilla','pb':'PB','cgpb':'CGPB','sb':'SB'}
rows=json.load(open(ROOT+'/analysis/points.json'))
g=defaultdict(list)
for r in rows: g[(r['kind'], r['offered_rps'])].append(r)
series=defaultdict(list)
for (k,rate),pts in sorted(g.items(), key=lambda kv: kv[0][1]):
    f=lambda n: (statistics.mean([p[n] for p in pts]), statistics.stdev([p[n] for p in pts]) if len(pts)>1 else 0.)
    tp,tps=f('completed_rps'); mn,mns=f('mean_ms'); p9,p9s=f('p99_ms')
    series[k].append((rate,tp,tps,mn,mns,p9,p9s))
plt.rcParams.update({'font.size':8,'axes.labelsize':8,'legend.fontsize':7,'xtick.labelsize':7,'ytick.labelsize':7,'pdf.fonttype':42})
fig,axes=plt.subplots(1,2,figsize=(6.6,2.5))
for ax,(mi,si,lab) in zip(axes,((3,4,'Mean response time (ms)'),(5,6,'p99 response time (ms)'))):
    for k in ('nt','v','pb','cgpb','sb'):
        s=series[k]
        peak=max(r[1] for r in s)
        pre=[r for r in s if r[1] >= 0.995*max(x[1] for x in s if x[0]<=r[0])]  # up to saturation
        ax.errorbar([r[1]/1000 for r in pre],[r[mi] for r in pre],
                    xerr=[r[2]/1000 for r in pre], yerr=[r[si] for r in pre],
                    color=COLORS[k],marker='o',markersize=2,linewidth=.9,capsize=1.5,label=LABELS[k])
        ax.plot(peak/1000, max(r[mi] for r in pre), marker='|', color=COLORS[k], markersize=7)
    ax.set_xlabel('Achieved throughput (k requests/s)'); ax.set_ylabel(lab)
    ax.set_yscale('log'); ax.grid(alpha=.2); ax.spines[['right','top']].set_visible(False)
h,l=axes[0].get_legend_handles_labels()
fig.legend(h,l,loc='upper center',ncol=5,frameon=False)
fig.tight_layout(rect=(0,0,1,.86),pad=.5,w_pad=1.)
out='/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/lat-vs-tput'
for e in ('png','pdf'): fig.savefig(out+'.'+e,dpi=300)
print('wrote', out)
print()
print('saturation point per kind (last point before throughput stops rising):')
for k in ('nt','v','pb','cgpb','sb'):
    s=series[k]; peak=max(s,key=lambda r:r[1])
    pre=[r for r in s if r[1] >= 0.995*max(x[1] for x in s if x[0]<=r[0])]
    last=pre[-1]
    print(f'  {LABELS[k]:11} max throughput {peak[1]:6.0f} | knee branch ends at {last[1]:6.0f} rps, mean {last[3]:8.1f} ms, p99 {last[5]:8.1f} ms | points on branch {len(pre)}/{len(s)}')
