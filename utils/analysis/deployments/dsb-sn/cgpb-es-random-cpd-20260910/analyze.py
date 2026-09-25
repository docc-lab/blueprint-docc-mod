#!/usr/bin/env python3
"""Decode exported CGPB payloads and compare them with the native span tree."""
import argparse
import base64
from collections import Counter, defaultdict
import csv
import json
import math
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parent
SPEC = json.loads((ROOT / 'experiment.json').read_text())


def varint(raw, offset=0):
    value = 0
    for shift in range(0, 70, 7):
        b = raw[offset]
        offset += 1
        value |= (b & 127) << shift
        if b < 128:
            return value, offset
    raise ValueError('Invalid uvarint')


def expected_bloom(ids, m, k):
    result = bytearray((m + 7) // 8)
    for sid in ids:
        raw = bytes.fromhex(sid)
        h1, h2 = int.from_bytes(raw[:4], 'big'), int.from_bytes(raw[4:], 'big')
        for i in range(k):
            bit = ((h1 + i * h2) & ((1 << 64) - 1)) % m
            result[bit // 8] |= 1 << (bit % 8)
    return bytes(result)


def percentile(values, q):
    values = sorted(values)
    index = (len(values) - 1) * q
    low, high = math.floor(index), math.ceil(index)
    return values[low] + (values[high] - values[low]) * (index - low)


def analyze_trace(trace, phase):
    spans = {s['spanID']: s for s in trace['spans']}
    assert len(spans) == len(trace['spans']), 'Duplicate span IDs'
    parent = {}
    children = defaultdict(list)
    tags = {}
    for sid, span in spans.items():
        refs = [r for r in span['references'] if r['refType'] == 'CHILD_OF']
        assert len(refs) <= 1
        if refs:
            assert refs[0]['traceID'] == trace['traceID']
            parent[sid] = refs[0]['spanID']
            assert parent[sid] in spans, 'Missing parent'
            children[parent[sid]].append(sid)
        tags[sid] = {t['key']: t['value'] for t in span['tags']}
        assert not tags[sid].get('error'), (trace['traceID'], sid, tags[sid])
        assert tags[sid].get('otel.status_code') != 'ERROR'
    roots = set(spans) - set(parent)
    assert len(roots) == 1, roots
    root = roots.pop()
    assert spans[root]['operationName'] == 'Wrk2APIServiceServer_ComposePost'
    depths = {root: 0}

    def depth(sid, active=None):
        if sid not in depths:
            active = set() if active is None else active
            assert sid not in active, 'Parent cycle'
            active.add(sid)
            depths[sid] = depth(parent[sid], active) + 1
        return depths[sid]

    checkpoints = {sid for sid in spans if '_br' in tags[sid]}
    assert root in checkpoints
    leaves = {sid for sid in spans if not children[sid]}
    assert all(tags[sid].get('span.kind') == 'server' for sid in leaves)
    assert leaves <= checkpoints, 'Server leaf failed to checkpoint'
    interior_checkpoints = checkpoints - leaves - {root}
    config = phase['config_map']
    low = config.get('cpd_min', config.get('cpd'))
    high = config.get('cpd_max', config.get('cpd'))
    capacity = max(1, high - 1)
    m = math.ceil(-capacity * math.log(0.0001) / math.log(2) ** 2)
    k = math.ceil(m / capacity * math.log(2))
    bloom_bytes = (m + 7) // 8
    rows = []
    gaps = Counter()
    leaf_gaps = Counter()
    checkpoint_children = defaultdict(list)
    signature = []
    payload_bytes = 0
    branch_records = 0
    nonbranch_records = []
    for sid, span in spans.items():
        d = depth(sid)
        row = {'trace_id': trace['traceID'], 'span_id': sid, 'parent_span_id': parent.get(sid, ''),
               'operation': span['operationName'], 'depth': d,
               'checkpoint': sid in checkpoints, 'leaf': sid in leaves}
        ancestors = []
        cursor = sid
        while cursor in parent:
            cursor = parent[cursor]
            ancestors.append(cursor)
        signature.append((span['operationName'], d,
                          spans[parent[sid]]['operationName'] if sid in parent else None))
        key = '_br' if sid in checkpoints else '_d'
        assert ('_br' in tags[sid]) != ('_d' in tags[sid]), tags[sid]
        raw = base64.b64decode(tags[sid][key], validate=True)
        encoded_depth, offset = varint(raw)
        assert encoded_depth == d, ('Encoded depth differs from parent tree', row, encoded_depth)
        payload_bytes += len(raw)
        row['payload_bytes'] = len(raw)
        if key == '_br':
            assert len(raw) >= offset + 8 + bloom_bytes
            anchor = raw[offset:offset + 8].hex()
            bloom = raw[offset + 8:offset + 8 + bloom_bytes]
            ha = raw[offset + 8 + bloom_bytes:]
            nearest = next((a for a in ancestors if a in checkpoints), '0000000000000000')
            assert anchor == nearest, ('Wrong checkpoint anchor', row, anchor, nearest)
            intervening = ancestors[:ancestors.index(anchor)] if sid != root else []
            assert bloom == expected_bloom(intervening, m, k), ('Bloom window mismatch', row)
            row['anchor_span_id'] = anchor
            if sid != root:
                gap = d - depth(anchor)
                row['checkpoint_gap'] = gap
                checkpoint_children[anchor].append((sid, gap))
                assert 1 <= gap <= high
                if sid in interior_checkpoints:
                    assert low <= gap <= high, ('Scheduled checkpoint outside range', row)
                    gaps[gap] += 1
                else:
                    leaf_gaps[gap] += 1
            offset = 0
            entries = []
            while offset < len(ha):
                assert offset + 8 < len(ha)
                branch = ha[offset:offset + 8].hex()
                offset += 8
                branch_depth, offset = varint(ha, offset)
                assert branch in ancestors
                assert branch_depth == depth(branch) + 1
                if len(children[branch]) < 2:
                    nonbranch_records.append({'emitting_span_id': sid,
                        'named_span_id': branch, 'named_operation': spans[branch]['operationName'],
                        'actual_children': len(children[branch])})
                entries.append({'branch_span_id': branch, 'child_depth': branch_depth})
            row['branch_records'] = entries
            branch_records += len(entries)
        else:
            assert len(raw) == offset, 'Unexpected bytes in ordinary depth payload'
        if low == high:
            assert (sid in checkpoints) == (d % low == 0 or sid in leaves)
        rows.append(row)

    # Non-leaf checkpoints reveal the distance selected by their anchor. A
    # terminal leaf alone only provides a lower bound, because it checkpoints
    # even before the incoming countdown expires.
    draws = []
    for anchor, descendants in checkpoint_children.items():
        complete = {gap for sid, gap in descendants if sid in interior_checkpoints}
        assert len(complete) <= 1, 'Sibling paths disagreed on their anchor countdown'
        if complete:
            candidates = list(complete)
            assert all(gap <= candidates[0] for _, gap in descendants)
        else:
            candidates = list(range(max(low, max(gap for _, gap in descendants)), high + 1))
        assert candidates
        draws.append({'anchor_span_id': anchor, 'anchor_depth': depth(anchor),
                      'candidates': candidates, 'observed_nonleaf_checkpoint': bool(complete)})
    root_draw = next(draw for draw in draws if draw['anchor_span_id'] == root)
    result = {'trace_id': trace['traceID'], 'root_start_us': spans[root]['startTime'],
              'spans': len(spans), 'deepest_path_spans': max(depths.values()) + 1,
              'checkpoints': len(checkpoints), 'root_checkpoints': 1,
              'leaf_checkpoints': len(leaves), 'interior_checkpoints': len(interior_checkpoints),
              'ordinary_spans': len(spans) - len(checkpoints),
              'bridge_payload_bytes': payload_bytes, 'branch_records': branch_records,
              'ha_records_naming_nonbranch_spans': nonbranch_records,
              'root_latency_ms': spans[root]['duration'] / 1000,
              'scheduled_interior_gaps': dict(gaps), 'leaf_gaps': dict(leaf_gaps),
              'root_draw_candidates': root_draw['candidates'], 'draws': draws,
              'tree_signature': sorted(signature), 'rows': rows}
    return result


def analyze_phase(phase):
    directory = ROOT / phase['name']
    traces = json.loads((directory / 'traces.json').read_text())
    result = json.loads((directory / 'result.json').read_text())
    analyzed = sorted((analyze_trace(t, phase) for t in traces), key=lambda t: t['root_start_us'])
    assert len(analyzed) == result['requests'] == 100
    gaps, leaf_gaps, root_draws = Counter(), Counter(), Counter()
    for t in analyzed:
        gaps.update(t['scheduled_interior_gaps'])
        leaf_gaps.update(t['leaf_gaps'])
        if len(t['root_draw_candidates']) == 1:
            root_draws[t['root_draw_candidates'][0]] += 1
    latencies = [t['root_latency_ms'] for t in analyzed]
    total_spans = sum(t['spans'] for t in analyzed)
    total_checkpoints = sum(t['checkpoints'] for t in analyzed)
    summary = {'phase': phase['name'], 'config': phase['config_map'], 'requests': len(analyzed),
               'total_spans': total_spans, 'spans_per_trace': dict(Counter(t['spans'] for t in analyzed)),
               'deepest_path_spans': max(t['deepest_path_spans'] for t in analyzed),
               'total_checkpoints': total_checkpoints,
               'checkpoints_per_request': total_checkpoints / len(analyzed),
               'checkpoint_fraction': total_checkpoints / total_spans,
               'root_checkpoints_per_request': 1,
               'leaf_checkpoints_per_request': statistics.mean(t['leaf_checkpoints'] for t in analyzed),
               'interior_checkpoints_per_request': statistics.mean(t['interior_checkpoints'] for t in analyzed),
               'bridge_payload_bytes_per_request': statistics.mean(t['bridge_payload_bytes'] for t in analyzed),
               'checkpoint_count_distribution': dict(Counter(t['checkpoints'] for t in analyzed)),
               'scheduled_interior_gap_counts': dict(sorted(gaps.items())),
               'leaf_gap_counts': dict(sorted(leaf_gaps.items())),
               'inferred_root_draw_counts': dict(sorted(root_draws.items())),
               'root_latency_median_ms': statistics.median(latencies),
               'root_latency_p95_ms': percentile(latencies, .95),
               'root_latency_max_ms': max(latencies),
               'ha_records_naming_nonbranch_spans': sum(len(t['ha_records_naming_nonbranch_spans']) for t in analyzed),
               'validated': ['complete parent tree', 'all server leaves checkpointed',
                   'encoded absolute depth', 'nearest checkpoint anchor', 'exact Bloom window bytes',
                   'branch record ancestry and depth', 'scheduled interior gap range',
                   'shared countdown across siblings', 'no error span flags']}
    (directory / 'analysis.json').write_text(json.dumps({'summary': summary, 'traces': analyzed}, indent=2) + '\n')
    with (directory / 'span-details.csv').open('w') as stream:
        fields = ['trace_id', 'span_id', 'parent_span_id', 'operation', 'depth', 'checkpoint',
                  'leaf', 'anchor_span_id', 'checkpoint_gap', 'payload_bytes']
        writer = csv.DictWriter(stream, fields, extrasaction='ignore')
        writer.writeheader()
        writer.writerows(row for trace in analyzed for row in trace['rows'])
    print(json.dumps(summary, indent=2))
    return analyzed, summary


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=[p['name'] for p in SPEC['phases']])
    args = parser.parse_args()
    phases = [p for p in SPEC['phases'] if not args.phase or args.phase == p['name']]
    results = [analyze_phase(p) for p in phases]
    if len(results) == 3:
        signatures = [[t['tree_signature'] for t in traces] for traces, _ in results]
        assert signatures[0] == signatures[1] == signatures[2], 'Workload call graphs differ across phases'
        (ROOT / 'comparison.json').write_text(json.dumps([s for _, s in results], indent=2) + '\n')
        print('All 300 request trees match across phases in request order.')


if __name__ == '__main__':
    main()
