#!/usr/bin/env python3
"""Tomislav-RetCtx (user 2026-09-25): several apps' n=5 ramps stacked into ONE figure: one row per app (delivered | mean |
p99), one legend at the top, one x-axis label at the bottom; each row keeps its own x range and latency caps. Data, pooling,
error bars and styling are plot_ramp_n1.py's (imported), so each row is identical to that app's single figure.
usage: plot_ramp_combined.py --config rows.json --out PREFIX
rows.json: [{"label": "Social Network", "nt": [ROOT..], "v": [ROOT..], "bridges": [ROOT..], "exclude": [..],
             "xmin": 1.9, "xstep": 0.5, "ymax_mean": 200, "ymax_p99": 700}, ...]"""
import argparse, json, os
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D
from matplotlib.ticker import MultipleLocator
import plot_ramp_n1 as base

ap = argparse.ArgumentParser(); ap.add_argument('--config', required=True); ap.add_argument('--out', required=True)
a = ap.parse_args(); rows = json.load(open(a.config))
W, H, FS = base.W, base.H, base.FS
plt.rcParams.update({'font.size': FS, 'axes.labelsize': FS, 'xtick.labelsize': FS, 'ytick.labelsize': FS, 'legend.fontsize': FS,
                     'font.family': 'sans-serif', 'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                     'pdf.fonttype': 42, 'svg.fonttype': 'none'})
# Tomislav-RetCtx (user 2026-09-25): the LaTeX caption names the rows; only a short tag (row 'short', e.g. SN / HR) after
# the first panel letter of each row, no row-label strip
top_in, bottom_in, gap_in, label_in = .2, .36, .30, 0
nrow = len(rows); fig_h = nrow * H + (nrow - 1) * gap_in + top_in + bottom_in
fig, axs = plt.subplots(nrow, 3, figsize=(3 * W + label_in, fig_h), squeeze=False)
fig.subplots_adjust(left=.075 + label_in / (3 * W + label_in), right=.99, top=1 - top_in / fig_h, bottom=bottom_in / fig_h,
                    wspace=.36, hspace=gap_in / H)
tags = iter('abcdefghijkl'); runs_txt = []
for r, row in enumerate(rows):
    base.EXCLUDE[:] = row.get('exclude', [])
    roots = {'nt': row['nt'], 'v': row['v'], 'pb': row['bridges'], 'cgpb': row['bridges'], 'sb': row['bridges']}
    dirs = {k: base.case_dirs(roots[k], k) for k in base.ORDER}
    data = {k: base.points(dirs[k]) for k in base.ORDER}
    runs_txt.append(f"== {row['label']} (excluded: {row.get('exclude') or 'none'})\n" + '\n'.join(f"{k}: {' '.join(dirs[k])}" for k in base.ORDER))
    xmax = max(p['offered_rps'] for k in base.ORDER for p in data[k]) / 1000
    ax_t, ax_m, ax_p = axs[r]
    for k in base.ORDER:
        xs = [p['offered_rps'] / 1000 for p in data[k]]
        kw = dict(color=base.COLORS[k], lw=1.1 if k != 'nt' else 1.0, ls=':' if k == 'nt' else '-', marker='o', ms=1.8, mew=0)
        ax_t.plot(xs, [p['completed_rps'] / 1000 for p in data[k]], **kw)
        ax_m.plot(xs, [p['mean_ms'] for p in data[k]], **kw)
        ax_p.plot(xs, [p['p99_ms'] for p in data[k]], **kw)
        for ax, stat, bkey in ((ax_m, 'mean_ms', '_mband'), (ax_p, 'p99_ms', '_band')):
            band = [(x, p[stat], p[bkey]) for x, p in zip(xs, data[k]) if p.get(bkey)]
            if band:
                ax.errorbar([b[0] for b in band], [b[1] for b in band],
                            yerr=[[max(0, b[1] - b[2][0]) for b in band], [max(0, b[2][1] - b[1]) for b in band]],
                            fmt='none', ecolor=base.COLORS[k], elinewidth=.6, capsize=1.3, capthick=.6, zorder=2)
    ax_t.plot([0, xmax], [0, xmax], color='#BBBBBB', lw=.6, ls='--', zorder=0)
    ax_t.set_ylabel('delivered (k req/s)'); ax_t.set_ylim(0, None)
    for ax, lab, top in ((ax_m, 'mean latency (ms)', row.get('ymax_mean', 60)), (ax_p, 'p99 latency (ms)', row.get('ymax_p99', 200))):
        ax.set_ylabel(lab); ax.set_ylim(0, top)
    xmin = row.get('xmin', 0)
    for c, ax in enumerate(axs[r]):
        ax.set_xlim(xmin, xmax * 1.02)
        if xmin and ax is ax_t: ax.set_ylim(xmin, None)
        if row.get('xstep'): ax.xaxis.set_major_locator(MultipleLocator(row['xstep']))
        for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
        ax.grid(True, lw=.3, alpha=.35); ax.set_axisbelow(True); ax.tick_params(length=2, pad=1.5)
        # the row tag in bold (mathtext bold, DejaVu Sans like the rest), the panel letter in regular weight
        tag = f'({next(tags)})' + (f" $\\mathbf{{{row['short']}}}$" if c == 0 and row.get('short') else '')
        ax.text(.03, .97, tag, transform=ax.transAxes, ha='left', va='top', fontsize=FS)
fig.supxlabel('offered load (k req/s)', fontsize=FS, y=.005)
handles = [Line2D([], [], color=base.COLORS[k], lw=1.2, ls=':' if k == 'nt' else '-', label=base.LABELS[k]) for k in base.ORDER]
fig.legend(handles=handles, loc='upper center', bbox_to_anchor=(.5, 1.0), ncol=5, frameon=False, handlelength=1.3,
           columnspacing=.9, handletextpad=.35, borderaxespad=.1)
os.makedirs(os.path.dirname(a.out), exist_ok=True)
for ext in ('pdf', 'png', 'svg'): fig.savefig(f'{a.out}.{ext}', dpi=300)
open(f'{a.out}.runs.txt', 'w').write('\n'.join(runs_txt) + '\n')
print('wrote', a.out)
