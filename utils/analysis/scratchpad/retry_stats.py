"""Tomislav-RetCtx: per point, SDK one-shot retry accounting from the BRIDGES_RETRY_metrics gauges in
the before/after snapshots: spans retried / accepted on retry / refused again, LP spans suppressed
locally, and the retry-queue high-water (spans, protobuf bytes; per-service max since pod start,
cumulative over the ramp). usage: retry_stats.py <case dir>"""
import glob, json, sys
for Pt in sorted(glob.glob(sys.argv[1] + '/rate-*')):
    try:
        B = json.load(open(f'{Pt}/before/snapshot.json')); A = json.load(open(f'{Pt}/after/snapshot.json'))
    except Exception:
        continue
    tot = dict(retried_spans=0, retry_ok_spans=0, retry_fail_spans=0, lp_suppressed_spans=0); hw = {}
    for pod, s in A['sdk'].items():
        a = s.get('BRIDGES_RETRY'); b = B['sdk'].get(pod, {}).get('BRIDGES_RETRY', {})
        if not a:
            continue
        for k in tot:
            tot[k] += a.get(k, 0) - b.get(k, 0)
        hw[pod.split('-service')[0]] = (a.get('retry_max_spans', 0), a.get('retry_max_bytes', 0))
    if not hw:
        print(f'{Pt[-5:]}: no BRIDGES_RETRY gauges'); continue
    top = max(hw.items(), key=lambda kv: kv[1][1])
    print(f"{Pt[-5:]}: retried {tot['retried_spans']:>9,.0f} spans (ok {tot['retry_ok_spans']:>9,.0f}, refused again "
          f"{tot['retry_fail_spans']:>9,.0f}) | LP suppressed {tot['lp_suppressed_spans']:>10,.0f} | queue high-water "
          f"{top[0]} {top[1][0]:,.0f} spans / {top[1][1] / 2**20:.2f} MiB; all: "
          + ' '.join(f'{k}={v[0]:,.0f}/{v[1] / 2**20:.2f}MiB' for k, v in sorted(hw.items())))
