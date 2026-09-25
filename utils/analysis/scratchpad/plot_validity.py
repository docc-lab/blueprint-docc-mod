#!/usr/bin/env python3
"""Tomislav-RetCtx: usable traces across the ramp.

Left panel: fraction of captured traces still usable, by each mode's own rule --
vanilla needs every span; a bridge needs no lost checkpoint and every severed
fragment reattached to its true nearest surviving ancestor (pb0 / cgp0 / sb3 on
the measured payloads). Right panel: the same against achieved throughput, so
each curve ends at its own capacity.

Bands are min-max across the five rounds; the line is the mean.
"""
import argparse, json, glob, statistics
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
    ap.add_argument('--name', default='trace-validity')
    ap.add_argument('--normalize', action='store_true',
                    help='left panel x = offered load / that configuration\'s own peak throughput')
    ap.add_argument('--width', type=float, default=3.33)
    ap.add_argument('--height', type=float, default=2.25)
    ap.add_argument('--fontsize', type=float, default=8)
    args = ap.parse_args()

    rows = []
    for f in glob.glob(str(S / 'validity' / '*.json')):
        rows += json.load(open(f))
    # Each configuration saturates at a different rate, so comparing at a common
    # offered rate compares systems at different points of their own envelopes.
    # peak[key] is that configuration's own peak completed throughput, per round.
    peak = defaultdict(dict)
    achieved = {}
    for root in {r['root'] for r in rows}:
        try:
            pts = json.load(open(Path(root) / 'analysis' / 'points.json'))
        except OSError:
            continue
        best = defaultdict(float)
        for p in pts:
            achieved[(root, p['kind'], p['offered_rps'])] = p['completed_rps']
            best[p['kind']] = max(best[p['kind']], p['completed_rps'])
        for kind, value in best.items():
            peak[root][kind] = value

    # series key -> rate -> list of (valid_pct, achieved)
    series = defaultdict(lambda: defaultdict(list))
    for r in rows:
        if r['kind'] == 'nt' or not r['scored']:
            continue
        key = (r['kind'], '' if r['kind'] == 'v' else r['reverse'])
        pct = r['intact_pct'] if r['kind'] == 'v' else r['valid_pct']
        a = achieved.get((r['root'], r['kind'], r['rate']))
        rel = r['rate'] / peak[r['root']][r['kind']] if peak.get(r['root'], {}).get(r['kind']) else None
        series[key][r['rate']].append((pct, a, rel))

    fs = args.fontsize
    plt.rcParams.update({'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                         'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1,
                         'font.family': 'sans-serif', 'axes.linewidth': .6,
                         'xtick.major.width': .6, 'ytick.major.width': .6})
    fig, axes = plt.subplots(1, 2, figsize=(args.width, args.height))

    order = [('v', ''), ('pb', 'off'), ('pb', 'on'), ('cgpb', 'off'),
             ('cgpb', 'on'), ('sb', 'off'), ('sb', 'on')]
    for key in order:
        if key not in series:
            continue
        kind, rev = key
        rates = sorted(series[key])
        mean = [statistics.mean(v for v, _, _ in series[key][r]) for r in rates]
        lo = [min(v for v, _, _ in series[key][r]) for r in rates]
        hi = [max(v for v, _, _ in series[key][r]) for r in rates]
        ach = [statistics.mean(a for _, a, _ in series[key][r] if a) / 1000
               if any(a for _, a, _ in series[key][r]) else None for r in rates]
        rel = [statistics.mean(x for _, _, x in series[key][r] if x)
               if any(x for _, _, x in series[key][r]) else None for r in rates]
        label = LABEL[kind] + ('' if not rev else f' {rev}')
        style = dict(color=COLOR[kind], lw=1.0,
                     ls='--' if rev == 'off' else '-')
        x0 = [r / 1000 for r in rates] if not args.normalize else rel
        pairs = [(x, m, l, h) for x, m, l, h in zip(x0, mean, lo, hi) if x is not None]
        axes[0].plot([p[0] for p in pairs], [p[1] for p in pairs], label=label, **style)
        axes[0].fill_between([p[0] for p in pairs], [p[2] for p in pairs],
                             [p[3] for p in pairs], color=COLOR[kind], alpha=.12, lw=0)
        # Achieved throughput doubles back past saturation, so the right panel
        # draws only the rising branch: each curve then ends at its own capacity
        # and the x position of its knee is read directly.
        pts = [(a, m) for a, m in zip(ach, mean) if a is not None]
        rising, peak = [], -1.0
        for a, m in pts:
            if a < peak:
                break
            peak = a
            rising.append((a, m))
        if rising:
            axes[1].plot([a for a, _ in rising], [m for _, m in rising], **style)

    for ax, xl in ((axes[0], 'Load / own peak' if args.normalize else 'Offered (k req/s)'), (axes[1], 'Achieved (k req/s)')):
        ax.set_xlabel(xl, fontsize=fs - 1, labelpad=1)
        ax.set_ylim(0, 103)
        ax.set_yticks([0, 25, 50, 75, 100])
        ax.grid(True, lw=.3, alpha=.4)
        ax.tick_params(length=2, pad=1.5)
    axes[0].set_ylabel('Usable traces (%)', fontsize=fs)
    axes[1].set_yticklabels([])
    # Legend inside: the lower left of the achieved panel is empty for every
    # series, so nothing is hidden and no vertical space is spent on it.
    handles, labels = axes[0].get_legend_handles_labels()
    axes[1].legend(handles, labels, loc='lower left', ncol=2, frameon=False,
                   handlelength=1.1, columnspacing=.6, handletextpad=.3,
                   labelspacing=.25, borderpad=0, borderaxespad=.2,
                   fontsize=fs - 2)
    fig.tight_layout(pad=.3, w_pad=.6)
    args.figures.mkdir(parents=True, exist_ok=True)
    for ext in ('pdf', 'png', 'svg'):
        fig.savefig(args.figures / f'{args.name}.{ext}', dpi=300)
    print(f'wrote {args.figures}/{args.name}.{{pdf,png,svg}}')
    for key in order:
        if key in series:
            r = sorted(series[key])
            print(f'  {LABEL[key[0]]:8} {key[1]:4} rates {r[0]}-{r[-1]}  '
                  f'rounds {len(series[key][r[0]])}')


if __name__ == "__main__":
    main()
