#!/usr/bin/env python3
"""Tomislav-RetCtx: the spanload dense-ramp 'throughput-combined' figure (No semconv | 10 semconv, one marker style per
variant) redrawn from the artifact's averaged-points.json at a chosen height, same width. The styling is copied from
analyze_spanload_suite.plot() in the artifact directory; margins are kept constant in INCHES (not figure fractions), so
a shorter figure squishes only the plot area. Writes <name>.{pdf,svg,png} next to the original; the artifact is untouched."""
import argparse, json
from pathlib import Path

PROFILES = ('zero', 'semconv10-example')
NAMES = {'none': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--root', type=Path, required=True, help='spanload dense-ramp artifact directory')
    ap.add_argument('--height', type=float, default=2.1, help='inches (original 2.1)')
    ap.add_argument('--width', type=float, default=4.4, help='inches (original 4.4)')
    ap.add_argument('--name', default='throughput-combined-squished')
    args = ap.parse_args()
    points = json.load(open(args.root / 'averaged-points.json'))

    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    import numpy as np
    styles = {'none': ('#333333', 'o', 2.6, '-'), 'pb': ('#0072B2', '^', 3.7, '-'),
              'cgpb': ('#D55E00', 's', 2.7, '--'), 'sb': ('#009E73', 'o', 1.7, ':')}
    fonts = {'font.size': 9, 'axes.titlesize': 10, 'axes.labelsize': 9,
             'xtick.labelsize': 8, 'ytick.labelsize': 8, 'legend.fontsize': 8}
    h, H0 = args.height, 2.1
    with plt.rc_context({**fonts, 'font.family': 'DejaVu Sans', 'axes.linewidth': .65,
        'xtick.major.width': .65, 'ytick.major.width': .65, 'xtick.major.size': 2.5, 'ytick.major.size': 2.5,
        'xtick.major.pad': 2, 'ytick.major.pad': 2, 'pdf.fonttype': 42, 'svg.fonttype': 'none', 'savefig.bbox': None}):
        fig, axes = plt.subplots(1, 2, figsize=(args.width, h), dpi=300)
        # original top margin (1 - .84 at 2.1 in) kept in inches; the bottom margin drops the removed footnote strip
        # (user 2026-09-25: no 'Mean of N runs' note, no SD shading) and keeps only the tick labels + x label (0.37 in)
        bottom, top = .37 / h, 1 - (1 - .84) * H0 / h
        # the one-line y label is ~1.5 in long; below ~1.9 in it no longer fits the axes height -> two lines, wider left margin
        two = h < 1.9
        fig.subplots_adjust(left=.165 if two else .125, right=.985, bottom=bottom, top=top, wspace=.22)
        ylabel = fig.supylabel('Throughput\n(k spans/s)' if two else 'Throughput (k spans/s)', x=.012, y=(bottom + top) / 2,
                               fontsize=9, linespacing=1.0)
        for ax, profile in zip(axes, PROFILES):
            hi = 0
            for variant, (color, marker, size, linestyle) in styles.items():
                series = [row for row in points if row['profile'] == profile and row['variant'] == variant]
                x = np.array([row['offered_spans_per_second'] / 1000 for row in series])
                mean = np.array([row['mean_exported_spans_per_second'] / 1000 for row in series])
                hi = max(hi, max(mean))
                ax.plot(x, mean, color=color, marker=marker, markersize=size, linewidth=1.05,
                        markerfacecolor=color if variant == 'none' else 'white', markeredgewidth=.8,
                        linestyle=linestyle, label=NAMES[variant])
            ax.set_title('No semconv' if profile == 'zero' else '10 semconv', pad=3)
            ax.set_xlabel('Offered rate (k spans/s)', labelpad=3)
            ax.set_xlim(0, 1700 if profile == 'zero' else 212)
            ax.set_xticks([0, 400, 800, 1200, 1600] if profile == 'zero' else [0, 50, 100, 150, 200])
            ax.set_ylim(0, max(hi * 1.10, 1080 if profile == 'zero' else 125))
            ax.set_yticks([0, 250, 500, 750, 1000] if profile == 'zero' else [0, 40, 80, 120])
            ax.grid(alpha=.20, linewidth=.5)
            ax.set_axisbelow(True)
        # one legend, in the right panel's empty lower-right corner (a framed legend in the left panel covers the bridges'
        # ~540k plateau once the figure is short); same entries and style as the original per-panel legends
        legend = axes[1].legend(loc='lower right', ncol=2, handlelength=.9, handletextpad=.3, columnspacing=.45,
                                borderpad=.25, labelspacing=.2, borderaxespad=.25, framealpha=.95, edgecolor='#cccccc', fancybox=False)
        legend.get_frame().set_linewidth(.5)
        fig.canvas.draw()
        renderer = fig.canvas.get_renderer()
        # Tomislav-RetCtx (user 2026-09-25: too much whitespace at the top): move the axes top up until the panel titles sit
        # 3 px (0.01 in) below the figure edge; the other margins are unchanged
        gap = fig.bbox.height - max(ax.title.get_window_extent(renderer).y1 for ax in axes)
        fig.subplots_adjust(top=top + (gap - 3) / fig.bbox.height)
        fig.canvas.draw()
        assert ylabel.get_window_extent(renderer).x1 + 3 <= min(t.get_window_extent(renderer).x0 for t in axes[0].get_yticklabels())
        for artist in [*fig.texts, *legend.get_texts(), *(t for ax in axes for t in [ax.title, ax.xaxis.label, *ax.get_xticklabels(), *ax.get_yticklabels()])]:
            b = artist.get_window_extent(renderer)
            assert b.x0 >= 0 and b.y0 >= 0 and b.x1 <= fig.bbox.width and b.y1 <= fig.bbox.height, artist.get_text()
        for suffix in ('pdf', 'svg', 'png'):
            fig.savefig(args.root / f'{args.name}.{suffix}', dpi=300, facecolor='white', bbox_inches=None)
        plt.close(fig)
    print(f'wrote {args.root / args.name}.{{pdf,svg,png}} at {args.width} x {h} in (original 4.4 x 2.1)')


if __name__ == '__main__':
    main()
