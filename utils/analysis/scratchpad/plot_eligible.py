#!/usr/bin/env python3
"""Tomislav-RetCtx: reconstruction-eligible traces across the ramp.

Vanilla keeps a trace only if every span survived -- it has no recovery path. A
bridge keeps one if every window anchor named by a surviving payload is still
present, a checkpoint restored by a returned truss counting as present. Both are
read straight off the wire: no reconstruction is run and no ground truth is used.

Bands are min-max over the five rounds; the line is the mean.
"""
import argparse, json, statistics
from collections import defaultdict
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

S = Path('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad')
COLOR = {'v': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABEL = {'v': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--figures', type=Path, required=True)
    ap.add_argument('--name', default='reconstruction-eligible-n5')
    ap.add_argument('--width', type=float, default=3.33)
    ap.add_argument('--height', type=float, default=2.1)
    ap.add_argument('--fontsize', type=float, default=8)
    args = ap.parse_args()

    raw = json.load(open(S / 'eligible.json'))
    series = defaultdict(dict)
    for key, vals in raw.items():
        kind, arm, rate = key.split('|')
        series[(kind, arm)][int(rate)] = vals

    fs = args.fontsize
    plt.rcParams.update({'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                         'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1,
                         'font.family': 'sans-serif', 'axes.linewidth': .6,
                         'xtick.major.width': .6, 'ytick.major.width': .6})
    fig, ax = plt.subplots(figsize=(args.width, args.height))

    for key in [('v', ''), ('pb', 'off'), ('pb', 'on'), ('cgpb', 'off'),
                ('cgpb', 'on'), ('sb', 'off'), ('sb', 'on')]:
        if key not in series:
            continue
        kind, arm = key
        rates = sorted(series[key])
        x = [r / 1000 for r in rates]
        mean = [statistics.mean(series[key][r]) for r in rates]
        lo = [min(series[key][r]) for r in rates]
        hi = [max(series[key][r]) for r in rates]
        label = LABEL[kind] + ('' if not arm else f' {arm}')
        ax.plot(x, mean, label=label, color=COLOR[kind], lw=1.0,
                ls='--' if arm == 'off' else '-')
        ax.fill_between(x, lo, hi, color=COLOR[kind], alpha=.12, lw=0)

    ax.set_xlabel('Offered load (k req/s)', fontsize=fs)
    ax.set_ylabel('Reconstruction-eligible (%)', fontsize=fs)
    ax.set_ylim(0, 103)
    ax.set_yticks([0, 25, 50, 75, 100])
    ax.grid(True, lw=.3, alpha=.4)
    ax.tick_params(length=2, pad=1.5)
    ax.legend(loc='lower left', ncol=2, frameon=False, handlelength=1.1,
              columnspacing=.6, handletextpad=.3, labelspacing=.25,
              borderpad=0, borderaxespad=.3, fontsize=fs - 2)
    fig.tight_layout(pad=.3)
    args.figures.mkdir(parents=True, exist_ok=True)
    for ext in ('pdf', 'png', 'svg'):
        fig.savefig(args.figures / f'{args.name}.{ext}', dpi=300)
    print(f'wrote {args.figures}/{args.name}.{{pdf,png,svg}}')


if __name__ == '__main__':
    main()
