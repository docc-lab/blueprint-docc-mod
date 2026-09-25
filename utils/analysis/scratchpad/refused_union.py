#!/usr/bin/env python3
"""Tomislav-RetCtx: exact trace-level loss census for a measured point from the SDK refused-trace
records (before/after refused-<pod>.bin, 17-byte records: 16-byte trace ID + flags; flag 1 = a
high-priority span was refused, flag 2 = any span was refused). Union across services, divided
by completed requests. Optional cross-check against the settled trace sample."""
import glob, gzip, json, os, re, sys
def records(path):
    b=open(path,'rb').read(); n=len(b)//17
    return [(b[i*17:i*17+16], b[i*17+16]) for i in range(n)]
for P in sys.argv[1:]:
    res=json.load(open(f'{P}/result.json')); completed=res['completed_requests']
    hp=set(); anyl=set(); per={}
    for after in sorted(glob.glob(f'{P}/after/refused-*.bin')):
        pod=os.path.basename(after)[len('refused-'):-4]; before=f'{P}/before/refused-{os.path.basename(after)[8:]}'
        nb=len(open(before,'rb').read())//17 if os.path.exists(before) else 0
        recs=records(after)[nb:]
        h={tid for tid,f in recs if f&1}; a={tid for tid,f in recs if f&2}
        per[pod]=(len(h),len(a)); hp|=h; anyl|=a
    kind=res.get('kind')
    # Tomislav-RetCtx: localize to the node-local agent each service exports to.
    node_of={}
    try:
        for it in json.load(open(f'{P}/after/pods.json'))['items']:
            node_of[it['metadata']['name']]=it['spec'].get('nodeName','?')
    except Exception: pass
    per_agent={}; hp_sets={}; any_sets={}
    for after in sorted(glob.glob(f'{P}/after/refused-*.bin')):
        pod=os.path.basename(after)[len('refused-'):-4]; before=f'{P}/before/refused-{os.path.basename(after)[8:]}'
        nb=len(open(before,'rb').read())//17 if os.path.exists(before) else 0
        recs=records(after)[nb:]; node=node_of.get(pod,'?')
        hp_sets.setdefault(node,set()).update(tid for tid,f in recs if f&1); any_sets.setdefault(node,set()).update(tid for tid,f in recs if f&2)
    print(f"== {P.split('/')[-3]} {P.split('/')[-1]} ({P.split('/')[3]}) completed requests {completed:,}")
    for pod,(h,a) in sorted(per.items(), key=lambda kv:-kv[1][1]):
        if a: print(f"   {re.sub(r'-(v|pb|cgpb|sb|nt)-es-.*','',pod):24s} {node_of.get(pod,'?'):7s} traces with HP loss {h:>9,} ({100*h/max(1,completed):5.2f}%)  any loss {a:>9,} ({100*a/max(1,completed):5.2f}%)")
    print("   per agent (union of its services):")
    for node in sorted(any_sets, key=lambda n:-len(any_sets[n])):
        if any_sets[node]: print(f"      {node:7s} HP-loss traces {len(hp_sets[node]):>9,} ({100*len(hp_sets[node])/max(1,completed):5.2f}%)  any-loss traces {len(any_sets[node]):>9,} ({100*len(any_sets[node])/max(1,completed):5.2f}%)")
    from collections import Counter
    multi=Counter(); multi_hp=Counter()
    for node,sset in any_sets.items():
        for t in sset: multi[t]+=1
    for node,sset in hp_sets.items():
        for t in sset: multi_hp[t]+=1
    dist=Counter(multi.values()); dist_hp=Counter(multi_hp.values())
    print(f"   agents per broken trace (any loss): {dict(sorted(dist.items()))}   (HP loss): {dict(sorted(dist_hp.items()))}")
    print(f"   UNION: traces with a lost checkpoint/HP span {len(hp):,} = {100*len(hp)/max(1,completed):.3f}%   traces with any lost span {len(anyl):,} = {100*len(anyl)/max(1,completed):.3f}%")
    st=f'{P}/settled-traces.json.gz'
    if os.path.exists(st):
        d=json.load(gzip.open(st))['data']; ids=[bytes.fromhex(t['traceID'].rjust(32,'0')) for t in d]
        print(f"   settled sample: {len(ids)} traces, {sum(1 for i in ids if i in hp)} with HP loss ({100*sum(1 for i in ids if i in hp)/max(1,len(ids)):.2f}%), {sum(1 for i in ids if i in anyl)} with any loss ({100*sum(1 for i in ids if i in anyl)/max(1,len(ids)):.2f}%)")
