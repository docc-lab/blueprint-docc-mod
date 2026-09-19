#!/usr/bin/env python3
"""Tomislav-RetCtx: the e2e three-panel figure (mean, p99, successful req/s; mean +- sd over
repetitions) drawn from several roots' analysis/curves.json so variants from different
campaigns share one set of axes.

Usage: plot_dsb_sn_e2e_compare.py --out DIR --series "LABEL=ROOT:KIND" ... [--name end-to-end-all]
"""
import argparse
import json
from pathlib import Path

PALETTE = ['#333333', '#2878B5', '#D36B23', '#33945B', '#7B3F9E', '#B5651D']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--series', action='append', required=True)
    parser.add_argument('--name', default='end-to-end-all')
    args = parser.parse_args()
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    plt.rcParams.update({'font.size': 8, 'axes.labelsize': 8, 'legend.fontsize': 7,
                         'xtick.labelsize': 7, 'ytick.labelsize': 7, 'pdf.fonttype': 42})
    fig, axes = plt.subplots(1, 3, figsize=(6.6, 2.25))
    for index, spec in enumerate(args.series):
        label, rest = spec.split('=', 1)
        root, kind = rest.rsplit(':', 1)
        curves = json.loads((Path(root) / 'analysis/curves.json').read_text())
        data = sorted((p for p in curves if p['kind'] == kind), key=lambda p: p['offered_rps'])
        for axis, metric in zip(axes, ('mean_ms', 'p99_ms', 'successful_rps')):
            axis.errorbar([p['offered_rps'] / 1000 for p in data], [p[metric]['mean'] for p in data],
                          yerr=[p[metric]['sd'] for p in data], color=PALETTE[index % len(PALETTE)],
                          marker='o', markersize=2, linewidth=.9, capsize=1.5, label=label)
    for axis, label in zip(axes, ('Mean latency (ms)', 'p99 latency (ms)', 'Successful requests/s')):
        axis.set(xlabel='Offered rate (k requests/s)', ylabel=label)
        axis.grid(alpha=.2)
        axis.spines[['right', 'top']].set_visible(False)
    handles, labels = axes[0].get_legend_handles_labels()
    fig.legend(handles, labels, loc='upper center', ncol=len(labels), frameon=False)
    fig.tight_layout(rect=(0, 0, 1, .9), pad=.5, w_pad=.8)
    args.out.mkdir(parents=True, exist_ok=True)
    for extension in ('pdf', 'svg', 'png'):
        fig.savefig(args.out / f'{args.name}.{extension}', dpi=300)
    print('wrote', args.out / args.name)


if __name__ == '__main__':
    main()
