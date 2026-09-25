import json, glob, sys
def cp_loss(base):
    """Checkpoint spans lost at the SDK (cp_dropped) as a share of cp_received, whole ramp."""
    rec=drop=0
    for p in sorted(glob.glob(base+'/rate-*/result.json')):
        d=p[:-12]
        try:
            b=json.load(open(d+'/before/snapshot.json')); a=json.load(open(d+'/after/snapshot.json'))
        except Exception: continue
        for n,m in a['sdk'].items():
            if '_processor_metrics' not in m or n.startswith('otelcol-'): continue
            pb=b['sdk'].get(n,{}).get('_processor_metrics',{})
            rec+=m['_processor_metrics'].get('cp_received',0)-pb.get('cp_received',0)
            drop+=m['_processor_metrics'].get('cp_dropped',0)-pb.get('cp_dropped',0)
    return rec, drop
PASS='/users/tomislav/deployments/dsb-sn/retctx-nw-n3-cpd2-6-inverse-20260915T215208Z/run'
ADM=open('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/nwe2e_campaign_root').read().strip()+'/run'
print('CHECKPOINT SPANS LOST AT THE APPLICATION SDK, whole ramp')
print('kind | passthrough collectors        | admission-control collectors')
for kind in ('pb','cgpb','sb'):
    for rep in (1,2,3):
        pr,pd = cp_loss(f'{PASS}/{rep:02d}-{kind}')
        ar,ad = cp_loss(f'{ADM}/{rep:02d}-{kind}')
        print('%-5s rep%d | %11d of %11d = %5.2f%% | %9d of %11d = %5.2f%%' % (
            kind, rep, pd, pr, 100*pd/pr if pr else 0, ad, ar, 100*ad/ar if ar else 0))
