#!/usr/bin/env python3
"""Tomislav-RetCtx: cumulative checkpoint loss over a 300 s stationary point, fleet-wide,
bursty (left) vs fixed-rate (right). Each line is one bridge; solid = response path on,
dashed = off. The data is the collectors' own per-second priority-processor counters
(hp_refused / (hp_admitted + hp_refused), cumulative from the point's start)."""
import json, argparse
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

S = Path('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad')
COLOR = {'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABEL = {'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
MARKER = {'pb': 'o', 'cgpb': 's', 'sb': '^'}

ap = argparse.ArgumentParser()
ap.add_argument('--figures', type=Path, default=Path('/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-22'))
ap.add_argument('--name', default='checkpoint-loss-timeline')
ap.add_argument('--width', type=float, default=3.33)
ap.add_argument('--height', type=float, default=2.05)
ap.add_argument('--fontsize', type=float, default=7)
a = ap.parse_args()
fs = a.fontsize
data = json.load(open(S / 'ckpt_timeline.json'))
plt.rcParams.update({'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                     'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1, 'font.family': 'sans-serif',
                     'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6})
fig, axes = plt.subplots(1, 2, figsize=(a.width, a.height), sharey=True)
ymax = 0
for ax, env, title in ((axes[0], 'bursty', 'Bursty, mean 5.6k'), (axes[1], 'fixed', 'Fixed 5.5k')):
    ends = []
    for kind in ('pb', 'cgpb', 'sb'):
        for arm, ls in (('on', '-'), ('off', '--')):
            k = f'{env}|{kind}|{arm}'
            if k not in data:
                continue
            d = data[k]
            # CGPB-SB is only dE 6.7 under protanopia, so a marker per bridge carries
            # identity alongside hue (sparse: every 50 s, small, surface-ringed).
            ax.plot(d['t'], d['cum_pct'], color=COLOR[kind], ls=ls, lw=1.1,
                    marker=MARKER[kind], markevery=(25 + 10 * list(COLOR).index(kind), 50), ms=3.2,
                    markerfacecolor=COLOR[kind], markeredgecolor='white', markeredgewidth=.5,
                    label=f'{LABEL[kind]} {arm}' if env == 'bursty' else None)
            ymax = max(ymax, max(d['cum_pct']))
            ends.append((d['cum_pct'][-1], f"{d['final_pct']:.1f}"))
    # direct end labels, nudged apart so none overlap (min gap in data units)
    ends.sort()
    gap = 0.32
    ys = [y for y, _ in ends]
    for i in range(1, len(ys)):
        if ys[i] - ys[i - 1] < gap:
            ys[i] = ys[i - 1] + gap
    for (y0, txt), y in zip(ends, ys):
        ax.annotate(txt, xy=(300, y0), xytext=(2, (y - y0) * 0), textcoords='offset points',
                    fontsize=fs - 2, color='#333333', va='center', ha='left',
                    xycoords='data', annotation_clip=False) if abs(y - y0) < 1e-9 else \
        ax.annotate(txt, xy=(300, y0), xytext=(303, y), textcoords='data', fontsize=fs - 2,
                    color='#333333', va='center', ha='left', annotation_clip=False,
                    arrowprops=dict(arrowstyle='-', lw=.4, color='#999999', shrinkA=0, shrinkB=0))
    ax.set_title(title, fontsize=fs, pad=2)
    ax.set_xlim(0, 300)
    ax.set_xticks([0, 100, 200, 300])
    ax.set_xlabel('Time in point (s)', fontsize=fs, labelpad=1)
    ax.grid(True, lw=.3, alpha=.4)
    ax.tick_params(length=2, pad=1.5)
    for sp in ('top', 'right'):
        ax.spines[sp].set_visible(False)
axes[0].set_ylabel('Checkpoints refused (%, cumulative)', fontsize=fs)
axes[0].set_ylim(0, ymax * 1.15)
# the curves are flat at zero for the first 2-4 minutes, so the upper-left is empty in both panels
axes[0].legend(loc='upper left', ncol=2, frameon=False, handlelength=1.6, columnspacing=.7,
               handletextpad=.4, labelspacing=.25, borderpad=0, borderaxespad=.3, fontsize=fs - 1)
fig.tight_layout(pad=.3, w_pad=1.2)
a.figures.mkdir(parents=True, exist_ok=True)
for ext in ('pdf', 'png', 'svg'):
    fig.savefig(a.figures / f'{a.name}.{ext}', dpi=300)
print(f'wrote {a.figures}/{a.name}.{{pdf,png,svg}}')
