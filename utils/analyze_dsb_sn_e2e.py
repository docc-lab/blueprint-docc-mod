#!/usr/bin/env python3
"""Tomislav-RetCtx: audit raw real-application ramps and plot paired repetitions."""
import argparse
import base64
from collections import Counter, defaultdict
import csv
from datetime import datetime
import gzip
import json
import math
from pathlib import Path
import re

import retctx_wire
import statistics

from prepare_dsb_sn_e2e import write_json


def uvarint(data, offset=0):
    result = 0
    for index in range(10):
        if offset + index >= len(data):
            raise ValueError('truncated varint')
        value = data[offset + index]
        if index == 9 and value > 1:
            raise ValueError('overflowing varint')
        result |= (value & 127) << (7 * index)
        if value < 128:
            return result, offset + index + 1
    raise ValueError('overflowing varint')


def mixed_radix_chunks(n):
    """Mirror structural_truss.go: digit runs whose radix product stays below 2**62."""
    chunks, start, product, limit = [], 0, 1, 1 << 62
    for i in range(n):
        radix = n - i
        if radix > limit // product and i > start:
            chunks.append((start, i))
            start, product = i, 1
        product *= radix
    if n:
        chunks.append((start, n))
    return chunks


def end_group(buf, offset, bound):
    header, offset = uvarint(buf, offset)
    count, lehmer = header >> 1, header & 1
    if count == 0:
        assert not lehmer, 'empty Lehmer group'
        return [], offset
    if not lehmer:
        ends = []
        for _ in range(count):
            value, offset = uvarint(buf, offset)
            ends.append(value)
        return ends, offset
    assert 0 < count <= bound, (count, bound)
    width = (bound + 7) // 8
    bitmap = buf[offset:offset + width]
    assert len(bitmap) == width, 'truncated end-event bitmap'
    offset += width
    members = [e for e in range(1, bound + 1) if bitmap[(e - 1) // 8] >> ((e - 1) % 8) & 1]
    assert len(members) == count, 'bitmap population differs from count'
    digits = [0] * count
    for lo, hi in mixed_radix_chunks(count):
        word, offset = uvarint(buf, offset)
        for i in range(hi - 1, lo - 1, -1):
            radix = count - i
            digits[i], word = word % radix, word // radix
        assert word == 0, 'Lehmer word out of range'
    remaining, ends = list(members), []
    for digit in digits:
        assert digit < len(remaining)
        ends.append(remaining.pop(digit))
    return ends, offset


def structural_tail(truss, offset):
    ha_len, offset = uvarint(truss, offset)
    ha, offset = truss[offset:offset + ha_len], offset + ha_len
    witnesses, pos = 0, 0
    while pos < len(ha):
        assert len(ha) >= pos + 9, 'truncated fan-out witness'
        _, pos = uvarint(ha, pos + 8)
        witnesses += 1
    count, offset = uvarint(truss, offset)
    ordinals, groups = [], []
    for _ in range(count):
        ordinal, offset = uvarint(truss, offset)
        ends, offset = end_group(truss, offset, ordinal - 1)
        ordinals.append(ordinal)
        groups.append(ends)
    delayed_count, offset = uvarint(truss, offset)
    delayed = []
    for _ in range(delayed_count):
        assert len(truss) >= offset + 24, 'truncated delayed truss'
        offset += 24
        children, offset = uvarint(truss, offset)
        ends, offset = end_group(truss, offset, children)
        delayed.append((children, ends))
    assert offset == len(truss), 'trailing bytes after S-Bridge tail'
    return {'ordinals': ordinals, 'end_events': groups, 'delayed': delayed, 'witnesses': witnesses}


def inspect_traces(path, kind):
    traces = json.load(gzip.open(path, 'rt'))['data']
    counts = Counter(traces=len(traces))
    windows, depths = Counter(), Counter()
    for trace in traces:
        spans = {s['spanID']: s for s in trace['spans']}
        origins = set()
        counts['spans'] += len(spans)
        for span in spans.values():
            tags = {t['key']: t['value'] for t in span.get('tags', [])}
            for ref in span.get('references', []):
                if ref['refType'] == 'CHILD_OF' and ref['traceID'] == trace['traceID']:
                    counts['missing_parent_references'] += ref['spanID'] not in spans
            payloads = retctx_wire.checkpoint_tags(span)
            if not payloads:
                continue
            counts['reverse_checkpoint_spans'] += 1
            # Tomislav-RetCtx: reads the packed `_rc` envelope and the legacy JSON
            # `bridges.checkpoint` one alike, so roots captured before 2026-09-19
            # still analyse unchanged.
            segments = [seg for payload in payloads for seg in retctx_wire.decode_envelope(payload)]
            for segment in segments:
                assert segment['kind'] == 'checkpoint.' + kind, segment['kind']
                assert segment['ttl'] is None, 'probability-mode segment contains a reverse TTL'
                raw = segment['data']
                assert len(raw) >= 10 and any(raw[:8]), 'missing origin or truss'
                depth, start = uvarint(raw, 8)
                assert depth > 0 and start < len(raw)
                origin = raw[:8].hex()
                assert origin not in origins, 'returned origin emitted more than once in a trace'
                origins.add(origin)
                counts['reverse_segments'] += 1
                counts['reverse_truss_bytes'] += len(raw) - start
                counts['missing_origin_spans'] += origin not in spans
                depths[str(depth)] += 1
                if kind in ('pb', 'cgpb', 'sb'):
                    truss = raw[start:]
                    truss_depth, pos = uvarint(truss)
                    assert truss_depth == depth, 'origin and truss depths differ'
                    assert len(truss) > pos + 8
                    distance = truss[pos + 8] + 1
                    assert 2 <= distance <= 6, distance
                    bloom_bits = math.ceil(-(distance - 1) * math.log(.0001) / math.log(2)**2)
                    bloom_bytes = math.ceil(bloom_bits / 8)
                    assert len(truss) >= pos + 9 + bloom_bytes, 'truncated window Bloom'
                    if kind == 'pb':
                        assert len(truss) == pos + 9 + bloom_bytes, 'PB Bloom differs from its intended CPD'
                    if kind == 'sb':
                        # Tomislav-RetCtx: CG core || varint(len HA) || HA || ordinals+end events || delayed.
                        tail = structural_tail(truss, pos + 9 + bloom_bytes)
                        assert 1 <= len(tail['ordinals']) <= distance, tail['ordinals']
                        counts['sb_end_events'] += sum(len(g) for g in tail['end_events'])
                        counts['sb_delayed_trusses'] += len(tail['delayed'])
                        counts['sb_fanout_witnesses'] += tail['witnesses']
                    windows[str(distance)] += 1
    return {'counts': dict(counts), 'returned_window_distances': dict(windows),
            'origin_depths': dict(depths)}


def verify_raw(point, result):
    text = (point / 'wrk.stdout').read_text()
    sent = re.search(r'^Sent (\d+) requests', text, re.M)
    completed = re.search(r'^\s*(\d+) requests in ', text, re.M)
    non_2xx = re.search(r'Non-2xx or 3xx responses:\s*(\d+)', text)
    assert sent and int(sent[1]) == result['sent_requests']
    assert completed and int(completed[1]) == result['completed_requests']
    assert (int(non_2xx[1]) if non_2xx else 0) == result['non_2xx_3xx']
    assert result['completed_requests'] <= result['sent_requests']
    # The short HDR table rounds to two decimal places in the displayed unit.
    tolerance = 5 if result['max_ms'] >= 1000 else .005
    assert result['p50_ms'] <= result['p95_ms'] + tolerance
    assert result['p95_ms'] <= result['p99_ms'] + tolerance
    assert result['p99_ms'] <= result['max_ms'] + tolerance
    stderr = (point / 'wrk.stderr').read_text()
    seeds = re.findall(r'init: final thread_id=(\d+), base_seed=(\d+)', stderr)
    assert len(seeds) == result['threads'], 'missing generator thread initialization'
    assert {int(s) for _, s in seeds} == {result['seed']}, seeds
    assert {int(t) for t, _ in seeds} == set(range(1, result['threads'] + 1))


def resource_deltas(before, after):
    # Kubelet snapshots bracket the request window; retain that wider interval.
    seconds = (datetime.fromisoformat(after['finished']) -
               datetime.fromisoformat(before['finished'])).total_seconds()
    groups = defaultdict(lambda: {'cpu_cores': 0., 'working_set_bytes': 0, 'max_pod_cpu_cores': 0.})
    for uid, old in before['cpu'].items():
        new = after['cpu'].get(uid)
        if new is None or new['cpu_ns'] < old['cpu_ns']:
            continue
        name = new['name']
        group = ('collector' if name.startswith('otelcol-') else
                 'backend' if name.startswith(('jaeger-', 'elasticsearch-')) else
                 'application' if '-service-' in name else 'database-cache')
        cores = (new['cpu_ns'] - old['cpu_ns']) / 1e9 / seconds
        groups[group]['cpu_cores'] += cores
        groups[group]['max_pod_cpu_cores'] = max(groups[group]['max_pod_cpu_cores'], cores)
        groups[group]['working_set_bytes'] += new['working_set_bytes']
    return dict(groups)


def periodic_deltas(before, after, kind):
    """Keep the periodic SDK/priority counter windows distinct from wrk timing."""
    fields = {
        'sdk': ('spans_received', 'spans_flushed', 'spans_sent', 'spans_dropped',
                'cp_received', 'cp_sent', 'cp_dropped', 'lp_received', 'lp_sent', 'lp_dropped',
                'send_deadline', 'send_unavailable', 'send_exhausted', 'send_canceled', 'send_other',
                'dee_dropped'),
        'reverse': ('leaf_rejects', 'checkpoints', 'received', 'local_checkpoints'),
        'priority': ('hp_admitted', 'lp_admitted', 'hp_refused', 'lp_refused', 'gc_count'),
    }
    totals = {key: {} for key in fields}
    missing, resets = [], []
    for pod in before['sdk'].keys() | after['sdk'].keys():
        groups = [('priority', '_processor_metrics')] if pod.startswith('otelcol-') and kind != 'v' else (
            [('sdk', '_processor_metrics'), ('reverse', 'BRIDGES_RT')] if '-service-' in pod else [])
        for group, log in groups:
            old = before['sdk'].get(pod, {}).get(log)
            new = after['sdk'].get(pod, {}).get(log)
            if old is None and new is None:
                if group != 'reverse':
                    missing.append(pod + '/' + group)
                continue
            if old is None or new is None:
                missing.append(pod + '/' + group)
                continue
            for field in fields[group]:
                if field not in old or field not in new:
                    continue
                delta = new[field] - old[field]
                if delta < 0:
                    resets.append(pod + '/' + field)
                else:
                    totals[group][field] = totals[group].get(field, 0) + delta
    return {'deltas': totals, 'missing_logs': missing, 'counter_resets': resets,
            'pod_restarts_changed': before['restarts'] != after['restarts']}


def analyze(root, partial=False):
    plan = json.loads((root / 'plan.json').read_text())
    expected = {(rep, kind, rate) for rep in range(1, plan['repetitions'] + 1)
                for kind in plan['variants'] for rate in plan['ramp_rates']}
    seen, rows, issues = set(), [], []
    for path in sorted((root / 'run').glob('*/rate-*/result.json')):
        if '-interrupted-' in path.parent.parent.name:
            continue
        result = json.loads(path.read_text())
        if 'kind' not in result:  # Snapshot collection is still in flight.
            continue
        key = (result['repetition'], result['kind'], result['offered_rps'])
        assert key in expected and key not in seen, key
        assert result['seed'] == plan['seeds'][key[0] - 1]
        assert result['offer_seconds'] == plan['seconds_per_rate']
        verify_raw(path.parent, result)
        seen.add(key)
        before = json.loads((path.parent / 'before/snapshot.json').read_text())
        after = json.loads((path.parent / 'after/snapshot.json').read_text())
        result['resources'] = resource_deltas(before, after)
        result['periodic_counters'] = periodic_deltas(before, after, result['kind'])
        # Use wrk's rate rather than its coarsely rounded duration display.
        result['successful_rps_from_display_duration'] = result['successful_rps']
        if result['completed_requests']:
            seconds = result['completed_requests'] / result['completed_rps']
            result['successful_rps'] = result['completed_rps'] * (
                1 - result['non_2xx_3xx'] / result['completed_requests'])
        else:
            seconds = result['wrk_seconds']
        result['sent_rps'] = result['sent_requests'] / seconds
        result['generator_cores'] = result['generator_cpu_seconds'] / result['wall_seconds']
        for field in ('snapshot_errors', 'counter_resets', 'restarts_changed'):
            if result[field]:
                issues.append({'point': list(key), 'field': field, 'value': result[field]})
        sample = path.parent / 'settled-traces.json.gz'
        result['trace_sample_stage'] = 'after-drain' if sample.exists() else 'immediate'
        if not sample.exists():
            sample = path.parent / 'sample-traces.json.gz'
        if sample.exists():
            result['trace_sample'] = inspect_traces(sample, result['kind'])
        else:
            issues.append({'point': list(key), 'field': 'trace_sample', 'value': 'not captured'})
        rows.append(result)
    if not partial:
        assert seen == expected, f'{len(expected - seen)} missing points'
        assert json.loads((root / 'run-complete.json').read_text())['passed']
        for rep, kind in {(r, k) for r, k, _ in expected}:
            run = root / 'run' / f'{rep:02d}-{kind}'
            complete = json.loads((run / 'complete.json').read_text())
            assert complete['points'] == len(plan['ramp_rates'])
            seed = (run / 'seed.log').read_text()
            assert 'Failed:' not in seed and re.findall(r'Succeeded:\s*(\d+)', seed) == ['962', '37624']
        for result in rows:
            if result['trace_sample_stage'] != 'after-drain':
                issues.append({'point': [result['repetition'], result['kind'], result['offered_rps']],
                               'field': 'settled_trace_sample', 'value': 'not captured'})
    groups = defaultdict(list)
    for row in rows:
        groups[row['kind'], row['offered_rps']].append(row)
    curves = []
    metrics = ('mean_ms', 'p50_ms', 'p95_ms', 'p99_ms', 'completed_rps', 'successful_rps',
               'sent_rps', 'generator_cores', 'non_2xx_3xx')
    for (kind, rate), points in sorted(groups.items()):
        item = {'kind': kind, 'offered_rps': rate, 'repetitions': len(points)}
        for metric in metrics:
            values = [p[metric] for p in points]
            item[metric] = {'mean': statistics.mean(values),
                            'sd': statistics.stdev(values) if len(values) > 1 else 0}
        curves.append(item)
    output = root / 'analysis'
    output.mkdir(exist_ok=True)
    write_json(output / 'audit.json', {'raw_verified_points': len(seen), 'expected_points': len(expected),
                                     'complete': seen == expected and not partial, 'issues': issues})
    write_json(output / 'points.json', rows)
    write_json(output / 'curves.json', curves)
    fields = ['kind', 'repetition', 'offered_rps', 'sent_requests', 'completed_requests', *metrics]
    with (output / 'points.csv').open('w') as stream:
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction='ignore')
        writer.writeheader()
        writer.writerows(rows)
    if not curves:
        return
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    plt.rcParams.update({'font.size': 8, 'axes.labelsize': 8, 'legend.fontsize': 7,
                         'xtick.labelsize': 7, 'ytick.labelsize': 7, 'pdf.fonttype': 42})
    fig, axes = plt.subplots(1, 3, figsize=(6.6, 2.25))
    colors = {'v': '#333333', 'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
    labels = {'v': 'Vanilla', 'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}
    for axis, metric, label in zip(axes, ('mean_ms', 'p99_ms', 'successful_rps'),
                                   ('Mean latency (ms)', 'p99 latency (ms)', 'Successful requests/s')):
        for kind in plan['variants']:
            data = sorted((p for p in curves if p['kind'] == kind), key=lambda p: p['offered_rps'])
            if not data:
                continue
            axis.errorbar([p['offered_rps']/1000 for p in data], [p[metric]['mean'] for p in data],
                          yerr=[p[metric]['sd'] for p in data], color=colors[kind],
                          marker='o', markersize=2, linewidth=.9, capsize=1.5, label=labels[kind])
        axis.set(xlabel='Offered rate (k requests/s)', ylabel=label)
        axis.grid(alpha=.2)
        axis.spines[['right', 'top']].set_visible(False)
    handles, labels = axes[0].get_legend_handles_labels()
    fig.legend(handles, labels, loc='upper center', ncol=4, frameon=False)
    fig.tight_layout(rect=(0, 0, 1, .9), pad=.5, w_pad=.8)
    for extension in ('pdf', 'svg', 'png'):
        fig.savefig(output / f'end-to-end.{extension}', dpi=300)
    plt.close(fig)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--partial', action='store_true')
    args = parser.parse_args()
    analyze(args.out.resolve(), args.partial)
