#!/usr/bin/env python3
"""Tomislav-RetCtx: overlay no-work ramps from several roots (e.g. old vs rebuilt SB).

Usage: plot_dsb_sn_nw_compare.py --out DIR --series "LABEL=ROOT:CASE[:dashed]" ...
Writes nw-compare.{pdf,svg,png}: mean, p99 and completed throughput vs offered rate.
"""
import argparse
import json
from pathlib import Path

PALETTE = ['#7A7A7A', '#333333', '#2878B5', '#D36B23', '#33945B', '#7B3F9E', '#B5651D', '#1F7A8C']


def load(root, case):
    """Mean and sample SD over every repetition directory NN-<case> in the root."""
    import statistics
    from collections import defaultdict
    by = defaultdict(list)
    for case_dir in Path(root, 'run').glob(f'*-{case}'):
        if '-interrupted-' in case_dir.name or not (case_dir / 'complete.json').exists():
            continue
        for p in case_dir.glob('rate-*/result.json'):
            r = json.loads(p.read_text())
            if 'kind' in r:
                by[r['offered_rps']].append(r)
    rows = []
    for rate, rs in sorted(by.items()):
        row = {'offered_rps': rate, 'n': len(rs)}
        for m in ('mean_ms', 'p99_ms', 'completed_rps'):
            vals = [r[m] for r in rs]
            row[m] = statistics.mean(vals)
            row[m + '_sd'] = statistics.stdev(vals) if len(vals) > 1 else 0.
        rows.append(row)
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--series', action='append', required=True)
    parser.add_argument('--name', default='nw-compare')
    args = parser.parse_args()
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    plt.rcParams.update({'font.size': 8, 'axes.labelsize': 8, 'legend.fontsize': 7,
                         'xtick.labelsize': 7, 'ytick.labelsize': 7, 'pdf.fonttype': 42})
    fig, axes = plt.subplots(1, 3, figsize=(6.6, 2.4))
    for index, spec in enumerate(args.series):
        label, rest = spec.split('=', 1)
        parts = rest.split(':')
        root, case = parts[0], parts[1]
        dashed = 'dashed' in parts[2:]
        color = next((int(v[6:]) for v in parts[2:] if v.startswith('color=')), index)
        rows = load(root, case)
        x = [r['offered_rps'] / 1000 for r in rows]
        style = dict(color=PALETTE[color % len(PALETTE)], marker='s' if dashed else 'o', markersize=2, linewidth=.9,
                     linestyle='--' if dashed else '-', label=label)
        axes[0].errorbar(x, [r['mean_ms'] for r in rows], yerr=[r['mean_ms_sd'] for r in rows], capsize=1.5, **style)
        axes[1].errorbar(x, [r['p99_ms'] for r in rows], yerr=[r['p99_ms_sd'] for r in rows], capsize=1.5, **style)
        axes[2].errorbar(x, [r['completed_rps'] / 1000 for r in rows], yerr=[r['completed_rps_sd'] / 1000 for r in rows], capsize=1.5, **style)
    for axis, label in zip(axes, ('Mean response time (ms)', 'p99 response time (ms)', 'Completed (k requests/s)')):
        axis.set(xlabel='Offered rate (k requests/s)', ylabel=label)
        axis.grid(alpha=.2)
        axis.spines[['right', 'top']].set_visible(False)
    lim = max(axes[2].get_xlim())
    axes[2].plot([0, lim], [0, lim], color='#BBBBBB', linewidth=.6, linestyle=':')
    handles, labels = axes[0].get_legend_handles_labels()
    ncol = 6 if len(labels) > 8 else min(5, len(labels))
    fig.legend(handles, labels, loc='upper center', ncol=ncol, frameon=False)
    fig.tight_layout(rect=(0, 0, 1, .82 if len(labels) > 8 else .84), pad=.5, w_pad=1.)
    args.out.mkdir(parents=True, exist_ok=True)
    for extension in ('pdf', 'svg', 'png'):
        fig.savefig(args.out / f'{args.name}.{extension}', dpi=300)
    print('wrote', args.out / args.name)


if __name__ == '__main__':
    main()
