#!/usr/bin/env python3
"""Tomislav-RetCtx: audit repeated complete ramps and draw their mean with sample SD."""

import argparse
import csv
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import statistics
from types import SimpleNamespace
import xml.etree.ElementTree as ET

import yaml


COUNTS = ['target_spans', 'offered_spans', 'attempted_spans', 'acknowledged_spans',
          'queue_dropped_spans', 'failed_spans', 'rejected_spans', 'unsent_spans',
          'scheduler_unissued_spans', 'attempted_checkpoint_spans', 'attempted_protobuf_bytes']
NAMES = {'none': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
PROFILES = ['zero', 'semconv10-example']


def read(path):
    return json.loads(path.read_text())


def write(path, data):
    path.write_text(json.dumps(data, indent=2, allow_nan=False) + '\n')


def counters(path, names):
    raw = path.read_text()
    return {name: sum(float(m.group(1)) for m in re.finditer(
        rf'^{name}(?:_total)?(?:\{{[^\n]*\}})?\s+([0-9.eE+-]+)', raw, re.M)) for name in names}


def audit(root, partial=False):
    manifest = read(root / 'manifest.json')
    recorded_rows = read(root / 'results.json')
    completed_names = {row['stage'] for row in recorded_rows}
    if not partial:
        assert read(root / 'completion.json')['all_stages_complete']
    spec = importlib.util.spec_from_file_location('archived_ramp', root / 'run_spanload_ramp.py')
    helper = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helper)
    assert hashlib.sha256((root / 'run_spanload_ramp.py').read_bytes()).hexdigest() == manifest['ramp_source_sha256']
    rows, all_phases = [], []
    for stage in manifest['stages']:
        if partial and stage['name'] not in completed_names:
            continue
        directory = root / ('ramps' if stage['kind'] == 'ramp' else 'controls') / stage['name']
        settings = read(directory / 'manifest.json')
        assert settings['generator_binary_sha256'] == manifest['generator_binary_sha256']
        assert settings['collector_image']['Id'] == manifest['collector_image']['Id']
        assert settings['complete_grid'] and settings['min_steady_seconds'] == 30
        assert settings['discard_first'] == 10 and settings['collector_gomemlimit'] == '2400MiB'
        assert settings['generator_cpus'] == manifest['generator_cpu_sets']
        assert settings['export_format'] == 'proto' and settings['profile'] == stage['profile']
        data = read(directory / 'results.json')
        assert [row['target_spans_per_second'] for row in data] == stage['rates']
        variant = directory / stage['variant']
        completed = read(variant / 'completion.json')
        assert completed['complete_grid'] and completed['steps'] == len(stage['rates'])
        config = yaml.safe_load((variant / 'collector.yaml').read_text())
        processors = ['memory_limiter', 'batch'] if stage['limiter'] else ['batch']
        assert config['service']['pipelines']['traces'] == {'receivers': ['otlp'], 'processors': processors, 'exporters': ['file']}
        assert config['processors']['batch'] == {'send_batch_size': 8192, 'send_batch_max_size': 8192, 'timeout': '200ms'}
        if stage['limiter']:
            assert config['processors']['memory_limiter'] == {'check_interval': '100ms', 'limit_mib': 3072, 'spike_limit_mib': 512}
        assert config['exporters']['file'] == {'path': '/dev/null', 'format': 'proto', 'flush_interval': '200ms'}
        final = read(variant / 'container-final.json')
        assert final['HostConfig']['NanoCpus'] == 1000000000 and final['HostConfig']['CpusetCpus'] == '2'
        assert final['HostConfig']['Memory'] == final['HostConfig']['MemorySwap'] == 4 * (1 << 30)
        assert all(value in final['Config']['Env'] for value in ['GOMAXPROCS=1', 'GOGC=100', 'GOMEMLIMIT=2400MiB'])
        assert final['State']['ExitCode'] == 0 and not final['State']['OOMKilled'] and final['RestartCount'] == 0
        for row in data:
            stem = f"{row['target_spans_per_second']:09d}"
            phases = []
            for index in range(4):
                sender = variant / f'{stem}-generator-{index}'
                native = read(sender / 'manifest.json')
                assert native['config']['profile'] == stage['profile'] and native['config']['bridge'] == stage['variant']
                records = [json.loads(line) for line in (sender / 'results.jsonl').read_text().splitlines()]
                phase, = [r for r in records if r['type'] == 'phase']
                assert phase['target_spans'] == phase['offered_spans'] + phase['scheduler_unissued_spans']
                assert phase['offered_spans'] == phase['attempted_spans'] + phase['queue_dropped_spans'] + phase['unsent_spans']
                assert phase['attempted_spans'] == phase['acknowledged_spans'] + phase['failed_spans'] + phase['rejected_spans']
                phases.append(phase)
                all_phases.append({**phase, 'kind': stage['kind'], 'repetition': stage['repetition'], 'profile': stage['profile'], 'variant': stage['variant']})
            combined = helper.combine_phases(phases)
            assert combined == read(variant / (stem + '-combined-phase.json'))
            samples = read(variant / (stem + '-samples.json'))
            calculated = helper.summarize_step(stage['variant'], row['target_spans_per_second'], combined, samples, SimpleNamespace(**settings))
            assert all(row[key] == value for key, value in calculated.items())
            before, after = [counters(variant / (stem + suffix), helper.METRICS) for suffix in ('-before.prom', '-after.prom')]
            delta = {name: after[name] - before[name] for name in helper.METRICS}
            acknowledged = sum(p['acknowledged_spans'] for p in phases)
            assert delta == row['full_phase_collector_deltas']
            assert delta[helper.METRICS[0]] == delta[helper.METRICS[2]] == acknowledged == row['acknowledged_spans']
            assert row['counts_reconciled'] and row['steady_seconds'] >= 30 and row['metric_scrape_errors'] == 0
            rows.append({**row, 'repetition': stage['repetition'], 'memory_limiter': stage['limiter'], 'kind': stage['kind'], 'stage': stage['name']})
    assert rows == recorded_rows
    if not partial:
        assert len(rows) == manifest['planned_ramp_points'] + manifest['planned_control_points']
    totals = {kind: {key: sum(p[key] for p in all_phases if p['kind'] == kind) for key in COUNTS} for kind in ('ramp', 'control')}
    return manifest, rows, totals


