#!/usr/bin/env python3
"""Tomislav-RetCtx: vanilla span-refusal timeline for the two stationary points.

The stock memory_limiter has no per-second counters, so the series is built from three
measured inputs: (1) every refuse/resume transition in each collector's log (state machine
below, incl. silent ends after a forced GC), (2) the offered request rate per second (wrk2
burst trace for the bursty point, constant for the fixed point), (3) each collector's
refused total over the point (prometheus after-before). While a collector refuses it refuses
everything, so refused_c(t) = total_c * R(t)*1[refusing_c(t)] / sum_t R(t)*1[refusing_c(t)].
Two checks against measured data follow. Writes bursty|v|- and fixed|v|- into ckpt_timeline.json."""
import gzip, glob, json, re, os, sys
from datetime import datetime, timezone
S = os.path.dirname(os.path.abspath(__file__))
PTS = {
 'bursty': '/storage/tomislav-retctx-e2e/retctx-nwe2e-burst6k-on-20260922T081601Z/run/01-v/rate-06000',
 'fixed':  '/storage/tomislav-retctx-e2e/retctx-nwe2e-fixed5570-on-20260922T094851Z/run/01-v/rate-05570',
}
SOFT_MIB = 102.4  # 60% limit (153.6) - 20% spike (51.2) of the 256 MiB cgroup limit

def ts(s):  # '2026-09-22T08:40:07.323Z' -> seconds since epoch
    return datetime.strptime(s[:23], '%Y-%m-%dT%H:%M:%S.%f').replace(tzinfo=timezone.utc).timestamp()

def refusing_intervals(logf, t0, t1):
    """Return [(start,end)] seconds relative to t0, clipped to [0, t1-t0]."""
    ev = []
    for line in gzip.open(logf, 'rt'):
        if 'memorylimiter' not in line: continue
        m = re.search(r'"cur_mem_mib": (\d+)', line)
        mem = int(m.group(1)) if m else None
        t = ts(line.split('\t')[0])
        if 'Refusing data' in line: ev.append((t, 'refuse', mem))
        elif 'Resuming normal' in line: ev.append((t, 'resume', mem))
        elif 'after GC' in line: ev.append((t, 'gc', mem))
    ev.sort()
    ivs = []; cur = None
    for i, (t, kind, mem) in enumerate(ev):
        if kind == 'refuse':
            if cur is None: cur = t
        elif kind == 'resume':
            if cur is not None: ivs.append((cur, t)); cur = None
        elif kind == 'gc' and cur is not None:
            # silent end: after a forced GC the limiter stores aboveSoftLimit without logging
            below = mem <= 101 or (mem == 102 and i + 1 < len(ev) and ev[i+1][1] == 'refuse' and ev[i+1][0] - t < 0.6)
            if below: ivs.append((cur, t)); cur = None
    if cur is not None: ivs.append((cur, t1))
    out = []
    for a, b in ivs:
        a = max(a, t0); b = min(b, t1)
        if b > a: out.append((a - t0, b - t0))
    return out

def offered_rate(P, env, n):
    if env == 'fixed':
        r = json.load(open(f'{P}/command.json'))['offered_rps']; return [float(r)] * n
    R = [None] * n
    for line in open(f'{P}/wrk.stderr'):
        m = re.match(r'burst epoch (\d+) t=[\d.]+s g=[\d.]+ rate=(\d+)', line)
        if m and int(m.group(1)) < n: R[int(m.group(1))] = float(m.group(2))
    assert all(x is not None for x in R), 'missing epochs'
    return R

def prom(d):
    out = {}
    for f in glob.glob(f'{d}/prometheus-otelcol-*.txt.gz'):
        cid = re.search(r'-ctr-(\w+)\.txt\.gz', f).group(1); m = {}
        for line in gzip.open(f, 'rt'):
            mm = re.match(r'otelcol_(receiver_accepted_spans|receiver_refused_spans)\w*(\{[^}]*\})?\s+([0-9.e+]+)', line)
            if mm: m[mm.group(1)] = m.get(mm.group(1), 0) + float(mm.group(3))
        out[cid] = m
    return out

def sdk_series(P, t0, n):
    """per-service cumulative spans_dropped per 1 s bin from vanilla_processor_metrics; None if truncated."""
    out = {}
    for f in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
        svc = re.search(r'logs-(.*)-service-v-es', f).group(1)
        lines = gzip.open(f, 'rt').read().splitlines()
        if len(lines) >= 2000: out[svc] = None; continue
        pts = []
        for line in lines:
            m = re.match(r'(\d{4}/\d\d/\d\d \d\d:\d\d:\d\d) INFO vanilla_processor_metrics .*spans_dropped=(\d+)', line)
            if m:
                t = datetime.strptime(m.group(1), '%Y/%m/%d %H:%M:%S').replace(tzinfo=timezone.utc).timestamp() - t0
                pts.append((t, int(m.group(2))))
        if not pts: out[svc] = None; continue
        # value at end of bin k = last sample with t <= k+1, minus value at t<=0
        base = max([v for t, v in pts if t <= 0], default=pts[0][1])
        ser = []; j = 0; last = base
        for k in range(n):
            while j < len(pts) and pts[j][0] <= k + 1: last = pts[j][1]; j += 1
            ser.append(last - base)
        out[svc] = ser
    return out

