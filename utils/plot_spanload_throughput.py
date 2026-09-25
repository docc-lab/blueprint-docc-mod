#!/usr/bin/env python3
"""Tomislav-RetCtx: collector-only span throughput from a spanload dense-ramp root.

Regenerates the two-panel offered-vs-exported figure from averaged-points.json.

Why this exists: the suite's own figure sets `svg.fonttype='none'`, so its SVG references
"DejaVu Sans" by NAME instead of embedding outlines. Any viewer without that font
substitutes one -- usually a serif -- which is why the figure reads as serif even though
the PDF embeds DejaVu Sans correctly. Here the default (`path`) is kept, so glyphs are
outlines and the figure renders identically everywhere.

Geometry matches the DSB figures: drawn at final on-page size, one column of a two-column
CS conference paper, so the point sizes below are the point sizes the reader sees.
"""
import argparse
import json
from collections import defaultdict
from pathlib import Path

COLORS = {'none': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABELS = {'none': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
ORDER = ('none', 'pb', 'cgpb', 'sb')
PANELS = (('zero', 'No semconv'), ('semconv10-example', '10 semconv'))


def load(root):
    points = json.loads((root / 'averaged-points.json').read_text())
    out = defaultdict(list)
    for p in points:
        out[(p['profile'], p['variant'])].append(p)
    for key in out:
        out[key].sort(key=lambda p: p['offered_spans_per_second'])
    return out


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True, help='spanload dense-ramp root')
    parser.add_argument('--figures', type=Path, required=True)
    parser.add_argument('--name', default='collector-throughput')
    parser.add_argument('--width', type=float, default=3.33,
                        help='inches; 3.33 = one column of a two-column CS conference paper')
    parser.add_argument('--height', type=float, default=1.65)
    parser.add_argument('--fontsize', type=float, default=8)
    # Tomislav-RetCtx (2026-09-25): a squished figure needs the legend in two columns so it stays below the plateaus
    parser.add_argument('--legend-ncol', type=int, default=1)
    parser.add_argument('--legend-panel', type=int, choices=(0, 1), default=0, help='panel that holds the legend')
    parser.add_argument('--ylabel-break', action='store_true', help='y label on two lines (short figures)')
    parser.add_argument('--xsteps', type=float, nargs=2, default=None, help='x tick step per panel (k spans/s), e.g. 500 50')
    parser.add_argument('--band', choices=('minmax', 'sd'), default='minmax',
                        help='shaded spread: min-to-max over repetitions (default; n=3 makes a '
                             'sample SD a weak statistic) or +/-1 SD')
    args = parser.parse_args()
    root = args.out.resolve()
    args.figures.mkdir(parents=True, exist_ok=True)
    data = load(root)

    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    fs = args.fontsize
    # font.family sans-serif and svg.fonttype 'path' (the default) are the two settings that
    # keep this figure from being re-rendered in whatever font the viewer happens to pick.
    plt.rcParams.update({'font.family': 'sans-serif', 'font.sans-serif': ['DejaVu Sans'],
                         'svg.fonttype': 'path', 'pdf.fonttype': 42,
                         'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                         'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1,
                         'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                         'xtick.major.size': 2.5, 'ytick.major.size': 2.5})
    fig, axes = plt.subplots(1, 2, figsize=(args.width, args.height))
    reps = set()
    for axis, (profile, title) in zip(axes, PANELS):
        for variant in ORDER:
            pts = data.get((profile, variant))
            if not pts:
                continue
            reps.update(p['repetitions'] for p in pts)
            x = [p['offered_spans_per_second'] / 1000 for p in pts]
            y = [p['mean_exported_spans_per_second'] / 1000 for p in pts]
            if args.band == 'minmax':
                lo = [p['minimum_exported_spans_per_second'] / 1000 for p in pts]
                hi = [p['maximum_exported_spans_per_second'] / 1000 for p in pts]
            else:
                lo = [(p['mean_exported_spans_per_second'] - p['sd_exported_spans_per_second']) / 1000 for p in pts]
                hi = [(p['mean_exported_spans_per_second'] + p['sd_exported_spans_per_second']) / 1000 for p in pts]
            axis.fill_between(x, lo, hi, color=COLORS[variant], alpha=.18, linewidth=0)
            axis.plot(x, y, color=COLORS[variant], marker='o', markersize=1.6, linewidth=.9,
                      label=LABELS[variant])
        axis.set_title(title, fontsize=fs, pad=2)
        axis.set_ylim(bottom=0)
        axis.set_xlim(left=0)
        axis.grid(alpha=.2)
        axis.spines[['right', 'top']].set_visible(False)
    if args.xsteps:
        from matplotlib.ticker import MultipleLocator
        for axis, step in zip(axes, args.xsteps):
            axis.xaxis.set_major_locator(MultipleLocator(step))
    axes[0].set_ylabel('Throughput\n(k spans/s)' if args.ylabel_break else 'Throughput (k spans/s)')

    # Legend inside the left panel, lower right: both curves plateau early, so that corner
    # is empty in each panel and the panel titles keep the top strip to themselves.
    axes[args.legend_panel].legend(loc='lower right', frameon=False, handlelength=1.0, handletextpad=.4,
                   labelspacing=.22, borderpad=.1, borderaxespad=.3, ncol=args.legend_ncol, columnspacing=.8)
    band = (fs + 3) / 72 / args.height
    fig.tight_layout(pad=.35, w_pad=.9, rect=(0, band, 1, 1))
    mid = (axes[0].get_position().x0 + axes[1].get_position().x1) / 2
    fig.text(mid, .012, 'Offered rate (k spans/s)', ha='center', va='bottom', fontsize=fs)
    for extension in ('pdf', 'svg', 'png'):
        fig.savefig(args.figures / f'{args.name}.{extension}', dpi=300)
    plt.close(fig)
    (args.figures / f'{args.name}.provenance.json').write_text(json.dumps({
        'source_root': str(root), 'repetitions': sorted(reps),
        'panels': {title: {'profile': profile,
                           'rates': [p['offered_spans_per_second'] for p in data[(profile, 'none')]]}
                   for profile, title in PANELS},
        'variants': [LABELS[v] for v in ORDER],
        'band': ('min to max over repetitions' if args.band == 'minmax' else '+/-1 SD'),
        'figure_inches': [args.width, args.height], 'fonts_points': {'labels': fs, 'ticks': fs - 1},
        'svg_text': 'outlines (svg.fonttype=path), so no font substitution in viewers',
    }, indent=1))
    for profile, title in PANELS:
        for variant in ORDER:
            pts = data.get((profile, variant))
            if pts:
                peak = max(p['mean_exported_spans_per_second'] for p in pts)
                print(f'{title:12} {LABELS[variant]:8} plateau {peak/1000:7.1f} k spans/s')


if __name__ == '__main__':
    main()
