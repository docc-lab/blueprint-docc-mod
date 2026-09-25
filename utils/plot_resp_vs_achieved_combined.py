#!/usr/bin/env python3
"""Tomislav-RetCtx (user 2026-09-25): several apps' response-vs-achieved figures stacked into ONE figure: one row per app
(mean | p99), one legend at the top, one x-axis label at the bottom; each row keeps its own x range and latency caps. Pooling
and error bars are plot_resp_vs_achieved.py's (imported), so each row is identical to that app's single figure.
usage: plot_resp_vs_achieved_combined.py --config rows.json --out PREFIX   (rows.json as for plot_ramp_combined.py)"""
import argparse, json, os, random
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D
from matplotlib.ticker import MultipleLocator, MaxNLocator
import plot_ramp_n1 as base
from plot_resp_vs_achieved import pooled_point, W, H, FS, BOTTOM_IN, TOP_IN

ap = argparse.ArgumentParser(); ap.add_argument('--config', required=True); ap.add_argument('--out', required=True)
a = ap.parse_args(); rows = json.load(open(a.config))
plt.rcParams.update({'font.size': FS, 'axes.labelsize': FS, 'xtick.labelsize': FS, 'ytick.labelsize': FS, 'legend.fontsize': FS,
                     'font.family': 'sans-serif', 'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                     'pdf.fonttype': 42, 'svg.fonttype': 'none'})
GAP_IN, LABEL_IN, TITLE_IN = .22, 0, 0   # between rows (tick labels); the LaTeX caption names the rows (user 2026-09-25)
ax_h = H - TOP_IN - BOTTOM_IN        # the single figure's axes height, kept per row
nrow = len(rows); fig_h = nrow * (ax_h + TITLE_IN) + (nrow - 1) * GAP_IN + TOP_IN + BOTTOM_IN; fig_w = W + LABEL_IN
fig, axs = plt.subplots(nrow, 2, figsize=(fig_w, fig_h), squeeze=False)
left = (.12 * W + LABEL_IN) / fig_w; right = 1 - (1 - .985) * W / fig_w
fig.subplots_adjust(left=left, right=right, top=1 - (TOP_IN + TITLE_IN) / fig_h, bottom=BOTTOM_IN / fig_h, wspace=.46,
                    hspace=(GAP_IN + TITLE_IN) / ax_h)
runs_txt = []
for r, row in enumerate(rows):
    base.EXCLUDE[:] = row.get('exclude', [])
    roots = {'nt': row['nt'], 'v': row['v'], 'pb': row['bridges'], 'cgpb': row['bridges'], 'sb': row['bridges']}
    dirs = {k: base.case_dirs(roots[k], k) for k in base.ORDER}
    data = {k: base.points(dirs[k]) for k in base.ORDER}
    runs_txt.append(f"== {row['label']} (excluded: {row.get('exclude') or 'none'})\n" + '\n'.join(f"{k}: {' '.join(dirs[k])}" for k in base.ORDER))
    pooled = {k: [pooled_point(p, random.Random(7)) for p in data[k]] for k in base.ORDER}
    xmax = max(q[0] / 1000 for k in base.ORDER for q in pooled[k])
    for ax, key, ymax, lab in ((axs[r][0], 'mean_ms', row.get('ymax_mean', 60), 'mean (ms)'), (axs[r][1], 'p99_ms', row.get('ymax_p99', 200), 'p99 (ms)')):
        for k in base.ORDER:
            pts = pooled[k]; xs = [q[0] / 1000 for q in pts]; ys = [q[1] if key == 'mean_ms' else q[2] for q in pts]
            ax.plot(xs, ys, color=base.COLORS[k], lw=1.1, ls=':' if k == 'nt' else '-', marker='o', ms=1.6, mew=0, zorder=3)
            lo_i, hi_i = (6, 7) if key == 'mean_ms' else (3, 4)
            band = [(xs[i], ys[i], pts[i][lo_i], pts[i][hi_i]) for i in range(len(xs)) if pts[i][lo_i] is not None]
            if band:
                ax.errorbar([b[0] for b in band], [b[1] for b in band], yerr=[[max(0, b[1] - b[2]) for b in band], [max(0, b[3] - b[1]) for b in band]],
                            fmt='none', ecolor=base.COLORS[k], elinewidth=.6, capsize=1.3, capthick=.6, zorder=2)
        ax.set_ylim(0, ymax); ax.set_ylabel(lab, labelpad=1.5)
        ax.yaxis.set_major_locator(MaxNLocator(nbins=4, steps=[1, 2, 2.5, 5, 10], integer=True))
        for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
        ax.grid(True, lw=.3, alpha=.35); ax.set_axisbelow(True); ax.tick_params(length=2, pad=1.5)
        ax.set_xlim(row.get('xmin', 0), xmax * 1.04)
        ax.xaxis.set_major_locator(MultipleLocator(row.get('xstep') or (5 if xmax <= 25 else 10)))
    # short row tag (SN / HR) in the mean panel's empty upper-left corner
    for ax in axs[r]:  # both panels (user 2026-09-25: p99 too, for clarity)
        ax.text(.04, .96, row.get('short', row['label']), transform=ax.transAxes, ha='left', va='top', fontsize=FS, fontweight='bold')
fig.supxlabel('achieved (k req/s)', fontsize=FS, y=.01 * H / fig_h)
handles = [Line2D([], [], color=base.COLORS[k], lw=1.2, ls=':' if k == 'nt' else '-', label=base.LABELS[k]) for k in base.ORDER]
fig.legend(handles=handles, loc='upper center', bbox_to_anchor=(.54, 1.0), ncol=5, frameon=False, handlelength=1.2, handletextpad=.3,
           columnspacing=.8, borderaxespad=0, borderpad=.1)
os.makedirs(os.path.dirname(a.out), exist_ok=True)
for ext in ('pdf', 'png', 'svg'): fig.savefig(f'{a.out}.{ext}', dpi=300)
open(f'{a.out}.runs.txt', 'w').write('\n'.join(runs_txt) + '\n')
print('wrote', a.out)