NODE_SVC = {'node-1': ['composepost', 'userid'], 'node-2': ['hometimeline', 'urlshorten'], 'node-3': ['usermention', 'usertimeline'],
            'node-4': ['socialgraph', 'text'], 'node-5': ['post-storage'], 'node-6': ['media', 'uniqueid'], 'node-7': ['wrk2api'], 'node-8': ['user']}

data = json.load(open(f'{S}/ckpt_timeline.json'))
for env, P in PTS.items():
    cmd = json.load(open(f'{P}/command.json')); res = json.load(open(f'{P}/result.json'))
    t0 = datetime.fromisoformat(cmd['started']).timestamp(); t1 = datetime.fromisoformat(res['finished']).timestamp()
    n = 300
    R = offered_rate(P, env, n)
    before, after = prom(f'{P}/before'), prom(f'{P}/after')
    pods = json.load(open(f'{P}/after/pods.json'))['items']
    node_of = {it['metadata']['name'].split('-ctr-')[-1]: it['spec']['nodeName'] for it in pods if it['metadata']['name'].startswith('otelcol-v-es')}
    sdk = sdk_series(P, t0, n)
    print(f'\n===== {env}  ({P})  span total this point = accepted+refused')
    print(f"{'coll':6s} {'node':7s} {'refused':>9s} {'ref%':>6s} {'refusing_s':>10s} {'time%':>6s} {'episodes':>8s} {'first_s':>7s}  sdk-check")
    fleet_cum = [0.0] * (n + 1); tot_all = 0.0; tot_ref = 0.0; onset = None
    per = {}
    for cid in sorted(after):
        acc = after[cid]['receiver_accepted_spans'] - before[cid].get('receiver_accepted_spans', 0)
        ref = after[cid].get('receiver_refused_spans', 0) - before[cid].get('receiver_refused_spans', 0)
        tot_all += acc + ref; tot_ref += ref
        logf = glob.glob(f'{P}/after/logs-otelcol-v-es-*-ctr-{cid}.txt.gz')[0]
        ivs = refusing_intervals(logf, t0, t1)
        w = [0.0] * n
        for a, b in ivs:
            for k in range(int(a), min(n, int(b) + 1)):
                frac = max(0.0, min(b, k + 1) - max(a, k))
                w[k] += frac * R[k]
        sw = sum(w)
        per_bin = [ref * x / sw if sw > 0 else 0.0 for x in w]
        cum = [0.0]
        for x in per_bin: cum.append(cum[-1] + x)
        for k in range(n + 1): fleet_cum[k] += cum[k]
        rt = sum(b - a for a, b in ivs)
        first = ivs[0][0] if ivs else None
        if first is not None and ref > 0 and (onset is None or first < onset): onset = first
        # check B: SDK-measured cumulative drops for the services on this node, if their logs are complete
        node = node_of[cid]; svcs = NODE_SVC[node]
        chk = ''
        if all(sdk.get(s) is not None for s in svcs):
            meas = [sum(sdk[s][k] for s in svcs) for k in range(n)]
            dev = max(abs(meas[k] - cum[k + 1]) for k in range(n))
            m_on = next((k for k in range(n) if meas[k] > 0), None)
            chk = f"sdk_total={meas[-1]:,} recon_total={ref:,.0f} max|dev|={dev:,.0f} ({100*dev/max(1,acc+ref):.2f}% of node spans) sdk_onset={m_on}s"
        else:
            chk = 'sdk logs truncated for ' + ','.join(s for s in svcs if sdk.get(s) is None)
        print(f"{cid:6s} {node:7s} {ref:9,.0f} {100*ref/max(1,acc+ref):6.1f} {rt:10.1f} {100*rt/300:6.1f} {len(ivs):8d} {('%.0f'%first) if first is not None else '-':>7s}  {chk}")
        per[cid] = {'node': node, 'refused': ref, 'accepted': acc, 'refusing_seconds': rt, 'episodes': len(ivs), 'first_refuse_s': first}
    cum_pct = [100 * x / tot_all for x in fleet_cum]
    print(f"FLEET refused={tot_ref:,.0f} of {tot_all:,.0f} = {100*tot_ref/tot_all:.2f}%  (result.json collector_deltas refused={res['collector_deltas']['otelcol_receiver_refused_spans_total']:,.0f})  onset={onset:.0f}s")
    data[f'{env}|v|-'] = {'pods': len(after), 't': list(range(n + 1)), 'cum_pct': cum_pct, 'cum_refused': fleet_cum,
                          'final_pct': cum_pct[-1], 'onset': onset, 'per_collector': per,
                          'method': 'memory_limiter refuse/resume transitions (collector logs) x offered rate per second, anchored to per-collector refused totals (prometheus)'}
json.dump(data, open(f'{S}/ckpt_timeline.json', 'w'))
print('\nwrote', f'{S}/ckpt_timeline.json', 'keys:', sorted(data))
