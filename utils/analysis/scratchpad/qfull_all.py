#!/usr/bin/env python3
# Tomislav-RetCtx: Jaeger "sending queue is full" pushback per stationary point + Jaeger queue gauges
import gzip, glob, json, re, os
ROOTS = {
 ('bursty','on'): '/storage/tomislav-retctx-e2e/retctx-nwe2e-burst6k-on-20260922T081601Z/run',
 ('bursty','off'): '/storage/tomislav-retctx-e2e/retctx-nwe2e-burst6k-off-20260922T091143Z/run',
 ('fixed','on'): '/storage/tomislav-retctx-e2e/retctx-nwe2e-fixed5570-on-20260922T094851Z/run',
 ('fixed','off'): '/storage/tomislav-retctx-e2e/retctx-nwe2e-fixed5570-off-20260922T102314Z/run',
}
def jaeger_gauges(d):
    out={}
    for f in glob.glob(f'{d}/backend-jaeger-*.txt.gz'):
        for line in gzip.open(f,'rt'):
            if line.startswith('#'): continue
            m=re.match(r'(jaeger_collector_queue_length|jaeger_collector_queue_capacity|jaeger_collector_spans_dropped_total|jaeger_collector_spans_received_total|jaeger_collector_spans_saved_by_svc_total|otelcol_exporter_queue_size|otelcol_exporter_queue_capacity|otelcol_exporter_enqueue_failed_spans|otelcol_receiver_refused_spans|otelcol_exporter_send_failed_spans)\w*(\{[^}]*\})?\s+([0-9.e+]+)', line)
            if m:
                out[m.group(1)] = out.get(m.group(1),0)+float(m.group(3))
    return out
print(f"{'env':6s} {'arm':3s} {'case':5s} {'qfull_total':>11s} {'per_coll':>8s} {'first_qfull_s':>13s} {'retry_other':>11s}  jaeger_after")
for (env,arm),run in sorted(ROOTS.items()):
    for case_dir in sorted(glob.glob(f'{run}/01-*')):
        case=os.path.basename(case_dir)[3:]
        pts=[p for p in glob.glob(f'{case_dir}/rate-*') if os.path.isdir(p)]
        if not pts: continue
        P=pts[0]
        cmd=json.load(open(f'{P}/command.json')); res=json.load(open(f'{P}/result.json'))
        st=cmd['started'][:19].replace('T',' '); fi=res['finished'][:19].replace('T',' ')
        from datetime import datetime
        t0=datetime.fromisoformat(cmd['started'][:26])
        q=0; o=0; n=0; first=None
        for f in glob.glob(f'{P}/after/logs-otelcol-*.txt.gz'):
            n+=1
            for line in gzip.open(f,'rt'):
                if 'retry_sender' not in line: continue
                ts=line[:19].replace('T',' ')
                if not (st<=ts<=fi): continue
                if 'queue is full' in line:
                    q+=1
                    tt=datetime.fromisoformat(line[:23])
                    dt=(tt-t0).total_seconds()
                    if first is None or dt<first: first=dt
                else: o+=1
        g=jaeger_gauges(f'{P}/after')
        gs=' '.join(f"{k.replace('jaeger_collector_','jc_').replace('otelcol_exporter_','oe_')}={int(v)}" for k,v in sorted(g.items()))

        print(f"{env:6s} {arm:3s} {case:5s} {q:11d} {q/max(1,n):8.1f} {('%.0f'%first) if first is not None else '-':>13s} {o:11d}  {gs}")
