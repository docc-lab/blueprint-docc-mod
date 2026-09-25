# Tomislav-RetCtx: SN real-work bridge overhead vs vanilla (n=5 each, no passthrough): flat region + knee/saturation,
# pooled like the figures (request-weighted mean, merged-spectra p99), 90 % bootstrap CIs by resampling DEPLOYMENTS
import sys, glob, json, numpy as np
sys.path.insert(0, '/users/tomislav/blueprint-docc-mod/utils')
from pool_latency import spectrum
D = '/users/tomislav/deployments/dsb-sn'
ROOT = {'v': f'{D}/retctx-snrw-final-v-20260924T163105Z', 'pb': f'{D}/retctx-snrw-nopt-br-20260924T184423Z',
        'cgpb': f'{D}/retctx-snrw-nopt-br-20260924T184423Z', 'sb': f'{D}/retctx-snrw-nopt-br-20260924T184423Z'}
def load(kind):
    reps = []
    for c in sorted(glob.glob(f'{ROOT[kind]}/run/[0-9][0-9]-{kind}')):
        pts = {}
        for f in glob.glob(f'{c}/rate-*/result.json'):
            d = json.load(open(f)); rows, n = spectrum(f.replace('result.json', 'wrk.stdout'))
            v = {}; [v.__setitem__(a, b) for a, b in rows]
            pts[d['offered_rps']] = (d['completed_rps'], d['mean_ms'], d['completed_requests'], np.array(list(v)), np.array(list(v.values())), n)
        reps.append(pts)
    return reps
def stats(reps, rate):
    xs = [r[rate] for r in reps if rate in r]
    thr = np.mean([x[0] for x in xs]); w = np.array([x[2] for x in xs], float); mean = float((w * [x[1] for x in xs]).sum() / w.sum())
    grid = np.unique(np.concatenate([x[3] for x in xs])); n = np.array([x[5] for x in xs], float)
    F = np.array([np.where(grid < x[3][0], 0, np.where(grid >= x[3][-1], 1, np.interp(grid, x[3], x[4]))) for x in xs])
    pooled = n @ F / n.sum(); p99 = float(grid[np.argmax(pooled >= .99)])
    return thr, mean, p99

data = {k: load(k) for k in ROOT}
rates = sorted(set.intersection(*[set(r) for k in data for r in data[k]]))
KNEE = [3100, 3200, 3300]
def thr_at(reps, stat_i, level):
    """achieved throughput where the pooled latency statistic first reaches level (linear interpolation on the
    response-vs-achieved curve, in offered-rate order)"""
    pts = [stats(reps, r) for r in rates]
    for (t0, *a0), (t1, *a1) in zip(pts, pts[1:]):
        y0, y1 = a0[stat_i], a1[stat_i]
        if y0 < level <= y1: return t0 + (t1 - t0) * (level - y0) / (y1 - y0)
    return float('nan')
def one(vr, br):
    out = []
    for r in KNEE:
        sv, sb = stats(vr, r), stats(br, r); out += [sb[1] / sv[1] - 1, sb[2] / sv[2] - 1]
    out += [np.mean(out[0::2]), np.mean(out[1::2])]
    for i, lvl in ((0, 100.), (1, 500.)):
        out.append(thr_at(br, i, lvl) / thr_at(vr, i, lvl) - 1)
    return np.array(out)
rng = np.random.default_rng(3); res = {}
for k in ('pb', 'cgpb', 'sb'):
    pt = one(data['v'], data[k])
    boots = np.array([one([data['v'][i] for i in rng.integers(0, 5, 5)], [data[k][i] for i in rng.integers(0, 5, 5)]) for _ in range(400)])
    res[k] = (pt, np.nanpercentile(boots, 5, axis=0), np.nanpercentile(boots, 95, axis=0))
labels = [f'{s} at {r/1000:.1f}k' for r in KNEE for s in ('mean', 'p99')] + ['mean, avg 3.1-3.3k', 'p99, avg 3.1-3.3k', 'throughput at mean=100 ms', 'throughput at p99=500 ms']
for i, lab in enumerate(labels):
    print(f'{lab:26s} ' + ' | '.join(f"{k.upper()} {100*res[k][0][i]:+6.1f}% [{100*res[k][1][i]:+.0f},{100*res[k][2][i]:+.0f}]" for k in res)
          + f"   avg {100*np.mean([res[k][0][i] for k in res]):+.1f}%")
print('v thresholds: throughput at mean=100 ms %.0f, at p99=500 ms %.0f' % (thr_at(data['v'], 0, 100.), thr_at(data['v'], 1, 500.)))