def aggregate(manifest, rows):
    points, summaries = [], {}
    for profile in PROFILES:
        summaries[profile] = {}
        for variant in NAMES:
            subset = [row for row in rows if row['kind'] == 'ramp' and row['profile'] == profile and row['variant'] == variant]
            rates = sorted({row['target_spans_per_second'] for row in subset})
            for rate in rates:
                group = [row for row in subset if row['target_spans_per_second'] == rate]
                assert len(group) == manifest['repetitions']
                values = [row['exported_spans_per_second'] for row in group]
                points.append({'profile': profile, 'variant': variant, 'offered_spans_per_second': rate,
                    'repetitions': len(group), 'mean_exported_spans_per_second': statistics.mean(values),
                    'sd_exported_spans_per_second': statistics.stdev(values) if len(values) > 1 else 0,
                    'minimum_exported_spans_per_second': min(values), 'maximum_exported_spans_per_second': max(values),
                    'mean_collector_cpu_percent': 100 * statistics.mean(row['collector_cpu_cores'] for row in group)})
            plateaus = []
            tail = []
            for repetition in range(1, manifest['repetitions'] + 1):
                last = sorted((row for row in subset if row['repetition'] == repetition), key=lambda r: r['target_spans_per_second'])[-3:]
                assert all(row['capacity_limited'] for row in last), 'the common upper rate range did not establish CPU saturation'
                plateaus.append(statistics.mean(row['exported_spans_per_second'] for row in last))
                tail.extend(last)
            summaries[profile][variant] = {'mean_plateau_spans_per_second': statistics.mean(plateaus),
                'sd_plateau_spans_per_second': statistics.stdev(plateaus) if len(plateaus) > 1 else 0,
                'plateau_per_repetition': plateaus, 'plateau_rates': rates[-3:],
                'collector_cpu_percent_at_plateau': statistics.mean(row['collector_cpu_cores'] for row in tail) * 100,
                'max_generator_cpu_cores_per_process': max(max(row['generator_cpu_cores_per_process']) for row in subset),
                'max_collector_rss_mib': max(row['collector_peak_rss_bytes'] for row in subset) / (1 << 20),
                'minimum_steady_seconds': min(row['steady_seconds'] for row in subset)}
        vanilla = summaries[profile]['none']['mean_plateau_spans_per_second']
        for item in summaries[profile].values():
            item['penalty_vs_vanilla_percent'] = 100 * (1 - item['mean_plateau_spans_per_second'] / vanilla)
    return points, summaries


