#!/usr/bin/env python3
"""Tomislav-RetCtx: n=1 ramp figure (2x2, 2.2 in cells, 8 pt minimum) for one app on the current stack:
(a) delivered vs offered, (b) mean latency, (c) p99 latency (no loss panel: user 2026-09-24).
Loss, HotelReservation: exact trace census from ClickHouse (<point>/trace-census.json): vanilla broken traces;
bridges LP-only (reconstructable) traces and broken traces (checkpoint lost; 0 at every rate in the 2026-09-24 sweep).
Loss, Social Network: collector counters (no trace census for that round): vanilla spans lost before the agents
(every one breaks a trace); bridges LP spans lost, as % of ALL spans (HP lost at the agents is printed).
usage: plot_ramp_n1.py --app hotel|sn --nt ROOT [ROOT ...] --v ROOT [..] --bridges ROOT [..] --out PREFIX [--title T]
(every repetition run/NN-<kind> and ramp pass in the roots is pooled; mean and p99 carry bootstrap 5-95 % error bars)"""
import argparse, glob, json, os, re, subprocess, sys
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D

COLORS = {'nt': '#7A7A7A', 'v': '#222222', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABELS = {'nt': 'None', 'v': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
ORDER = ['nt', 'v', 'pb', 'cgpb', 'sb']
W, H, FS = 2.2, 1.55, 8

def _point(f):
    d = json.load(open(f))
    if 'collector_deltas' not in d: return None
    c = os.path.join(os.path.dirname(f), 'trace-census.json')
    d['_census'] = json.load(open(c)) if os.path.exists(c) else None
    d['_dir'] = os.path.dirname(f)
    return d

EXCLUDE = []  # Tomislav-RetCtx: repetition directories left out on request (--exclude), printed and recorded with the figure

def case_dirs(roots, kind):
    """Tomislav-RetCtx (2026-09-24): every repetition of a kind (run/NN-<kind>, one fresh deployment each) across the
    given roots, in root order; the first directory's rates are the grid. Directories matching an EXCLUDE substring
    (e.g. 'retctx-snrw-final-nt-20260924T163105Z/run/05-nt') are left out."""
    roots = [roots] if isinstance(roots, str) else roots
    return [d for r in roots for d in sorted(glob.glob(f'{r}/run/[0-9][0-9]-{kind}'))
            if '-interrupted-' not in d and '-superseded' not in d and not any(e in d for e in EXCLUDE)]

def record_exclusions(out, dirs):
    """Write <out>.runs.txt: the repetition directories each curve pools, and what was excluded."""
    with open(f'{out}.runs.txt', 'w') as f:
        for k, ds in dirs.items(): f.write(f'{k}: ' + ' '.join(ds) + '\n')
        f.write('excluded: ' + (' '.join(EXCLUDE) or 'none') + '\n')

def pooled_q(wins, q):
    """Smallest spectrum value whose pooled CDF reaches q (pool_latency.pooled's rule), by bisection."""
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from pool_latency import cdf
    total = sum(n for _, n in wins); grid = sorted({v for w, _ in wins for v, _ in w}); lo, hi = 0, len(grid) - 1
    while lo < hi:
        mid = (lo + hi) // 2
        if sum(n * cdf(w, grid[mid]) for w, n in wins) / total >= q: hi = mid
        else: lo = mid + 1
    return grid[lo]

def pool_p99(wins, q=.99, n_boot=200, seed=7):
    """Tomislav-RetCtx: pooled quantile of wrk2 spectra (pool_latency.pooled's rule: the smallest spectrum value whose
    request-weighted pooled CDF reaches q, each window's CDF linear between its spectrum rows) and a bootstrap 5-95 %
    band over windows, vectorized: each spectrum is parsed once; a resample's quantile only considers grid values of
    the windows it drew. Returns (pooled, lo, hi)."""
    import numpy as np
    grid = np.array(sorted({v for w, _ in wins for v, _ in w})); F = np.empty((len(wins), len(grid))); M = np.zeros_like(F, dtype=bool)
    for i, (rows, _) in enumerate(wins):
        vals, fr = {}, None
        for v, f in rows: vals[v] = f  # duplicate values: the last row wins (bisect_right in pool_latency.cdf)
        xv, fv = np.array(list(vals)), np.array(list(vals.values()))
        F[i] = np.where(grid < xv[0], 0., np.where(grid >= xv[-1], 1., np.interp(grid, xv, fv)))
        M[i] = np.isin(grid, xv)
    n = np.array([c for _, c in wins], dtype=float)
    def quantile(mult):
        pooled = (mult * n) @ F / (mult * n).sum(); ok = (pooled >= q) & ((mult > 0) @ M)
        return grid[np.argmax(ok)] if ok.any() else grid[((mult > 0) @ M)][-1]
    rng = np.random.default_rng(seed)
    boots = np.sort([quantile(np.bincount(rng.integers(0, len(wins), len(wins)), minlength=len(wins)).astype(float)) for _ in range(n_boot)])
    return quantile(np.ones(len(wins))), boots[int(.05 * n_boot)], boots[int(.95 * n_boot) - 1]

def mean_band(res, n_boot=200, seed=7):
    """Tomislav-RetCtx: bootstrap 5-95 % band of the request-weighted mean latency over runs (windows)."""
    import numpy as np
    rq = np.array([r['completed_requests'] for r in res], float); mm = np.array([r['mean_ms'] for r in res], float)
    idx = np.random.default_rng(seed).integers(0, len(res), (n_boot, len(res)))
    b = np.sort((rq[idx] * mm[idx]).sum(1) / rq[idx].sum(1))
    return b[int(.05 * n_boot)], b[int(.95 * n_boot) - 1]

def points(case_dirs):
    """One entry per offered rate. The entry pools every run of that load level: ramp passes (<case>/pass-kk/rate-*,
    same grid as pass 1) and the other repetitions' case directories (fresh deployments, same grid). delivered = mean
    over runs, mean = request-weighted, p99 = merged wrk2 spectra with a bootstrap 5-95 % band over runs ('_band'),
    census affected = mean over runs, broken bound = max; '_passes' / '_runs' hold the per-run results."""
    import random
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from pool_latency import spectrum
    case_dirs = [case_dirs] if isinstance(case_dirs, str) else case_dirs
    if not case_dirs: return []
    case_dir, out = case_dirs[0], []
    for f in sorted(glob.glob(f'{case_dir}/rate-*/result.json')):
        d = _point(f)
        if d is None: continue
        rate = os.path.basename(d['_dir'])
        runs = [d] + [q for q in (_point(g) for g in sorted(glob.glob(f'{case_dir}/pass-*/{rate}/result.json'))) if q]
        for c in case_dirs[1:]:
            runs += [q for q in (_point(g) for g in [f'{c}/{rate}/result.json'] + sorted(glob.glob(f'{c}/pass-*/{rate}/result.json'))
                                 if os.path.exists(g)) if q]
        d = dict(d, _runs=runs)
        if len(runs) > 1:
            n = sum(r['completed_requests'] for r in runs)
            wins = [w for w in (spectrum(f"{r['_dir']}/wrk.stdout") for r in runs) if w[0] and w[1]]
            p99, lo, hi = pool_p99(wins)
            d = dict(d, _passes=runs, _band=(lo, hi), _mband=mean_band(runs), completed_rps=sum(r['completed_rps'] for r in runs) / len(runs),
                     mean_ms=sum(r['mean_ms'] * r['completed_requests'] for r in runs) / n, p99_ms=p99)
            cs = [r['_census'] for r in runs]
            if all(c and not c.get('missed') for c in cs):
                d['_census'] = dict(cs[0], affected_pct=sum(c['affected_pct'] for c in cs) / len(cs),
                                    broken_upper_pct=max(c.get('broken_upper_pct', 0) for c in cs), passes=len(cs))
            else:
                d['_census'] = {'missed': True, 'reason': 'a pass has no census'}
        out.append(d)
    return sorted(out, key=lambda d: d['offered_rps'])

def hplp(case_dir, full=None):
    """per offered rate: (LP lost % of LP, HP lost at agents, HP share %) or vanilla spans lost %"""
    here = os.path.dirname(os.path.abspath(__file__))
    tool = os.environ.get('HPLP', '/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/snnw_hplp.py')
    per = []  # one dict per run: each repetition's case directory, then its <case>/pass-kk
    for d in [x for c in ([case_dir] if isinstance(case_dir, str) else case_dir) for x in [c] + sorted(glob.glob(f'{c}/pass-*'))]:
        res = {}
        for l in subprocess.run([sys.executable, tool, d] + ([full] if full else []), capture_output=True, text=True).stdout.splitlines():
            r = int(l.split(':')[0]); lp = re.search(r'LP lost +([\d.]+)%', l); va = re.search(r'before agents +([\d.]+)%', l)
            # Tomislav-RetCtx (2026-09-24): HP lost = SDK + agents (the helper now reports both; LP lost includes SDK drops)
            hp = re.search(r'HP lost: SDK (\d+), agents (\d+)', l) or re.search(r'HP lost at agents (\d+)', l); sh = re.search(r'HP share (\d+)%', l)
            res[r] = (float(lp.group(1)) if lp else float(va.group(1)), sum(int(x) for x in hp.groups()) if hp else None, int(sh.group(1)) if sh else None)
        per.append(res)
    if len(per) == 1:
        return per[0]
    # passes: loss % averaged, HP lost summed, over the passes that have the rate
    out = {}
    for r in per[0]:
        xs = [p[r] for p in per if r in p]
        out[r] = (sum(x[0] for x in xs) / len(xs), None if xs[0][1] is None else sum(x[1] for x in xs),
                  None if xs[0][2] is None else round(sum(x[2] for x in xs) / len(xs)))
    return out

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--app', choices=('hotel', 'sn'), default=None, help='(unused; kept for older command lines)')
    # Tomislav-RetCtx (2026-09-24): ROOTs; every repetition (run/NN-<kind>) and pass in them is pooled, the first root's
    # first repetition defines the grid
    ap.add_argument('--nt', nargs='+', required=True); ap.add_argument('--v', nargs='+', required=True); ap.add_argument('--bridges', nargs='+', required=True)
    ap.add_argument('--out', required=True); ap.add_argument('--title', default='')
    ap.add_argument('--xmin', type=float, default=0, help='x-axis start (k req/s)'); ap.add_argument('--xstep', type=float, default=None, help='x tick step (k req/s)')
    ap.add_argument('--ymax-mean', type=float, default=60); ap.add_argument('--ymax-p99', type=float, default=200)
    ap.add_argument('--exclude', nargs='*', default=[], help='repetition directories to leave out (path substrings)')
    a = ap.parse_args(); EXCLUDE[:] = a.exclude
    plt.rcParams.update({'font.size': FS, 'axes.labelsize': FS, 'xtick.labelsize': FS, 'ytick.labelsize': FS, 'legend.fontsize': FS,
                         'font.family': 'sans-serif', 'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                         'pdf.fonttype': 42, 'svg.fonttype': 'none'})
    roots = {'nt': a.nt, 'v': a.v, 'pb': a.bridges, 'cgpb': a.bridges, 'sb': a.bridges}
    dirs = {k: case_dirs(roots[k], k) for k in ORDER}
    data = {k: points(dirs[k]) for k in ORDER}
    for k in ORDER:
        ns = [len(p.get('_runs', [p])) for p in data[k]]
        print(f'{k}: {len(dirs[k])} repetition dirs, runs per rate {min(ns)}-{max(ns)}, rates {len(ns)}')
    xmax = max(p['offered_rps'] for k in ORDER for p in data[k]) / 1000
    # Tomislav-RetCtx (user 2026-09-24): no loss panel -- one row: (a) delivered, (b) mean, (c) p99; 2.2 x 1.55 in cells
    top_in, bottom_in = .2, .36
    fig, axs = plt.subplots(1, 3, figsize=(3 * W, H + top_in + bottom_in))
    fig.subplots_adjust(left=.075, right=.99, top=1 - top_in / (H + top_in + bottom_in), bottom=bottom_in / (H + top_in + bottom_in), wspace=.36)
    ax_t, ax_m, ax_p = axs
    for k in ORDER:
        xs = [p['offered_rps'] / 1000 for p in data[k]]
        kw = dict(color=COLORS[k], lw=1.1 if k != 'nt' else 1.0, ls=':' if k == 'nt' else '-', marker='o', ms=1.8, mew=0)
        ax_t.plot(xs, [p['completed_rps'] / 1000 for p in data[k]], **kw)
        ax_m.plot(xs, [p['mean_ms'] for p in data[k]], **kw)
        ax_p.plot(xs, [p['p99_ms'] for p in data[k]], **kw)
        # Tomislav-RetCtx (user 2026-09-24): error bars (not shaded bands) on mean AND p99 -- bootstrap 5-95 % over runs
        for ax, stat, bkey in ((ax_m, 'mean_ms', '_mband'), (ax_p, 'p99_ms', '_band')):
            band = [(x, p[stat], p[bkey]) for x, p in zip(xs, data[k]) if p.get(bkey)]
            if band:
                ax.errorbar([b[0] for b in band], [b[1] for b in band], yerr=[[max(0, b[1] - b[2][0]) for b in band], [max(0, b[2][1] - b[1]) for b in band]],
                            fmt='none', ecolor=COLORS[k], elinewidth=.6, capsize=1.3, capthick=.6, zorder=2)
    ax_t.plot([0, xmax], [0, xmax], color='#BBBBBB', lw=.6, ls='--', zorder=0)
    ax_t.set_ylabel('delivered (k req/s)'); ax_t.set_ylim(0, None)
    # Tomislav-RetCtx (user 2026-09-24): never log-scale latency; linear, capped so the pre-knee region is readable
    for ax, lab, top in ((ax_m, 'mean latency (ms)', a.ymax_mean), (ax_p, 'p99 latency (ms)', a.ymax_p99)):
        ax.set_ylabel(lab); ax.set_ylim(0, top)
    for ax, tag in zip(axs.flat, 'abc'):
        ax.set_xlim(a.xmin, xmax * 1.02)
        if a.xmin and ax is ax_t:
            ax.set_ylim(a.xmin, None)
        if a.xstep:
            from matplotlib.ticker import MultipleLocator
            ax.xaxis.set_major_locator(MultipleLocator(a.xstep))
        for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
        ax.grid(True, lw=.3, alpha=.35); ax.set_axisbelow(True); ax.tick_params(length=2, pad=1.5)
        ax.text(.03, .97, f'({tag})', transform=ax.transAxes, ha='left', va='top', fontsize=FS)
    fig.supxlabel('offered load (k req/s)', fontsize=FS, y=.005)
    handles = [Line2D([], [], color=COLORS[k], lw=1.2, ls=':' if k == 'nt' else '-', label=LABELS[k]) for k in ORDER]
    fig.legend(handles=handles, loc='upper center', bbox_to_anchor=(.5, 1.0), ncol=5, frameon=False, handlelength=1.3,
               columnspacing=.9, handletextpad=.35, borderaxespad=.1)
    os.makedirs(os.path.dirname(a.out), exist_ok=True)
    for ext in ('pdf', 'png', 'svg'): fig.savefig(f'{a.out}.{ext}', dpi=300)
    record_exclusions(a.out, dirs)
    print('wrote', a.out, '| excluded:', EXCLUDE or 'none')

if __name__ == '__main__':
    main()
