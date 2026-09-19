#!/usr/bin/env python3
"""Tomislav-RetCtx: response time against ACHIEVED throughput (load-latency curves).

Each curve ends at that variant's capacity, so the saturation point is read off the
x-axis directly, and variants are compared at equal delivered work rather than at
equal offered load (at a common offered rate the variants deliver very different
throughput, so their latencies are not comparable points).

Past saturation throughput falls while latency climbs, so the curve doubles back.
The rising branch is drawn solid; the post-saturation points are drawn faint rather
than discarded, so nothing is hidden. --no-tail omits them entirely.

Latencies are coordinated-omission-corrected from an open-loop generator: past
saturation they include queueing at the load generator and grow without bound, so
the vertical part of each curve is real but its height is not a service-quality
measurement.
"""
import argparse
import json
import statistics
from collections import defaultdict
from pathlib import Path

COLORS = {'nt': '#7A7A7A', 'v': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABELS = {'nt': 'None', 'v': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
ORDER = ('nt', 'v', 'pb', 'cgpb', 'sb')


def curves(root, extra=(), repetitions=None):
    """Per kind, per offered rate: mean and sample SD over repetitions.

    extra is a list of "ROOT:KIND" overrides: that kind is taken from that other root and
    REPLACES any same-kind rows in the primary root. The real-work campaign needs this
    because the rebuilt S-Bridge was measured in its own root while the primary root still
    holds the superseded one."""
    rows = json.loads((root / 'analysis' / 'points.json').read_text())
    for spec in extra:
        other, kind = spec.rsplit(':', 1)
        rows = [r for r in rows if r['kind'] != kind]
        rows += [r for r in json.loads((Path(other) / 'analysis' / 'points.json').read_text())
                 if r['kind'] == kind]
    if repetitions:
        # Tomislav-RetCtx: plot a subset of the runs (n=1 while a campaign is still
        # measuring its later repetitions). The band then collapses onto the line.
        # Applied AFTER the extras merge so a borrowed baseline is filtered the same way
        # and every curve in the figure carries the same number of runs.
        rows = [r for r in rows if r.get('repetition', 1) in repetitions]
        assert rows, ('no points for repetitions', repetitions)
    grouped = defaultdict(list)
    for r in rows:
        grouped[(r['kind'], r['offered_rps'])].append(r)
    out = defaultdict(list)
    for (kind, rate), pts in sorted(grouped.items(), key=lambda kv: kv[0][1]):
        def stat(name):
            vals = [p[name] for p in pts]
            return statistics.mean(vals), (statistics.stdev(vals) if len(vals) > 1 else 0.)
        item = {'offered_rps': rate, 'repetitions': len(pts)}
        for name in ('completed_rps', 'mean_ms', 'p99_ms'):
            item[name], item[name + '_sd'] = stat(name)
            # n=3 makes a sample SD a weak statistic, so the figure shows the actual
            # envelope of the runs instead: min and max across repetitions.
            item[name + '_min'] = min(p[name] for p in pts)
            item[name + '_max'] = max(p[name] for p in pts)
        item['runs'] = {name: [p[name] for p in pts] for name in ('completed_rps', 'mean_ms', 'p99_ms')}
        out[kind].append(item)
    return out


def split_at_saturation(points):
    """Rising branch = points whose throughput is still at/above the best seen so far
    (0.5 % tolerance for noise), PLUS the first point after it.

    That extra point matters: it is the one that shoots vertically as latency explodes at
    constant throughput. Without it, whether a curve shows its collapse depends on whether
    the next measurement happens to land inside the tolerance, so some variants got a
    vertical segment and others just stopped. Including one always makes the comparison fair.
    """
    rising, best = [], 0.
    for i, p in enumerate(points):
        best = max(best, p['completed_rps'])
        if p['completed_rps'] >= 0.995 * best:
            rising.append(i)
    cut = min(rising[-1] + 2, len(points)) if rising else len(points)
    return points[:cut], points[cut:]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True, help='experiment root (reads analysis/points.json)')
    parser.add_argument('--figures', type=Path, required=True, help='directory to write the figure into')
    parser.add_argument('--name', default='latency-vs-throughput')
    parser.add_argument('--xtick', type=float, default=2,
                        help='x tick spacing in k req/s. 2 suits the no-work ramp (0..12k); the '
                             'real-work ramp only spans ~2..3.5k and needs something like 0.5.')
    parser.add_argument('--repetition', type=int, action='append', default=[], metavar='N',
                        help='only use these repetitions (repeatable). Default: all of them.')
    parser.add_argument('--extra', action='append', default=[], metavar='ROOT:KIND',
                        help='take KIND from another root, replacing that kind in the primary root')
    parser.add_argument('--no-tail', action='store_true', help='omit the post-saturation branch entirely')
    parser.add_argument('--ylim', default='300,600',
                        help='y-axis ceiling in ms for the mean and p99 panels, comma separated. '
                             'Linear axes: the knee is the point of the figure, so the axis is bounded '
                             'and curves run off the top rather than being compressed by a log scale. '
                             '"none" for auto-scale.')
    parser.add_argument('--logy', action='store_true', help='logarithmic y (compresses the knee; not recommended)')
    parser.add_argument('--width', type=float, default=3.33,
                        help='figure width in inches. Default 3.33 = one column of a two-column '
                             'CS conference paper (ACM sigconf; IEEE is 3.5). Figures are drawn at '
                             'final size so the point sizes below are the point sizes on the page.')
    parser.add_argument('--height', type=float, default=1.65)
    parser.add_argument('--fontsize', type=float, default=8,
                        help='axis-label point size on the page (ACM body text is 9 pt; 8 sits just '
                             'under it, which reads fine at column width). Ticks and legend are one '
                             'point smaller.')
    parser.add_argument('--spaghetti', action='store_true',
                        help='also draw each repetition as a thin line inside the band')
    args = parser.parse_args()
    root = args.out.resolve()
    args.figures.mkdir(parents=True, exist_ok=True)
    data = curves(root, args.extra, args.repetition)

    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    # Tomislav-RetCtx: drawn at final on-page size (one column wide), so these are the
    # point sizes the reader sees. Do NOT draw large and scale down. Default 9 pt matches
    # ACM body text, so figure type reads at the same size as the prose around it.
    fs = args.fontsize
    plt.rcParams.update({'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                         'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1, 'pdf.fonttype': 42,
                         'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                         'xtick.major.size': 2.5, 'ytick.major.size': 2.5})
    fig, axes = plt.subplots(1, 2, figsize=(args.width, args.height))
    ceilings = [None, None] if args.ylim == 'none' else [float(v) for v in args.ylim.split(',')]
    summary = {}
    for axis, ceiling, (field, label) in zip(axes, ceilings, (('mean_ms', 'Mean resp. (ms)'),
                                                              ('p99_ms', 'p99 resp. (ms)'))):
        for kind in ORDER:
            points = data.get(kind)
            if not points:
                continue
            rising, tail = split_at_saturation(points)
            x = [p['completed_rps'] / 1000 for p in rising]
            # Throughput varies 0.09 % across repetitions (2.7 % worst case), so an x error
            # bar draws nothing; the run-to-run spread that matters is all in latency.
            axis.fill_between(x, [p[field + '_min'] for p in rising], [p[field + '_max'] for p in rising],
                              color=COLORS[kind], alpha=.18, linewidth=0)
            if args.spaghetti:
                for i in range(max(len(p['runs'][field]) for p in rising)):
                    ys = [p['runs'][field][i] if i < len(p['runs'][field]) else None for p in rising]
                    axis.plot([xx for xx, yy in zip(x, ys) if yy is not None],
                              [yy for yy in ys if yy is not None],
                              color=COLORS[kind], linewidth=.4, alpha=.45)
            axis.plot(x, [p[field] for p in rising], color=COLORS[kind], marker='o', markersize=1.6,
                      linewidth=.9, label=LABELS[kind])
            if tail and not args.no_tail:
                axis.plot([p['completed_rps'] / 1000 for p in tail], [p[field] for p in tail],
                          color=COLORS[kind], marker='o', markersize=1.2, linewidth=.6, alpha=.25)
            summary[kind] = {'capacity_rps': max(p['completed_rps'] for p in points),
                             'rising_points': len(rising), 'tail_points': len(tail),
                             'total_points': len(points)}
        # One shared x label: repeating it under each panel collides at column width.
        axis.set_ylabel(label)
        if args.logy:
            axis.set_yscale('log')
        elif ceiling:
            axis.set_ylim(0, ceiling)
        # 2k ticks: the interesting spread between variants is 6k-12k, and 5k steps put
        # only two labelled gridlines across it.
        axis.xaxis.set_major_locator(matplotlib.ticker.MultipleLocator(args.xtick))
        axis.grid(alpha=.2)
        axis.spines[['right', 'top']].set_visible(False)
    # Legend above the panels in one row. Kept vertically tight: the band is just the text
    # height and the handle/column spacing is squeezed so five entries fit one column width.
    # Label text stays black; colour lives in the line sample, as usual.
    handles, labels = axes[0].get_legend_handles_labels()
    # One shared x label. At 9 pt the text is wider than a single ~1.3 in panel, so a
    # per-panel label overflows the figure edge. Reserve bands just tall enough for the
    # text (larger bands leave visible whitespace) and centre both across the panels.
    band = (fs + 3) / 72 / args.height
    top = (fs + 1) / 72 / args.height
    fig.tight_layout(pad=.35, w_pad=.9, rect=(0, band, 1, 1 - top))
    mid = (axes[0].get_position().x0 + axes[1].get_position().x1) / 2
    fig.text(mid, .012, 'Achieved throughput (k req/s)', ha='center', va='bottom', fontsize=fs)
    # Centred on the figure (x=.5), not on the axes span, which sits right of centre
    # because of the y-label gutter. Vertically anchored to the axes top: slack between
    # the axes and the figure top would otherwise show as a gap under the legend.
    fig.legend(handles, labels, loc='lower center',
               bbox_to_anchor=(.5, max(ax.get_position().y1 for ax in axes) + .022), ncol=len(labels),
               frameon=False, handlelength=1.0, handletextpad=.4, columnspacing=.8,
               borderpad=0, borderaxespad=.1, fontsize=fs - 1)
    for extension in ('pdf', 'svg', 'png'):
        fig.savefig(args.figures / f'{args.name}.{extension}', dpi=300)
    plt.close(fig)
    (args.figures / f'{args.name}.summary.json').write_text(json.dumps(
        {'root': str(root), 'kinds': summary,
         'y_axis': ('log' if False else 'linear'), 'y_ceiling_ms': None,
         'repetitions': sorted({p['repetitions'] for pts in data.values() for p in pts}),
         'repetition_filter': args.repetition or 'all',
         'spread': 'shaded band = min to max over repetitions (a sample SD would be a weak statistic '
                   'at n=3, and there is no band at all at n=1). Throughput spread is 0.09 % median / '
                   '2.7 % worst, so no x spread is drawn.',
         'branch_rule': 'rising branch = throughput still within 0.5 % of the best seen so far; '
                        'post-saturation points drawn faint',
         'note': 'coordinated-omission-corrected latency from an open-loop generator; past saturation '
                 'it includes generator queueing and is not a service-quality measurement'}, indent=1))
    for kind, s in summary.items():
        print(f"{LABELS[kind]:11} capacity {s['capacity_rps']:7.0f} rps | rising {s['rising_points']}/{s['total_points']} points")


if __name__ == '__main__':
    main()