def plot(root, points, repetitions):
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    import numpy as np
    from PIL import Image
    styles = {'none': ('#333333', 'o', 2.6, '-'), 'pb': ('#0072B2', '^', 3.7, '-'),
              'cgpb': ('#D55E00', 's', 2.7, '--'), 'sb': ('#009E73', 'o', 1.7, ':')}
    fonts = {'font.size': 9, 'axes.titlesize': 10, 'axes.labelsize': 9,
             'xtick.labelsize': 8, 'ytick.labelsize': 8, 'legend.fontsize': 8}
    with plt.rc_context({**fonts, 'font.family': 'DejaVu Sans', 'axes.linewidth': .65,
        'xtick.major.width': .65, 'ytick.major.width': .65, 'xtick.major.size': 2.5, 'ytick.major.size': 2.5,
        'xtick.major.pad': 2, 'ytick.major.pad': 2, 'pdf.fonttype': 42, 'svg.fonttype': 'none', 'savefig.bbox': None}):
        fig, axes = plt.subplots(1, 2, figsize=(4.4, 2.1), dpi=300)
        fig.subplots_adjust(left=.125, right=.985, bottom=.23, top=.84, wspace=.22)
        ylabel = fig.supylabel('Throughput (k spans/s)', x=.015, y=.535, fontsize=9)
        for ax, profile in zip(axes, PROFILES):
            top = 0
            for variant, (color, marker, size, linestyle) in styles.items():
                series = [row for row in points if row['profile'] == profile and row['variant'] == variant]
                x = np.array([row['offered_spans_per_second'] / 1000 for row in series])
                mean = np.array([row['mean_exported_spans_per_second'] / 1000 for row in series])
                sd = np.array([row['sd_exported_spans_per_second'] / 1000 for row in series])
                top = max(top, max(mean + sd))
                ax.fill_between(x, mean - sd, mean + sd, color=color, alpha=.15, linewidth=0)
                ax.plot(x, mean, color=color, marker=marker, markersize=size, linewidth=1.05,
                        markerfacecolor=color if variant == 'none' else 'white', markeredgewidth=.8,
                        linestyle=linestyle, label=NAMES[variant])
            ax.set_title('No semconv' if profile == 'zero' else '10 semconv', pad=6)
            ax.set_xlabel('Offered rate (k spans/s)', labelpad=3)
            ax.set_xlim(0, 1700 if profile == 'zero' else 212)
            ax.set_xticks([0, 400, 800, 1200, 1600] if profile == 'zero' else [0, 50, 100, 150, 200])
            ax.set_ylim(0, max(top * 1.10, 1080 if profile == 'zero' else 125))
            ax.set_yticks([0, 250, 500, 750, 1000] if profile == 'zero' else [0, 40, 80, 120])
            ax.grid(alpha=.20, linewidth=.5)
            ax.set_axisbelow(True)
            legend = ax.legend(loc='lower right', ncol=2, handlelength=1.05, handletextpad=.3, columnspacing=.6,
                               borderpad=.35, labelspacing=.3, borderaxespad=.3, framealpha=.95, edgecolor='#cccccc', fancybox=False)
            legend.get_frame().set_linewidth(.5)
        fig.text(.55, .025, f'Mean of {repetitions} runs; shading: ±1 SD', ha='center', va='bottom', fontsize=7)
        fig.canvas.draw()
        renderer = fig.canvas.get_renderer()
        assert ylabel.get_window_extent(renderer).x1 + 3 <= min(t.get_window_extent(renderer).x0 for t in axes[0].get_yticklabels())
        for artist in [*fig.texts, *(t for ax in axes for t in [ax.title, ax.xaxis.label, *ax.get_xticklabels(), *ax.get_yticklabels(), *ax.get_legend().get_texts()])]:
            bounds = artist.get_window_extent(renderer)
            assert bounds.x0 >= 0 and bounds.y0 >= 0 and bounds.x1 <= fig.bbox.width and bounds.y1 <= fig.bbox.height, artist.get_text()
        for suffix in ('pdf', 'svg', 'png'):
            fig.savefig(root / ('throughput-combined.' + suffix), dpi=300, facecolor='white', bbox_inches=None)
        plt.close(fig)
    box, = re.findall(rb'/MediaBox\s*\[\s*0\s+0\s+([0-9.]+)\s+([0-9.]+)\s*\]', (root / 'throughput-combined.pdf').read_bytes())
    assert [float(value) for value in box] == [316.8, 151.2]
    svg = ET.parse(root / 'throughput-combined.svg').getroot()
    assert svg.attrib['width'] == '316.8pt' and svg.attrib['height'] == '151.2pt'
    with Image.open(root / 'throughput-combined.png') as raster:
        assert raster.size == (1320, 630)
    write(root / 'figure-settings.json', {'annotation': 'Tomislav-RetCtx: complete measured grids; mean and sample SD across repetitions.',
        'width_inches': 4.4, 'height_inches': 2.1, 'png_dpi': 300, 'fonts_points': fonts,
        'shared_y_label': True, 'independent_y_scales': True, 'points_source': 'averaged-points.json'})


