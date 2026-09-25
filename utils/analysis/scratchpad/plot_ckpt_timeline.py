#!/usr/bin/env python3
"""Tomislav-RetCtx: cumulative loss of trace-critical spans over a 300 s stationary point,
fleet-wide, bursty (left) vs fixed-rate (right). Bridges lines: checkpoints refused
(hp_refused / (hp_admitted + hp_refused)), from the priority processor's per-second counters;
solid = response path on, dashed = off. Vanilla (black): all spans refused, since any lost span
breaks a vanilla trace; series from the memory_limiter refuse/resume transitions in the collector
logs, weighted by the offered rate and anchored to per-collector refused totals (see README).
The y axis is broken: 0-6 % holds the bridges, 18-25 % the vanilla end point."""
import json, argparse
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D

S = Path('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad')
COLOR = {'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B', 'v': '#1A1A1A'}
LABEL = {'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
MARKER = {'pb': 'o', 'cgpb': 's', 'sb': '^'}

ap = argparse.ArgumentParser()
ap.add_argument('--figures', type=Path, default=Path('/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-22'))
ap.add_argument('--name', default='checkpoint-loss-timeline')
ap.add_argument('--width', type=float, default=3.33)
ap.add_argument('--height', type=float, default=2.6)
ap.add_argument('--fontsize', type=float, default=7)
a = ap.parse_args()
fs = a.fontsize
data = json.load(open(S / 'ckpt_timeline.json'))
plt.rcParams.update({'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                     'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1, 'font.family': 'sans-serif',
                     'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6})
LO, HI = (0, 6.3), (18, 25)
fig = plt.figure(figsize=(a.width, a.height))
gs = fig.add_gridspec(2, 2, height_ratios=[HI[1] - HI[0], (LO[1] - LO[0]) * 1.45], hspace=0.10, wspace=0.34,
                      left=0.13, right=0.905, top=0.80, bottom=0.14)
cols = {}
for j, (env, title) in enumerate((('bursty', 'Bursty, mean 5.6k req/s'), ('fixed', 'Fixed 5.5k req/s'))):
    top = fig.add_subplot(gs[0, j]); bot = fig.add_subplot(gs[1, j], sharex=top)
    cols[env] = (top, bot)
    ends_lo, ends_hi = [], []
    for kind in ('pb', 'cgpb', 'sb'):
        for arm, ls in (('on', '-'), ('off', '--')):
            k = f'{env}|{kind}|{arm}'
            if k not in data: continue
            d = data[k]
            for ax in (top, bot):
                # CGPB-SB is only dE 6.7 under protanopia: a marker per bridge carries identity alongside hue.
                ax.plot(d['t'], d['cum_pct'], color=COLOR[kind], ls=ls, lw=1.0,
                        marker=MARKER[kind], markevery=(25 + 10 * list(LABEL).index(kind), 50), ms=3.0,
                        markerfacecolor=COLOR[kind], markeredgecolor='white', markeredgewidth=.5)
            ends_lo.append((d['cum_pct'][-1], f"{d['final_pct']:.1f}"))
    dv = data.get(f'{env}|v|-')
    if dv:
        for ax in (top, bot):
            ax.plot(dv['t'], dv['cum_pct'], color=COLOR['v'], lw=1.4, ls='-', solid_capstyle='round')
        ends_hi.append((dv['cum_pct'][-1], f"{dv['final_pct']:.1f}"))
    # direct end labels, nudged apart (data units)
    for ax, ends, gap in ((bot, ends_lo, 0.34), (top, ends_hi, 0.8)):
        ends.sort(); ys = [y for y, _ in ends]
        for i in range(1, len(ys)):
            if ys[i] - ys[i - 1] < gap: ys[i] = ys[i - 1] + gap
        for (y0, txt), y in zip(ends, ys):
            if abs(y - y0) < 1e-9:
                ax.annotate(txt, xy=(300, y0), xytext=(2, 0), textcoords='offset points', fontsize=fs - 2,
                            color='#333333', va='center', ha='left', annotation_clip=False)
            else:
                ax.annotate(txt, xy=(300, y0), xytext=(305, y), textcoords='data', fontsize=fs - 2, color='#333333',
                            va='center', ha='left', annotation_clip=False,
                            arrowprops=dict(arrowstyle='-', lw=.4, color='#999999', shrinkA=0, shrinkB=0))
    top.set_ylim(*HI); bot.set_ylim(*LO)
    top.set_yticks([18, 21, 24]); bot.set_yticks([0, 2, 4, 6])
    top.set_title(title, fontsize=fs, pad=2)
    for ax in (top, bot):
        ax.set_xlim(0, 300); ax.grid(True, lw=.3, alpha=.4); ax.tick_params(length=2, pad=1.5)
        ax.spines['right'].set_visible(False)
    top.spines['top'].set_visible(False); top.spines['bottom'].set_visible(False)
    bot.spines['top'].set_visible(False)
    top.tick_params(axis='x', bottom=False, labelbottom=False)
    bot.set_xticks([0, 100, 200, 300]); bot.set_xlabel('Time in point (s)', fontsize=fs, labelpad=1)
    # axis-break marks on the left spine
    kw = dict(marker=[(-1, -.6), (1, .6)], markersize=5, linestyle='none', color='k', mec='k', mew=.6, clip_on=False)
    top.plot([0], [0], transform=top.transAxes, **kw); bot.plot([0], [1], transform=bot.transAxes, **kw)
    if j == 1:
        for ax in (top, bot): ax.tick_params(axis='y', labelleft=False)
fig.text(0.015, 0.47, 'Trace-critical spans refused (%, cumulative)', rotation=90, va='center', ha='left', fontsize=fs)
handles = [Line2D([], [], color=COLOR['v'], lw=1.4, label='Vanilla: all spans'),
           Line2D([], [], color='none', label='Bridges: checkpoints')]
for kind in ('pb', 'cgpb', 'sb'):
    for arm, ls in (('on', '-'), ('off', '--')):
        handles.append(Line2D([], [], color=COLOR[kind], ls=ls, lw=1.0, marker=MARKER[kind], ms=3.0,
                              markerfacecolor=COLOR[kind], markeredgecolor='white', markeredgewidth=.5,
                              label=f'{LABEL[kind]} {arm}'))
fig.legend(handles=handles, loc='upper center', bbox_to_anchor=(0.52, 1.0), ncol=4, frameon=False,
           handlelength=1.7, columnspacing=1.0, handletextpad=.4, labelspacing=.2, borderaxespad=0, fontsize=fs - 1)
a.figures.mkdir(parents=True, exist_ok=True)
for ext in ('pdf', 'png', 'svg'):
    fig.savefig(a.figures / f'{a.name}.{ext}', dpi=300)
print(f'wrote {a.figures}/{a.name}.{{pdf,png,svg}}')
