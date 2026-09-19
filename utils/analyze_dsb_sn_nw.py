#!/usr/bin/env python3
"""Tomislav-RetCtx: audit and plot the zero-work Social Network ramp (paper Figure 12 style).

Reads <root>/run/01-<case>/rate-*/ from run_dsb_sn_nw.py, verifies raw wrk output,
computes per-point resource and telemetry deltas, collector refusal rates (expected
zero under passthrough) and SDK drops, then writes analysis/{audit,points,curves}.json,
points.csv, nw-response-time.{pdf,svg,png} (mean, p99) and nw-end-to-end.{pdf,svg,png}
(mean, p99, completed throughput). 100 % sampling is solid, 10 % dashed.
"""
import argparse
from collections import defaultdict
import csv
import json
from pathlib import Path
import re
import statistics

from prepare_dsb_sn_e2e import write_json
from analyze_dsb_sn_e2e import verify_raw, resource_deltas, periodic_deltas, inspect_traces

COLORS = {'nt': '#7A7A7A', 'v': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABELS = {'nt': 'no-tracing', 'v': 'vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}


def analyze(root, partial=False):
    plan = json.loads((root / 'plan.json').read_text())
    repetitions = int(plan.get('repetitions', 1))
    profile = plan.get('collector_profile', 'passthrough')
    expected = {(rep, case, rate) for rep in range(1, repetitions + 1) for case in plan['cases'] for rate in plan['ramp_rates']}
    seen, rows, issues = set(), [], []
    for path in sorted((root / 'run').glob('*/rate-*/result.json')):
        if '-interrupted-' in path.parent.parent.name:
            continue
        result = json.loads(path.read_text())
        if 'kind' not in result:
            continue
        key = (result.get('repetition', 1), result['case'], result['offered_rps'])
        assert key in expected and key not in seen, key
        assert result['seed'] == plan['seeds'][key[0] - 1] and result['offer_seconds'] == plan['seconds_per_rate']
        verify_raw(path.parent, result)
        seen.add(key)
        before = json.loads((path.parent / 'before/snapshot.json').read_text())
        after = json.loads((path.parent / 'after/snapshot.json').read_text())
        result['resources'] = resource_deltas(before, after)
        # periodic_deltas' kind argument only selects whether priority-processor logs are expected
        # on the collectors: passthrough roots have none, admission roots have them for bridges.
        counter_kind = result['kind'] if profile.startswith('admission') and result['kind'] in ('pb', 'cgpb', 'sb') else 'v'
        result['periodic_counters'] = periodic_deltas(before, after, counter_kind)
        priority = result['periodic_counters']['deltas'].get('priority', {})
        result['priority_counters'] = priority
        admitted = priority.get('hp_admitted', 0.) + priority.get('lp_admitted', 0.)
        refused = priority.get('hp_refused', 0.) + priority.get('lp_refused', 0.)
        result['priority_refused_pct'] = 100 * refused / (admitted + refused) if admitted + refused else 0.
        result['priority_hp_refused'] = priority.get('hp_refused', 0.)
        deltas = result['collector_deltas']
        accepted = deltas.get('otelcol_receiver_accepted_spans_total', 0.)
        refused = deltas.get('otelcol_receiver_refused_spans_total', 0.)
        result['collector_refused_pct'] = 100 * refused / (accepted + refused) if accepted + refused else 0.
        result['collector_accepted_spans'] = accepted
        sdk = result['periodic_counters']['deltas'].get('sdk', {})
        result['sdk_spans_dropped'] = sdk.get('spans_dropped', 0.)
        result['sdk_spans_received'] = sdk.get('spans_received', 0.)
        if result['completed_requests']:
            seconds = result['completed_requests'] / result['completed_rps']
            result['successful_rps'] = result['completed_rps'] * (1 - result['non_2xx_3xx'] / result['completed_requests'])
        else:
            seconds = result['wrk_seconds']
        result['sent_rps'] = result['sent_requests'] / seconds
        result['generator_cores'] = result['generator_cpu_seconds'] / result['wall_seconds']
        result['application_cores'] = result['resources'].get('application', {}).get('cpu_cores', 0.)
        result['collector_cores'] = result['resources'].get('collector', {}).get('cpu_cores', 0.)
        for field in ('snapshot_errors', 'counter_resets', 'restarts_changed'):
            if result[field]:
                issues.append({'point': list(key), 'field': field, 'value': result[field]})
        if refused and profile != 'admission':
            issues.append({'point': list(key), 'field': 'collector_refused_spans', 'value': refused})
        if result['priority_hp_refused']:
            issues.append({'point': list(key), 'field': 'priority_hp_refused', 'value': result['priority_hp_refused']})
        if result['sdk_spans_dropped']:
            issues.append({'point': list(key), 'field': 'sdk_spans_dropped', 'value': result['sdk_spans_dropped']})
        if result['kind'] != 'nt':
            sample = path.parent / 'settled-traces.json.gz'
            result['trace_sample_stage'] = 'after-drain' if sample.exists() else 'immediate'
            if not sample.exists():
                sample = path.parent / 'sample-traces.json.gz'
            if sample.exists():
                try:
                    result['trace_sample'] = inspect_traces(sample, result['kind'])
                except Exception as error:  # record, do not discard the measurement
                    issues.append({'point': list(key), 'field': 'trace_sample', 'value': repr(error)})
            else:
                issues.append({'point': list(key), 'field': 'trace_sample', 'value': 'not captured'})
        rows.append(result)
    if not partial:
        assert seen == expected, f'{len(expected - seen)} missing points'
        assert json.loads((root / 'run-complete.json').read_text())['passed']
    metrics = ('mean_ms', 'p50_ms', 'p95_ms', 'p99_ms', 'completed_rps', 'successful_rps', 'sent_rps',
               'generator_cores', 'application_cores', 'collector_cores', 'collector_refused_pct',
               'sdk_spans_dropped', 'non_2xx_3xx', 'priority_refused_pct', 'priority_hp_refused')
    # Curves: per case and offered rate, mean and sample SD over repetitions.
    grouped = defaultdict(list)
    for row in rows:
        grouped[row['case'], row['offered_rps']].append(row)
    curves = defaultdict(list)
    for (case, rate), points in sorted(grouped.items(), key=lambda kv: (kv[0][0], kv[0][1])):
        item = {'offered_rps': rate, 'repetitions': len(points)}
        for m in metrics:
            values = [p[m] for p in points]
            item[m] = statistics.mean(values)
            item[m + '_sd'] = statistics.stdev(values) if len(values) > 1 else 0.
        curves[case].append(item)
    knees = {}
    for case, points in curves.items():
        best = max(points, key=lambda p: p['completed_rps'])
        knees[case] = {'peak_completed_rps': best['completed_rps'], 'peak_completed_rps_sd': best['completed_rps_sd'],
                       'at_offered_rps': best['offered_rps'], 'repetitions': best['repetitions'],
                       'per_repetition_peak': sorted((max(p['completed_rps'] for p in rows if p['case'] == case and p.get('repetition', 1) == rep), rep)
                                                     for rep in {p.get('repetition', 1) for p in rows if p['case'] == case})}
    output = root / 'analysis'
    output.mkdir(exist_ok=True)
    write_json(output / 'audit.json', {'raw_verified_points': len(seen), 'expected_points': len(expected),
                                     'complete': seen == expected and not partial, 'issues': issues})
    write_json(output / 'points.json', rows)
    write_json(output / 'curves.json', {'curves': curves, 'knees': knees})
    fields = ['case', 'kind', 'sample_ratio', 'repetition', 'offered_rps', 'connections', 'threads', 'sent_requests',
              'completed_requests', *metrics]
    with (output / 'points.csv').open('w') as stream:
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction='ignore')
        writer.writeheader()
        writer.writerows(sorted(rows, key=lambda r: (r['case'], r.get('repetition', 1), r['offered_rps'])))
    if not curves:
        return
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    plt.rcParams.update({'font.size': 8, 'axes.labelsize': 8, 'legend.fontsize': 7,
                         'xtick.labelsize': 7, 'ytick.labelsize': 7, 'pdf.fonttype': 42})

    def draw(axis, metric, scale=1.):
        for case in plan['cases']:
            points = curves.get(case)
            if not points:
                continue
            kind = case.split('-')[0]
            dashed = case.endswith('-s10')
            axis.errorbar([p['offered_rps'] / 1000 for p in points], [p[metric] * scale for p in points],
                          yerr=[p[metric + '_sd'] * scale for p in points], capsize=1.5,
                          color=COLORS[kind], marker='o' if not dashed else 's', markersize=2, linewidth=.9,
                          linestyle='--' if dashed else '-', label=f"{LABELS[kind]}{' 10%' if dashed else ''}")

    def finish(fig, axes, name):
        for axis in axes:
            axis.set_xlabel('Offered rate (k requests/s)')
            axis.grid(alpha=.2)
            axis.spines[['right', 'top']].set_visible(False)
        handles, labels = axes[0].get_legend_handles_labels()
        fig.legend(handles, labels, loc='upper center', ncol=5, frameon=False)
        fig.tight_layout(rect=(0, 0, 1, .84), pad=.5, w_pad=1.)
        for extension in ('pdf', 'svg', 'png'):
            fig.savefig(output / f'{name}.{extension}', dpi=300)
        plt.close(fig)

    fig, axes = plt.subplots(1, 2, figsize=(4.4, 2.3))
    draw(axes[0], 'mean_ms'); axes[0].set_ylabel('Mean response time (ms)')
    draw(axes[1], 'p99_ms'); axes[1].set_ylabel('p99 response time (ms)')
    finish(fig, axes, 'nw-response-time')
    fig, axes = plt.subplots(1, 3, figsize=(6.6, 2.4))
    draw(axes[0], 'mean_ms'); axes[0].set_ylabel('Mean response time (ms)')
    draw(axes[1], 'p99_ms'); axes[1].set_ylabel('p99 response time (ms)')
    draw(axes[2], 'completed_rps', 1 / 1000); axes[2].set_ylabel('Completed (k requests/s)')
    lim = max(plan['ramp_rates']) / 1000
    axes[2].plot([0, lim], [0, lim], color='#BBBBBB', linewidth=.6, linestyle=':')
    finish(fig, axes, 'nw-end-to-end')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--partial', action='store_true')
    args = parser.parse_args()
    analyze(args.out.resolve(), args.partial)
