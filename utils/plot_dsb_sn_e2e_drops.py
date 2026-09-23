#!/usr/bin/env python3
"""Tomislav-RetCtx: response-time ramps and span-drop-rate ramps from the e2e run.

Reads completed cases under <root>/run, aggregates per offered rate across
repetitions (mean, sample SD), and writes analysis/drops.{json,csv} plus
analysis/response-time.{pdf,svg,png} and analysis/drop-rates.{pdf,svg,png}.
Read-only with respect to raw data.

Drop-rate definitions (per 30 s point window, all eight collectors):
  vanilla total        = receiver refused / (accepted + refused)      [no class split]
  bridge total         = (hp_refused + lp_refused) / all admitted+refused (priority counters)
  bridge checkpoint    = hp_refused / (hp_admitted + hp_refused); "worst" = max over collectors
  bridge non-checkpoint= lp_refused / (lp_admitted + lp_refused)
The vanilla total line is repeated on every drop panel as the reference.
"""
import argparse
from collections import defaultdict
import csv
import gzip
import json
from pathlib import Path
import statistics

from prepare_dsb_sn_e2e import write_json

COLORS = {'nt': '#7A7A7A', 'v': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABELS = {'nt': 'No tracing', 'v': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
# KINDS are the kinds with a drop rate. The no-work campaign adds a no-tracing case: it has no
# SDK, so its collectors receive nothing and no drop rate exists for it, but it is the baseline
# for the response-time ramps and is drawn there via TIMING_ONLY.
KINDS = ('v', 'pb', 'cgpb', 'sb')
TIMING_ONLY = ('nt',)


EXTRA_COLORS = ['#7B3F9E', '#B5651D', '#1F7A8C']


def cases(root):
    for case in sorted((root / 'run').iterdir()):
        if case.is_dir() and '-interrupted-' not in case.name and '-never-started-' not in case.name \
                and (case / 'complete.json').exists():
            yield case


def priority_sample(path):
    with gzip.open(path, 'rt') as stream:
        lines = stream.read().splitlines()
    line = next(l for l in reversed(lines) if 'priority_processor_metrics' in l)
    return json.loads(line[line.index('{'):])


def pooled(parts, key):
    """Application-wide rate: refused and total summed over the given collectors."""
    num = sum(p[key][0] for p in parts)
    den = sum(p[key][1] for p in parts)
    return 100 * num / den if den else None


def hottest(parts, key):
    rated = [p for p in parts if p[key][1]]
    return max(rated, key=lambda p: p[key][0] / p[key][1]) if rated else None


def worst(parts, key):
    """Same formula over the single worst collector for this metric."""
    top = hottest(parts, key)
    return pooled([top], key) if top else None


def except_worst(parts, key):
    """Same formula over every collector except the worst one for this metric. Distinguishes a
    hot spot on one collector from loss spread across the whole application."""
    rated = [p for p in parts if p[key][1]]
    if len(rated) < 2:
        return None
    top = hottest(parts, key)
    return pooled([p for p in rated if p is not top], key)


def timing_row(point, kind):
    """Response-time fields only, for a kind that has no drop rate (no SDK, nothing to shed).
    The drop fields stay None; aggregate() skips them, so this kind appears only in the
    response-time panels."""
    result = json.loads((point / 'result.json').read_text())
    return {'kind': kind, 'repetition': result['repetition'], 'offered_rps': result['offered_rps'],
            'completed_rps': result['completed_rps'], 'mean_ms': result['mean_ms'], 'p99_ms': result['p99_ms'],
            'total_pct': None, 'total_worst_pct': None, 'checkpoint_pct': None,
            'checkpoint_worst_pct': None, 'noncheckpoint_pct': None}


def point_drops(point, kind):
    result = json.loads((point / 'result.json').read_text())
    pods = json.loads((point / 'after/pods.json').read_text())['items']
    row = {'kind': kind, 'repetition': result['repetition'], 'offered_rps': result['offered_rps'],
           'completed_rps': result['completed_rps'], 'mean_ms': result['mean_ms'], 'p99_ms': result['p99_ms']}
    if kind == 'v':
        before = json.loads((point / 'before/snapshot.json').read_text())['collectors']
        after = json.loads((point / 'after/snapshot.json').read_text())['collectors']
        parts = []
        for name, new in after.items():
            old = before.get(name)
            if old is None:
                continue
            dr = new.get('otelcol_receiver_refused_spans_total', 0) - old.get('otelcol_receiver_refused_spans_total', 0)
            da = new.get('otelcol_receiver_accepted_spans_total', 0) - old.get('otelcol_receiver_accepted_spans_total', 0)
            assert dr >= 0 and da >= 0, (point, name)
            parts.append({'total': (dr, da + dr)})
        row.update(total_pct=pooled(parts, 'total'), total_worst_pct=worst(parts, 'total'),
                   total_except_worst_pct=except_worst(parts, 'total'),
                   checkpoint_pct=None, checkpoint_worst_pct=None, checkpoint_except_worst_pct=None,
                   noncheckpoint_pct=None, noncheckpoint_worst_pct=None,
                   noncheckpoint_except_worst_pct=None)
        return row
    keys = ('hp_admitted', 'hp_refused', 'lp_admitted', 'lp_refused')
    parts = []
    for pod in pods:
        name = pod['metadata']['name']
        if not name.startswith('otelcol-'):
            continue
        old = priority_sample(point / 'before' / f'logs-{name}.txt.gz')
        new = priority_sample(point / 'after' / f'logs-{name}.txt.gz')
        d = {key: new[key] - old[key] for key in keys}
        assert all(v >= 0 for v in d.values()), (point, name, 'counter reset')
        parts.append({
            'total': (d['hp_refused'] + d['lp_refused'], sum(d.values())),
            'checkpoint': (d['hp_refused'], d['hp_admitted'] + d['hp_refused']),
            'noncheckpoint': (d['lp_refused'], d['lp_admitted'] + d['lp_refused']),
        })
    assert len(parts) == 8, (point, len(parts))
    row.update(total_pct=pooled(parts, 'total'), total_worst_pct=worst(parts, 'total'),
               total_except_worst_pct=except_worst(parts, 'total'),
               checkpoint_pct=pooled(parts, 'checkpoint'), checkpoint_worst_pct=worst(parts, 'checkpoint'),
               checkpoint_except_worst_pct=except_worst(parts, 'checkpoint'),
               noncheckpoint_pct=pooled(parts, 'noncheckpoint'),
               noncheckpoint_worst_pct=worst(parts, 'noncheckpoint'),
               noncheckpoint_except_worst_pct=except_worst(parts, 'noncheckpoint'))
    return row


def aggregate(rows, field):
    groups = defaultdict(list)
    for row in rows:
        if row.get(field) is not None:
            groups[(row['kind'], row['offered_rps'])].append(row[field])
    out = {}
    for key, values in groups.items():
        out[key] = {'mean': statistics.mean(values), 'sd': statistics.stdev(values) if len(values) > 1 else 0.,
                    'n': len(values)}
    return out


def series(agg, kind):
    keys = sorted(k for k in agg if k[0] == kind)
    return ([k[1] / 1000 for k in keys], [agg[k]['mean'] for k in keys], [agg[k]['sd'] for k in keys])


def main():
    global KINDS
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--extra', action='append', default=[],
                        help='LABEL=ROOT:KIND adds that root\'s KIND ramps as an additional series named LABEL')
    parser.add_argument('--borrow', action='append', default=[], metavar='ROOT:KIND',
                        help='load KIND from another root under its OWN name (unlike --extra, which '
                             'renames it to a new series). Used to bring the vanilla reference into a '
                             'campaign that measured only bridges: vanilla has no response path to turn '
                             'off, so its ramps are identical in both roots.')
    parser.add_argument('--repetition', type=int, action='append', default=[], metavar='N',
                        help='only use these repetitions (repeatable). Default: all of them. Applied '
                             'to borrowed and extra series too, so every curve carries the same n.')
    parser.add_argument('--suffix', default='', help='appended to output file names')
    parser.add_argument('--rt-xcut', type=float, default=None, metavar='K',
                        help='DROP response-time points above this offered rate (k req/s), instead of '
                             'only clipping the view as --xmax does. Use this to zoom into the '
                             'pre-knee region: --xmax leaves the segment heading to the next, '
                             'off-scale point as a stub climbing the right edge, and the last point '
                             'sits on the axis line with half its error bar cut off. The axis is then '
                             'given a small margin so the last point is fully drawn.')
    parser.add_argument('--xmax', type=float, default=None,
                        help='cut the response-time panels at this offered rate (k req/s). Past '
                             'the knee latency is dominated by generator queueing and grows without '
                             'bound, which flattens everything below it.')
    parser.add_argument('--rt-height', type=float, default=None,
                        help='height in inches of the response-time figure (default: half the width)')
    parser.add_argument('--rt-ylim', default=None, metavar='MEAN,P99',
                        help='y ceilings in ms for the mean and p99 response-time panels')
    parser.add_argument('--xtick', type=float, default=1.0,
                        help='x tick spacing in k req/s (real-work ramp spans ~2..5k)')
    parser.add_argument('--exclude', action='append', default=[], metavar='KIND',
                        help='drop this kind from the primary root. Use with --extra when a '
                             'variant was re-measured in its own root and the primary root still '
                             'holds the superseded one.')
    parser.add_argument('--width', type=float, default=3.33,
                        help='inches; 3.33 = one column of a two-column CS conference paper. '
                             'Figures are drawn at final size, so the point sizes are what the '
                             'reader sees. Below 4.5 in the three drop panels stack vertically.')
    parser.add_argument('--legend-cols', type=int, default=0,
                        help='legend columns; 0 puts every entry on one row. Eight series do not '
                             'fit one row at column width, so use 4 for two rows.')
    parser.add_argument('--fontsize', type=float, default=8)
    parser.add_argument('--figures', type=Path, default=None,
                        help='also write the figures here (default: only <root>/analysis)')
    args = parser.parse_args()
    root = args.out.resolve()
    rows = []
    for case in cases(root):
        kind = case.name[3:]
        if kind in args.exclude or (kind not in KINDS and kind not in TIMING_ONLY):
            continue
        for point in sorted(case.glob('rate-*')):
            if (point / 'result.json').exists() and 'kind' in json.loads((point / 'result.json').read_text()):
                rows.append(timing_row(point, kind) if kind in TIMING_ONLY else point_drops(point, kind))
    kinds = [k for k in KINDS if k not in args.exclude]
    for spec in args.borrow:
        borrow_root, kind = spec.rsplit(':', 1)
        assert not any(r['kind'] == kind for r in rows), ('borrowed kind already in the primary root', kind)
        for case in cases(Path(borrow_root).resolve()):
            if case.name[3:] != kind:
                continue
            for point in sorted(case.glob('rate-*')):
                if (point / 'result.json').exists() and 'kind' in json.loads((point / 'result.json').read_text()):
                    rows.append(timing_row(point, kind) if kind in TIMING_ONLY else point_drops(point, kind))
        assert any(r['kind'] == kind for r in rows), ('borrowed kind not found', spec)
        if kind not in kinds and kind in KINDS:
            kinds.append(kind)
    for index, spec in enumerate(args.extra):
        label, rest = spec.split('=', 1)
        extra_root, kind = rest.rsplit(':', 1)
        for case in cases(Path(extra_root).resolve()):
            if case.name[3:] != kind:
                continue
            for point in sorted(case.glob('rate-*')):
                if (point / 'result.json').exists() and 'kind' in json.loads((point / 'result.json').read_text()):
                    row = point_drops(point, kind)
                    row['kind'] = label
                    rows.append(row)
        # Tomislav-RetCtx: the same label may appear more than once -- one --extra per
        # campaign root when repetitions live in separate roots. Those rows ACCUMULATE
        # into a single series; appending the label again would draw it once per root.
        if label not in COLORS:
            COLORS[label] = EXTRA_COLORS[len([k for k in kinds if k not in KINDS]) % len(EXTRA_COLORS)]
        LABELS.setdefault(label, label)
        if label not in kinds:
            kinds.append(label)
    KINDS = tuple(kinds)
    if args.repetition:
        # Tomislav-RetCtx: applied after every source is loaded, so a borrowed baseline is cut
        # to the same runs as the primary root and no curve gets a tighter error bar than another.
        rows = [r for r in rows if r['repetition'] in args.repetition]
        assert rows, ('no points for repetitions', args.repetition)
    output = root / 'analysis'
    output.mkdir(exist_ok=True)
    fields = ['kind', 'repetition', 'offered_rps', 'completed_rps', 'mean_ms', 'p99_ms',
              'total_pct', 'total_worst_pct', 'total_except_worst_pct',
              'checkpoint_pct', 'checkpoint_worst_pct', 'checkpoint_except_worst_pct',
              'noncheckpoint_pct', 'noncheckpoint_worst_pct', 'noncheckpoint_except_worst_pct']
    with (output / f'drops{args.suffix}.csv').open('w') as stream:
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        writer.writerows(rows)
    reps = sorted({r['repetition'] for r in rows})
    aggregates = {field: aggregate(rows, field) for field in fields[3:]}
    write_json(output / f'drops{args.suffix}.json', {
        'repetitions': reps, 'points': rows,
        'aggregates': {field: {f'{k[0]}:{k[1]}': v for k, v in agg.items()} for field, agg in aggregates.items()}})

    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.ticker
    import matplotlib.pyplot as plt
    fs = args.fontsize
    # Drawn at final on-page size; svg.fonttype 'path' keeps glyphs as outlines so viewers
    # without the font cannot substitute a serif.
    plt.rcParams.update({'font.family': 'sans-serif', 'font.sans-serif': ['DejaVu Sans'],
                         'svg.fonttype': 'path', 'pdf.fonttype': 42,
                         'font.size': fs, 'axes.labelsize': fs, 'legend.fontsize': fs - 1,
                         'xtick.labelsize': fs - 1, 'ytick.labelsize': fs - 1,
                         'axes.linewidth': .6, 'xtick.major.width': .6, 'ytick.major.width': .6,
                         'xtick.major.size': 2.5, 'ytick.major.size': 2.5})
    stacked = args.width < 4.5

    def draw(axis, agg, kind, cut=None, **style):
        x, y, sd = series(agg, kind)
        if cut is not None:
            kept = [i for i, v in enumerate(x) if v <= cut]
            x, y, sd = [x[i] for i in kept], [y[i] for i in kept], [sd[i] for i in kept]
        axis.errorbar(x, y, yerr=sd, color=COLORS[kind], marker='o', markersize=1.6, linewidth=.9,
                      capsize=1.0, elinewidth=.6, label=LABELS[kind], **style)

    def finish(fig, axes, path, vertical=None):
        # vertical stacks label only the bottom panel; side-by-side panels share ONE centred
        # label, because at column width the text is wider than a single panel and would be
        # clipped at the figure edge.
        vertical = stacked if vertical is None else vertical
        for i, axis in enumerate(axes):
            if vertical and i == len(axes) - 1:
                axis.set_xlabel('Offered rate (k req/s)')
            axis.xaxis.set_major_locator(matplotlib.ticker.MultipleLocator(args.xtick))
            axis.grid(alpha=.2)
            axis.spines[['right', 'top']].set_visible(False)
        handles, labels = axes[0].get_legend_handles_labels()
        legend_rows = -(-len(labels) // (args.legend_cols or len(labels)))
        band = (legend_rows * (fs + 2)) / 72 / fig.get_size_inches()[1]
        bottom = 0 if vertical else (fs + 3) / 72 / fig.get_size_inches()[1]
        fig.tight_layout(pad=.35, w_pad=.9, h_pad=.7, rect=(0, bottom, 1, 1 - band))
        if not vertical:
            mid = (axes[0].get_position().x0 + axes[-1].get_position().x1) / 2
            fig.text(mid, .012, 'Offered rate (k req/s)', ha='center', va='bottom', fontsize=fs)
        ncol = args.legend_cols or len(labels)
        rows = -(-len(labels) // ncol)
        fig.legend(handles, labels, loc='lower center',
                   bbox_to_anchor=(.5, max(a.get_position().y1 for a in axes) + .012),
                   ncol=ncol, frameon=False, handlelength=1.0, handletextpad=.4,
                   columnspacing=.8, borderpad=0, borderaxespad=.1, fontsize=fs - 1)
        for extension in ('pdf', 'svg', 'png'):
            fig.savefig(output / f'{path}{args.suffix}.{extension}', dpi=300)
            if args.figures:
                args.figures.mkdir(parents=True, exist_ok=True)
                fig.savefig(args.figures / f'{path}{args.suffix}.{extension}', dpi=300)
        plt.close(fig)

    # Response-time ramps: mean and p99.
    fig, axes = plt.subplots(1, 2, figsize=(args.width, args.rt_height or args.width * .50))
    timing_kinds = tuple(k for k in TIMING_ONLY if any(r['kind'] == k for r in rows)) + KINDS
    rt_ceilings = [None, None] if not args.rt_ylim else [float(v) for v in args.rt_ylim.split(',')]
    for axis, ceiling, field, label in zip(axes, rt_ceilings, ('mean_ms', 'p99_ms'),
                                           ('Mean resp. (ms)', 'p99 resp. (ms)')):
        for kind in timing_kinds:
            draw(axis, aggregates[field], kind, cut=args.rt_xcut)
        axis.set_ylabel(label)
        if args.rt_xcut:
            # Margin of a fifth of a tick, so the last marker and its error bar clear the spine.
            axis.set_xlim(right=args.rt_xcut + args.xtick / 5)
        elif args.xmax:
            axis.set_xlim(right=args.xmax)
        if ceiling:
            axis.set_ylim(0, ceiling)
    finish(fig, axes, 'response-time', vertical=False)

    # Drop-rate ramps: total, worst-case checkpoint, non-checkpoint; vanilla total repeated.
    if stacked:
        fig, axes = plt.subplots(3, 1, figsize=(args.width, args.width * .95), sharex=True)
    else:
        fig, axes = plt.subplots(1, 3, figsize=(args.width, args.width * .34))
    panels = ((('total_pct', 'All spans (%)'),
               ('checkpoint_worst_pct', 'Checkpoint (%)'),
               ('noncheckpoint_pct', 'Other spans (%)')) if stacked else
              (('total_pct', 'All spans\ndropped (%)'),
               ('checkpoint_worst_pct', 'Checkpoint spans dropped,\nworst collector (%)'),
               ('noncheckpoint_pct', 'Non-checkpoint spans\ndropped (%)')))
    for axis, (field, label) in zip(axes, panels):
        draw(axis, aggregates['total_pct'], 'v')
        for kind in KINDS[1:]:
            draw(axis, aggregates[field], kind)
        axis.set_ylabel(label)
        axis.set_ylim(bottom=0)
    finish(fig, axes, 'drop-rates')

    # Same three metrics, application-wide (all eight collectors pooled) and with the single
    # worst collector for that metric removed.
    # One formula, three collector sets: all eight, all but the worst, the worst alone.
    for name, suffix, title in (('drop-rates-all', '_pct', 'all 8 collectors'),
                                ('drop-rates-except-worst', '_except_worst_pct', 'excluding worst collector'),
                                ('drop-rates-worst', '_worst_pct', 'worst collector only')):
        if stacked:
            fig, axes = plt.subplots(3, 1, figsize=(args.width, args.width * .95), sharex=True)
        else:
            fig, axes = plt.subplots(1, 3, figsize=(args.width, args.width * .36))
        panels = ((('total' + suffix, 'All spans (%)'),
                   ('checkpoint' + suffix, 'Checkpoint (%)'),
                   ('noncheckpoint' + suffix, 'Other spans (%)')) if stacked else
                  (('total' + suffix, 'All spans dropped (%%)\n%s' % title),
                   ('checkpoint' + suffix, 'Checkpoint spans dropped (%%)\n%s' % title),
                   ('noncheckpoint' + suffix, 'Non-checkpoint dropped (%%)\n%s' % title)))
        for axis, (field, label) in zip(axes, panels):
            draw(axis, aggregates['total' + suffix], 'v')
            for kind in KINDS[1:]:
                draw(axis, aggregates[field], kind)
            axis.set_ylabel(label)
            axis.set_ylim(bottom=0)
        finish(fig, axes, name)

    print('points', len(rows), 'repetitions', reps)
    for field in ('total_pct', 'total_except_worst_pct', 'total_worst_pct',
                  'checkpoint_pct', 'checkpoint_except_worst_pct', 'checkpoint_worst_pct'):
        for kind in KINDS:
            x, y, sd = series(aggregates[field], kind)
            if y:
                print(field, kind, ' '.join(f'{v:.1f}' for v in y))


if __name__ == '__main__':
    main()
