import json, glob, sys
root=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/norev_root').read().strip()
case=sys.argv[1]
base=f'{root}/run/{case}'
rows=[json.load(open(p)) for p in sorted(glob.glob(base+'/rate-*/result.json'))]
rows=[r for r in rows if 'kind' in r and 'restarts_changed' in r]
if not rows: print(case,'no points yet'); raise SystemExit
def prio(d):
    b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
    o={'hp_refused':0,'lp_refused':0,'hp_admitted':0,'lp_admitted':0}; miss=0
    for n,m in a['sdk'].items():
        if not n.startswith('otelcol-') or '_processor_metrics' not in m: continue
        pb=b['sdk'].get(n,{}).get('_processor_metrics')
        if pb is None: miss+=1; continue
        for k in o: o[k]+=m['_processor_metrics'].get(k,0)-pb.get(k,0)
    return o, miss
hp_tot=0
l=max(rows,key=lambda r:r['offered_rps'])
for r in rows:
    o,_=prio(base+'/rate-%05d'%r['offered_rps']); hp_tot+=o['hp_refused']
o,miss=prio(base+'/rate-%05d'%l['offered_rps']); s=l['wall_seconds']
ref=l['collector_deltas'].get('otelcol_receiver_refused_spans_total',0); acc=l['collector_deltas'].get('otelcol_receiver_accepted_spans_total',0)
pk=max(rows,key=lambda r:r['completed_rps'])
print(f"{case}: {len(rows)}/28 points | peak so far {pk['completed_rps']:.0f}@{pk['offered_rps']}")
print(f"  latest offered {l['offered_rps']} completed {l['completed_rps']:.0f} mean {l['mean_ms']:.1f} ms p99 {l['p99_ms']:.1f} ms | refused {ref/s:.0f}/s ({100*ref/(acc+ref) if acc+ref else 0:.1f}%)")
print(f"  latest point priority: hp_refused {o['hp_refused']:.0f} lp_refused {o['lp_refused']:.0f} hp_admitted {o['hp_admitted']:.0f} (collectors missing before-logs: {miss})")
print(f"  hp_refused SUMMED over all {len(rows)} measured points: {hp_tot:.0f}")
