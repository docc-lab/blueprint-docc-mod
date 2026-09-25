#!/usr/bin/env python3
"""Tomislav-RetCtx: response time against ACHIEVED throughput, one row of two cells (mean | p99) at one column
width (3.33 in), LINEAR y (user 2026-09-24: never log). None, Vanilla, PB, CGPB, SB, each load level pooled over its runs (passes, repetitions).
Every point is drawn at full weight, including the post-saturation ones (delivered stays flat or falls while latency
climbs), so each curve goes straight up at saturation (user 2026-09-24). The y range is capped so the pre-knee region is
readable; points above it leave the top of the panel.
usage: plot_resp_vs_achieved.py --nt DIR [DIR ...] --v DIR --bridges DIR --out PREFIX [--ymax-mean MS] [--ymax-p99 MS]"""
import argparse, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D
from plot_ramp_n1 import points, case_dirs, pooled_q, pool_p99, mean_band, EXCLUDE, record_exclusions, COLORS, LABELS, ORDER
import glob, random
from pool_latency import spectrum, cdf


def windows_of(point_dir):
    """Tomislav-RetCtx: the window plus any knee windows (rate-XXXXX/window-N/) of one run of a load level. The runs
    themselves (ramp passes and repetitions) come from plot_ramp_n1.points()' '_runs'."""
    cands = [point_dir] + sorted(glob.glob(f'{point_dir}/window-*'))
    return [d for d in cands if os.path.exists(f'{d}/wrk.stdout') and os.path.exists(f'{d}/result.json')]


def pooled_point(p, rng):
    """(achieved, pooled mean, pooled p99, band_lo, band_hi, windows) over every window of every run of the load level:
    achieved = mean over windows; pooled mean = request-weighted; p99 from the merged spectra; band = bootstrap
    5-95 pct over windows (None for one window)."""
    import json as _json
    ws = [w for r in p.get('_runs', [p]) for w in windows_of(r['_dir'])]; res = [_json.load(open(f'{w}/result.json')) for w in ws]
    wins = [spectrum(f'{w}/wrk.stdout') for w in ws]
    n_total = sum(r['completed_requests'] for r in res)
    mean = sum(r['mean_ms'] * r['completed_requests'] for r in res) / n_total
    p99 = pooled_q(wins, 0.99); lo = hi = None
    if len(wins) > 1:
        p99, lo, hi = pool_p99(wins)  # Tomislav-RetCtx: vectorized bootstrap (plot_ramp_n1.pool_p99)
    mlo, mhi = mean_band(res) if len(res) > 1 else (None, None)
    return sum(r['completed_rps'] for r in res) / len(res), mean, p99, lo, hi, len(wins), mlo, mhi

# Tomislav-RetCtx (user 2026-09-24): flattened to 2/3 of 1.75 in; legend moved to one row above the panels
W, H, FS = 3.33, 1.17, 8
BOTTOM_IN, TOP_IN = 0.34, 0.17  # margins in inches (tick labels + x label below; one legend row above)

