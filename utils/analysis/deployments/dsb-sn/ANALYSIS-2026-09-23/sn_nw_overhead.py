#!/usr/bin/env python3
"""Tomislav-RetCtx: SN no-work n=5 (response path ON rounds): bridge overhead over vanilla,
max-throughput reduction, and knee characterisation. Per round first, then median / range
across the five rounds; bridge-vs-vanilla ratios are PAIRED within a round (same night, same
drift). Writes sn_nw_overhead.json next to this script.

Definitions
  capacity mu      median achieved throughput over the saturated points (open-loop: once the
                   offered rate exceeds mu, achieved stays flat at mu). Saturated = mean latency
                   above SAT_MS; the jump between adjacent grid points is 80 -> 400+ ms, so any
                   threshold in 150..400 ms classifies identically except SB @7000 (233 ms,
                   already at its plateau).
  last stable      highest offered grid point below the first saturated one.
  SLO throughput   highest achieved throughput with p99 <= SLO, first crossing, log-linear
                   interpolation between the grid points either side.
  queueing fit     mean(lambda) = m0 + b * lambda / (mu_fit - lambda) on the stable points
                   (M/M/1-shaped delay); mu_fit is compared with the measured mu.
  per-request CPU  slope of cores vs achieved throughput over stable points up to 0.8 mu
                   (core-ms per request); intercept = idle cost.
  matched load     mean / p99 at the same OFFERED rate, and at the same UTILISATION rho=lambda/mu.
"""
import glob, json, math, statistics as st
from collections import defaultdict
from pathlib import Path
import numpy as np

ROOTS = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
CASES = ['nt', 'v', 'pb', 'cgpb', 'sb']
SAT_MS = 200.0
SLOS = (50, 100, 200)
RATES_MATCHED = (500, 2000, 4000, 6000)
RHOS = (0.5, 0.8, 0.9)


def load():
    data = defaultdict(dict)  # (round, case) -> sorted list of points
    for i, root in enumerate(ROOTS, 1):
        pts = defaultdict(list)
        for p in json.load(open(f'{root}/analysis/points.json')):
            pts[p['case']].append(p)
        for case, ps in pts.items():
            data[i][case] = sorted(ps, key=lambda p: p['offered_rps'])
    return data


def capacity(ps):
    sat = [p for p in ps if p['mean_ms'] > SAT_MS]
    first = sat[0]['offered_rps'] if sat else None
    mu = st.median(p['completed_rps'] for p in ps if first and p['offered_rps'] >= first) if sat else None
    stable = [p for p in ps if first is None or p['offered_rps'] < first]
    return mu, first, stable


def slo_throughput(stable, slo):
    prev = None
    for p in stable:
        if p['p99_ms'] > slo:
            if prev is None:
                return None
            x0, y0, x1, y1 = prev['completed_rps'], math.log(prev['p99_ms']), p['completed_rps'], math.log(p['p99_ms'])
            return x0 + (math.log(slo) - y0) * (x1 - x0) / (y1 - y0)
        prev = p
    return stable[-1]['completed_rps'] if stable else None  # never crossed before saturation


def queue_fit(stable, mu_meas):
    lam = np.array([p['completed_rps'] for p in stable], float)
    m = np.array([p['mean_ms'] for p in stable], float)
    best = None
    for mu in np.arange(lam.max() * 1.001, mu_meas * 2.0, 5.0):
        X = np.column_stack([np.ones_like(lam), lam / (mu - lam)])
        coef, res, *_ = np.linalg.lstsq(X, m, rcond=None)
        sse = float(((X @ coef - m) ** 2).sum())
        if coef[1] > 0 and (best is None or sse < best[0]):
            best = (sse, mu, coef)
    sse, mu, (m0, b) = best
    r2 = 1 - sse / float(((m - m.mean()) ** 2).sum())
    return {'mu_fit': mu, 'm0_ms': m0, 'b_ms': b, 'r2': r2}


def cpu_slope(stable, mu, key):
    pts = [p for p in stable if p['completed_rps'] <= 0.8 * mu]
    x = np.array([p['completed_rps'] for p in pts]); y = np.array([p[key] for p in pts])
    slope, icpt = np.polyfit(x, y, 1)
    return slope * 1000.0, icpt  # core-ms per request, idle cores


def interp(ps, xkey, x, ykey, log=False):
    for a, b in zip(ps, ps[1:]):
        if a[xkey] <= x <= b[xkey]:
            ya, yb = (math.log(a[ykey]), math.log(b[ykey])) if log else (a[ykey], b[ykey])
            y = ya + (x - a[xkey]) * (yb - ya) / (b[xkey] - a[xkey])
            return math.exp(y) if log else y
    return None


def per_round(ps):
    mu, first, stable = capacity(ps)
    out = {'mu': mu, 'first_saturated_offered': first,
           'last_stable_offered': stable[-1]['offered_rps'] if stable else None,
           'slo': {s: slo_throughput(stable, s) for s in SLOS},
           'fit': queue_fit(stable, mu),
           'cpu_app': cpu_slope(stable, mu, 'application_cores'),
           'cpu_coll': cpu_slope(stable, mu, 'collector_cores'),
           'at_rate': {r: {k: next((p[k] for p in ps if p['offered_rps'] == r), None) for k in ('mean_ms', 'p99_ms')}
                       for r in RATES_MATCHED},
           'at_rho': {rho: {k: interp(stable, 'completed_rps', rho * mu, k, log=True) for k in ('mean_ms', 'p99_ms')}
                      for rho in RHOS}}
    return out


