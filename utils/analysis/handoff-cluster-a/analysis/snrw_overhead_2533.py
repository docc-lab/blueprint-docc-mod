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
v0 = stats(data['v'], rates[0])[1]
flat = [r for r in rates if 2500 <= r <= 3300]
peak = {k: max((stats(data[k], r)[0], r) for r in rates) for k in data}
knee = peak['v'][1]
print('rates', rates[0], '..', rates[-1], '| region (fixed 2.5k-3.3k incl. saturation; v mean at', rates[0], 'value):', flat, '| v peak (knee) at', knee)
rng = np.random.default_rng(1)
def overhead(kind, B=1000):
    def one(vr, br):
        m = [stats(br, r)[1] / stats(vr, r)[1] - 1 for r in flat]; p = [stats(br, r)[2] / stats(vr, r)[2] - 1 for r in flat]
        dm = [stats(br, r)[1] - stats(vr, r)[1] for r in flat]
        tk = max(stats(br, r)[0] for r in rates) / max(stats(vr, r)[0] for r in rates) - 1
        kb, kv = stats(br, knee), stats(vr, knee)
        return np.mean(m), np.mean(dm), np.mean(p), tk, kb[1] / kv[1] - 1, kb[2] / kv[2] - 1
    point = one(data['v'], data[kind])
    boots = np.array([one([data['v'][i] for i in rng.integers(0, 5, 5)], [data[kind][i] for i in rng.integers(0, 5, 5)]) for _ in range(B)])
    lo, hi = np.percentile(boots, 5, axis=0), np.percentile(boots, 95, axis=0)
    return point, lo, hi
names = ['flat mean latency (%)', 'flat mean latency (ms)', 'flat p99 (%)', 'saturation throughput (%)', f'mean at knee {knee//100/10}k (%)', f'p99 at knee {knee//100/10}k (%)']
res = {k: overhead(k, 400) for k in ('pb', 'cgpb', 'sb')}
for i, nm in enumerate(names):
    scale = 1 if 'ms' in nm else 100
    print(f'{nm:28s} ' + ' | '.join(f"{k.upper()} {scale*res[k][0][i]:+6.1f} [{scale*res[k][1][i]:+.1f},{scale*res[k][2][i]:+.1f}]" for k in res))
print('peak delivered:', {k: f'{peak[k][0]:,.0f} @ {peak[k][1]}' for k in peak})
print('v flat region pooled mean/p99 by rate:', {r: tuple(round(x, 1) for x in stats(data['v'], r)[1:]) for r in flat})