def main():
    ap = argparse.ArgumentParser()
    # Tomislav-RetCtx (2026-09-24): ROOTs, every repetition and pass pooled (plot_ramp_n1.case_dirs / points)
    ap.add_argument('--nt', nargs='+', required=True); ap.add_argument('--v', nargs='+', required=True); ap.add_argument('--bridges', nargs='+', required=True)
    ap.add_argument('--out', required=True); ap.add_argument('--ymax-mean', type=float, default=60); ap.add_argument('--ymax-p99', type=float, default=200)
    ap.add_argument('--xmin', type=float, default=0, help='x-axis start (k req/s)'); ap.add_argument('--xstep', type=float, default=None, help='x tick step (k req/s)')
    ap.add_argument('--exclude', nargs='*', default=[], help='repetition directories to leave out (path substrings)')
    a = ap.parse_args(); EXCLUDE[:] = a.exclude
    plt.rcParams.update({'font.size': FS, 'axes.labelsize': FS, 'xtick.labelsize': FS, 'ytick.labelsize': FS, 'legend.fontsize': FS,
                         'font.family': 'sans-serif', 'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                         'pdf.fonttype': 42, 'svg.fonttype': 'none'})
    roots = {'nt': a.nt, 'v': a.v, 'pb': a.bridges, 'cgpb': a.bridges, 'sb': a.bridges}
    dirs = {k: case_dirs(roots[k], k) for k in ORDER}
    data = {k: points(dirs[k]) for k in ORDER}
    fig, axs = plt.subplots(1, 2, figsize=(W, H))
    fig.subplots_adjust(left=.12, right=.985, top=1 - TOP_IN / H, bottom=BOTTOM_IN / H, wspace=.46)
    xmax = 0
    for ax, key, ymax, lab in ((axs[0], 'mean_ms', a.ymax_mean, 'mean (ms)'), (axs[1], 'p99_ms', a.ymax_p99, 'p99 (ms)')):
        for k in ORDER:
            pts = data[k]
            # Tomislav-RetCtx: pooled over each load level's windows (one window = the plain wrk2 values)
            pooled = [pooled_point(p, random.Random(7)) for p in pts]
            xs = [q[0] / 1000 for q in pooled]
            ys = [q[1] if key == 'mean_ms' else q[2] for q in pooled]
            peak = max(range(len(xs)), key=lambda i: xs[i]); xmax = max(xmax, max(xs))
            ls = ':' if k == 'nt' else '-'
            # Tomislav-RetCtx (user 2026-09-24): post-saturation points at full weight -- the curve goes straight up at
            # saturation (and leaves the top of the panel) instead of fading out
            ax.plot(xs, ys, color=COLORS[k], lw=1.1, ls=ls, marker='o', ms=1.6, mew=0, zorder=3)
            # Tomislav-RetCtx (user 2026-09-24): error bars (not shaded bands) on mean AND p99, every point (bootstrap 5-95 %);
            # every point gets its bar, post-saturation points included
            lo_i, hi_i = (6, 7) if key == 'mean_ms' else (3, 4)
            for seg, alpha in ((range(0, len(xs)), 1.0),):
                band = [(xs[i], ys[i], pooled[i][lo_i], pooled[i][hi_i]) for i in seg if pooled[i][lo_i] is not None]
                if band:
                    ax.errorbar([b[0] for b in band], [b[1] for b in band], yerr=[[max(0, b[1] - b[2]) for b in band], [max(0, b[3] - b[1]) for b in band]],
                                fmt='none', ecolor=COLORS[k], elinewidth=.6, capsize=1.3, capthick=.6, alpha=alpha, zorder=2)
        ax.set_ylim(0, ymax); ax.set_ylabel(lab, labelpad=1.5)
        from matplotlib.ticker import MaxNLocator
        ax.yaxis.set_major_locator(MaxNLocator(nbins=4, steps=[1, 2, 2.5, 5, 10], integer=True))
        for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
        ax.grid(True, lw=.3, alpha=.35); ax.set_axisbelow(True); ax.tick_params(length=2, pad=1.5)
    from matplotlib.ticker import MultipleLocator
    for ax in axs: ax.set_xlim(a.xmin, xmax * 1.04); ax.xaxis.set_major_locator(MultipleLocator(a.xstep or (5 if xmax <= 25 else 10)))
    fig.supxlabel('achieved (k req/s)', fontsize=FS, y=.01)
    handles = [Line2D([], [], color=COLORS[k], lw=1.2, ls=':' if k == 'nt' else '-', label=LABELS[k]) for k in ORDER]
    fig.legend(handles=handles, loc='upper center', bbox_to_anchor=(.54, 1.0), ncol=5, frameon=False, handlelength=1.2, handletextpad=.3,
               columnspacing=.8, borderaxespad=0, borderpad=.1)
    os.makedirs(os.path.dirname(a.out), exist_ok=True)
    for ext in ('pdf', 'png', 'svg'): fig.savefig(f'{a.out}.{ext}', dpi=300)
    record_exclusions(a.out, dirs)
    print('wrote', a.out, '| excluded:', EXCLUDE or 'none')

if __name__ == '__main__':
    main()