def summary(values):
    v = [x for x in values if x is not None]
    return (st.median(v), min(v), max(v)) if v else (None, None, None)


def fmt(t, f='{:,.0f}'):
    m, lo, hi = t
    return '--' if m is None else f"{f.format(m)} [{f.format(lo)}..{f.format(hi)}]"


def main():
    data = load()
    rounds = sorted(data)
    R = {c: {r: per_round(data[r][c]) for r in rounds} for c in CASES}
    out = {'roots': ROOTS, 'definitions': __doc__, 'per_round': R}
    print(f'{len(rounds)} rounds: ' + ', '.join(Path(r).name for r in ROOTS))
    print('\n== capacity (plateau throughput, req/s) median [min..max] over rounds; paired change')
    for c in CASES:
        mu = summary([R[c][r]['mu'] for r in rounds])
        vs_v = summary([100 * (R[c][r]['mu'] / R['v'][r]['mu'] - 1) for r in rounds])
        vs_nt = summary([100 * (R[c][r]['mu'] / R['nt'][r]['mu'] - 1) for r in rounds])
        last = summary([R[c][r]['last_stable_offered'] for r in rounds])
        print(f"  {c:5s} mu {fmt(mu)}   vs vanilla {fmt(vs_v, '{:+.1f}%')}   vs no-tracing {fmt(vs_nt, '{:+.1f}%')}"
              f"   last stable offered {fmt(last)}")
    print('\n== SLO throughput: highest achieved req/s with p99 <= SLO (paired change vs vanilla)')
    for s in SLOS:
        for c in CASES:
            x = summary([R[c][r]['slo'][s] for r in rounds])
            d = summary([100 * (R[c][r]['slo'][s] / R['v'][r]['slo'][s] - 1) for r in rounds])
            print(f"  p99<={s:3d}ms {c:5s} {fmt(x)}   vs vanilla {fmt(d, '{:+.1f}%')}")
    print('\n== queueing fit on stable points: mean = m0 + b*l/(mu_fit-l)')
    for c in CASES:
        f = [R[c][r]['fit'] for r in rounds]
        print(f"  {c:5s} mu_fit {fmt(summary([x['mu_fit'] for x in f]))}  vs measured mu "
              f"{fmt(summary([R[c][r]['fit']['mu_fit'] / R[c][r]['mu'] for r in rounds]), '{:.3f}')}x"
              f"  m0 {fmt(summary([x['m0_ms'] for x in f]), '{:.1f}')} ms  b {fmt(summary([x['b_ms'] for x in f]), '{:.2f}')} ms"
              f"  R2 {fmt(summary([x['r2'] for x in f]), '{:.3f}')}")
    print('\n== per-request CPU (stable points <= 0.8 mu): core-ms per request; idle cores')
    for c in CASES:
        app = summary([R[c][r]['cpu_app'][0] for r in rounds]); coll = summary([R[c][r]['cpu_coll'][0] for r in rounds])
        tot = summary([R[c][r]['cpu_app'][0] + R[c][r]['cpu_coll'][0] for r in rounds])
        dv = summary([(R[c][r]['cpu_app'][0] + R[c][r]['cpu_coll'][0]) - (R['v'][r]['cpu_app'][0] + R['v'][r]['cpu_coll'][0]) for r in rounds])
        dvp = summary([100 * ((R[c][r]['cpu_app'][0] + R[c][r]['cpu_coll'][0]) / (R['v'][r]['cpu_app'][0] + R['v'][r]['cpu_coll'][0]) - 1) for r in rounds])
        print(f"  {c:5s} app {fmt(app, '{:.3f}')}  collectors {fmt(coll, '{:.3f}')}  total {fmt(tot, '{:.3f}')}"
              f"  vs vanilla {fmt(dv, '{:+.3f}')} ({fmt(dvp, '{:+.1f}%')})"
              f"  idle app cores {fmt(summary([R[c][r]['cpu_app'][1] for r in rounds]), '{:.1f}')}")
    print('\n== latency at the same OFFERED rate (median over rounds; paired diff vs vanilla)')
    for rate in RATES_MATCHED:
        for c in CASES:
            m = summary([R[c][r]['at_rate'][rate]['mean_ms'] for r in rounds]); q = summary([R[c][r]['at_rate'][rate]['p99_ms'] for r in rounds])
            dm = summary([R[c][r]['at_rate'][rate]['mean_ms'] - R['v'][r]['at_rate'][rate]['mean_ms'] for r in rounds])
            dq = summary([R[c][r]['at_rate'][rate]['p99_ms'] - R['v'][r]['at_rate'][rate]['p99_ms'] for r in rounds])
            print(f"  @{rate:5d} {c:5s} mean {fmt(m, '{:.1f}')}  p99 {fmt(q, '{:.1f}')}   vs v: mean {fmt(dm, '{:+.1f}')}  p99 {fmt(dq, '{:+.1f}')}")
    print('\n== latency at the same UTILISATION rho = lambda/mu (each config at its own capacity)')
    for rho in RHOS:
        for c in CASES:
            m = summary([R[c][r]['at_rho'][rho]['mean_ms'] for r in rounds]); q = summary([R[c][r]['at_rho'][rho]['p99_ms'] for r in rounds])
            print(f"  rho {rho:.1f} {c:5s} mean {fmt(m, '{:.1f}')} ms  p99 {fmt(q, '{:.1f}')} ms")
    json.dump(out, open(Path(__file__).with_suffix('.json'), 'w'), indent=1, default=str)


if __name__ == '__main__':
    main()