def report(root, manifest, rows, totals, summaries):
    lines = ['# Complete collector ramps with memory protection', '',
        'Tomislav-RetCtx: one collector, one CPU, 4 GiB/no swap; OTLP/gRPC → memory_limiter → batch → protobuf file export to /dev/null. '
        'The memory limiter checks every 100ms, with a 3,072 MiB hard threshold and 512 MiB spike allowance. '
        'GOMEMLIMIT=2400MiB, GOMAXPROCS=1, GOGC=100. Collector batch target/max: 8,192 spans; batch timeout and file flush: 200ms.', '',
        f'Each variant completes all 16 zero-attribute rates (100k–1.6M in 100k increments) and all 20 ten-attribute rates '
        f'(10k–200k in 10k increments), with {manifest["repetitions"]} repetitions. '
        f'Each point offers load for {manifest["seconds"]} seconds, discards the first 10, and measures at least 30 seconds '
        'of the four senders\' common active interval, excluding drain. Variant order rotates and profile order alternates across repetitions.', '',
        '[Combined figure (PDF)](throughput-combined.pdf) · [PNG](throughput-combined.png) · [SVG](throughput-combined.svg) · '
        '[Mean/SD data](averaged-points.json) · [Raw point CSV](results.csv)', '',
        'The figure is 4.4 × 2.1 inches with one shared y-axis label and independent axis scales. Lines show means and shading shows '
        'one sample standard deviation across repetitions; it is not a confidence interval. Every marker is measured.', '',
        '| Variant | Zero attributes, spans/s | SD of plateau across runs | Ten attributes, spans/s | SD across runs | Penalty, zero / ten |',
        '| --- | ---: | ---: | ---: | ---: | ---: |']
    for variant, name in NAMES.items():
        a, b = [summaries[profile][variant] for profile in PROFILES]
        lines.append(f'| {name} | {a["mean_plateau_spans_per_second"]:,.0f} | {a["sd_plateau_spans_per_second"]:,.0f} | '
                     f'{b["mean_plateau_spans_per_second"]:,.0f} | {b["sd_plateau_spans_per_second"]:,.0f} | '
                     f'{a["penalty_vs_vanilla_percent"]:.2f}% / {b["penalty_vs_vanilla_percent"]:.2f}% |')
    lines += ['', 'Each run\'s plateau is its mean over the final three offered rates; the table averages these run means. '
        'All final points meet the recorded collector CPU saturation criteria. Small bridge-type differences require interpreting the measured variability.', '',
        '## Matched memory-limiter controls', '',
        'The controls keep batches at 8,192 and GOMEMLIMIT at 2400MiB in both cases. Only the memory_limiter processor changes. '
        'They use one overloaded point per case, so their differences are diagnostic rather than repeated estimates.', '',
        '| Profile / variant | Without limiter, spans/s | With limiter, spans/s | Change |', '| --- | ---: | ---: | ---: |']
    for profile in PROFILES:
        for variant in ('none', 'sb'):
            controls = [row for row in rows if row['kind'] == 'control' and row['profile'] == profile and row['variant'] == variant]
            a, = [row['exported_spans_per_second'] for row in controls if not row['memory_limiter']]
            b, = [row['exported_spans_per_second'] for row in controls if row['memory_limiter']]
            lines.append(f'| {profile} / {NAMES[variant]} | {a:,.0f} | {b:,.0f} | {100 * (b/a-1):+.2f}% |')
    ramps = [row for row in rows if row['kind'] == 'ramp']
    lines += ['', '## Accounting and scope', '',
        f'All {len(ramps)} ramp points and {len(rows)-len(ramps)} controls passed independent raw-counter and steady-window audits. '
        f'Ramp acknowledgements/exported spans: {totals["ramp"]["acknowledged_spans"]:,}. '
        f'Generator queue drops: {totals["ramp"]["queue_dropped_spans"]:,}; RPC-failed spans: {totals["ramp"]["failed_spans"]:,}; '
        f'partial-rejected spans: {totals["ramp"]["rejected_spans"]:,}; unsent: {totals["ramp"]["unsent_spans"]:,}; '
        f'scheduler-unissued: {totals["ramp"]["scheduler_unissued_spans"]:,}. '
        'Full-phase collector accepted/exported deltas equal generator acknowledgements, including drain.', '',
        f'Maximum measured sender CPU per process: {max(max(row["generator_cpu_cores_per_process"]) for row in rows):.3f} of four available cores. '
        f'Peak steady-window collector RSS: {max(row["collector_peak_rss_bytes"] for row in rows)/(1<<20):.1f} MiB. '
        'Raw Prometheus snapshots include memory and refusal metrics. All collector exits were clean, with no OOMs/restarts.', '',
        'Four native generator processes have independent wall-clock open-loop arrival schedules; the x-axis is their aggregate offered target. '
        'Each process uses eight RPC workers, a 64-batch queue, 512 spans/request, a 2s RPC timeout, and a 5s drain limit. '
        'Full queues drop scheduled work and count it. Attempted RPC traffic, receiver acceptance, and export remain distinct measurements.', '',
        'The payload distribution and checkpoint share are unchanged from the supplied Uber day 1 random-CPD 2–8 PB0/CGP0/SB3 histograms: '
        '52.50588161787073% checkpoint spans and seed 42 per process. The ten-attribute profile is illustrative. '
        'These are synthetic marginal payload mixtures, not application traces. The /dev/null exporter retains protobuf serialization but omits backend/network export latency.', '',
        'This configuration differs from the earlier batch-512/no-limiter measurements in both memory protection and batching. '
        'The matched controls isolate the limiter at the new batch setting. Source/configuration hashes, exact commands, native manifests, '
        'before/after counters, one-second samples, and Docker resource records are retained. Application services and Kubernetes were not changed.', '',
        'Reproduce this report and figure with `python analyze_spanload_suite.py --out .` from this artifact directory.', '']
    (root / 'RESULTS.md').write_text('\n'.join(lines))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--verify-completed-stages', action='store_true', help='audit completed stages without generating final summaries or plots')
    args = parser.parse_args()
    root = args.out.resolve()
    manifest, rows, totals = audit(root, partial=args.verify_completed_stages)
    if args.verify_completed_stages:
        result = {'status': 'passed', 'partial': True, 'points': len(rows), 'totals': totals}
        write(root / 'INTERIM_AUDIT.json', result)
        print(json.dumps(result))
        return
    points, summaries = aggregate(manifest, rows)
    write(root / 'averaged-points.json', points)
    write(root / 'SUMMARY.json', {'status': 'passed', 'profiles': summaries, 'totals': totals,
                                'ramp_points': manifest['planned_ramp_points'], 'control_points': manifest['planned_control_points']})
    with (root / 'results.csv').open('w', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    plot(root, points, manifest['repetitions'])
    report(root, manifest, rows, totals, summaries)
    print(json.dumps(summaries, indent=2))


if __name__ == '__main__':
    main()
