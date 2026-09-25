#!/usr/bin/env python3
"""Validate the fixed-6 follow-up and explain its byte comparison."""
from collections import defaultdict
import hashlib
import json
import re
import statistics

import analyze
import run_phase as run


def mean(traces, key):
    return statistics.mean(t[key] for t in traces)


def components(traces):
    return {
        'bloom': mean(traces, 'bloom_bytes'),
        'checkpoint_anchor_and_depth': 9 * mean(traces, 'checkpoints'),
        'window_descriptor': mean(traces, 'window_descriptor_bytes'),
        'ordinary_depth': mean(traces, 'ordinary_spans'),
        'branch_records': 9 * mean(traces, 'branch_records'),
    }


def verify_inputs(directory):
    pod = next(p['metadata']['name'] for p in run.pods()['items']
               if p['metadata']['name'].startswith('post-db-'))
    posts = json.loads((directory / 'new-posts.json').read_text())
    ids = [re.fullmatch(r'ObjectId\("([0-9a-f]{24})"\)', p['id']).group(1)
           for p in posts]
    js_ids = ','.join('ObjectId(' + json.dumps(oid) + ')' for oid in ids)
    js = ('var p=db.getSiblingDB("post").getCollection("post"); '
          'print(JSON.stringify(p.find({_id:{$in:[' + js_ids +
          ']}}).sort({_id:1}).toArray()));')
    documents = json.loads(run.kube('exec', pod, '--', 'mongo', '--quiet', '--eval', js))
    assert len(documents) == len(posts)
    run.save(directory / 'stored-posts.json', documents)
    hashes = []
    for post in documents:
        text = post['text']
        for url in sorted(post['urls'], key=lambda u: len(u['shortenedurl']), reverse=True):
            text = text.replace(url['shortenedurl'], url['expandedurl'])
        normalized = {
            'creator': post['creator'], 'text': text,
            'usermentions': sorted(post['usermentions'], key=lambda m: json.dumps(m, sort_keys=True)),
            'medias': sorted(post['medias'], key=lambda m: json.dumps(m, sort_keys=True)),
            'urls': sorted(u['expandedurl'] for u in post['urls']),
            'posttype': post['posttype'],
        }
        hashes.append(hashlib.sha256(json.dumps(normalized, sort_keys=True).encode()).hexdigest())
    run.save(directory / 'input-hashes.json', hashes)
    return hashes


def main():
    limit = run.SPEC['comparison_requests']
    fixed6, summary = analyze.analyze_phase(run.SPEC['phases'][0], limit=limit)
    traces = {'fixed6': fixed6}
    summaries = {'fixed6': summary}
    hashes = {'fixed6': verify_inputs(run.ROOT / 'fixed6')}
    for name in ['fixed2', 'fixed4', 'random2_6']:
        prior = json.loads((run.OLD / name / 'analysis.json').read_text())
        traces[name], summaries[name] = prior['traces'], prior['summary']
        hashes[name] = json.loads((run.OLD / name / 'input-hashes.json').read_text())
        assert len(traces[name]) == len(fixed6) == limit
        assert hashes[name][:limit] == hashes['fixed6'][:limit], name
        # Persisted JSON signatures contain lists; fresh analysis uses tuples.
        prior_trees = [[tuple(row) for row in t['tree_signature']] for t in traces[name]]
        current_trees = [[tuple(row) for row in t['tree_signature']] for t in fixed6]
        assert prior_trees == current_trees, name
    prior_ids = set()
    for name in ['fixed2', 'fixed4', 'random2_6']:
        prior_ids.update(p['post_id'] for p in json.loads((run.OLD / name / 'new-posts.json').read_text()))
    new_ids = {p['post_id'] for p in json.loads((run.ROOT / 'fixed6/new-posts.json').read_text())}
    assert prior_ids.isdisjoint(new_ids), 'Post IDs collide with previous measurements'
    by_root = defaultdict(list)
    for trace in traces['random2_6']:
        by_root[trace['root_draw_candidates'][0]].append(trace)
    byte_components = {name: components(items) for name, items in traces.items()}
    for name, values in byte_components.items():
        assert abs(sum(values.values()) - mean(traces[name], 'bridge_payload_bytes')) < 1e-9
    assert all(t['checkpoints'] == 9 and t['bridge_payload_bytes'] == 230 for t in fixed6)
    result = {
        'validated_at': run.stamp(), 'matched_requests_per_phase': limit,
        'inputs_and_call_graphs_match': True,
        'distinct_post_ids_across_measurements': True,
        'summaries': summaries,
        'byte_components': byte_components,
        'random_minus_fixed4': {
            key: byte_components['random2_6'][key] - byte_components['fixed4'][key]
            for key in byte_components['fixed4']},
        'random_grouped_by_root_draw': {
            distance: {
                'requests': len(items), 'checkpoints_per_request': mean(items, 'checkpoints'),
                'bridge_bytes_per_request': mean(items, 'bridge_payload_bytes'),
                'checkpoint_counts': sorted(set(t['checkpoints'] for t in items)),
            } for distance, items in sorted(by_root.items())},
        'byte_definition': 'Decoded _br and _d only; excludes forward baggage and other OTLP fields',
    }
    run.save(run.ROOT / 'comparison.json', result)
    run.save(run.ROOT / 'input-equivalence.json', {
        'matched': True, 'matched_requests_per_phase': limit,
        'all_measured_counts': {name: len(values) for name, values in hashes.items()},
        'hashes': hashes,
    })
    run.report('Fixed 6 measured 9 checkpoints and 230 bridge bytes per request; 99 request inputs and call graphs match all prior phases')


if __name__ == '__main__':
    main()
