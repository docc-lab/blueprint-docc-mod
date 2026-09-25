#!/usr/bin/env python3
"""Archive live manifests and verify the completed experiment without requests."""
import copy
import hashlib
import json
import math
import re

import yaml
import run_phase as run

root = run.ROOT
manifest = root / 'k8s'
manifest.mkdir(exist_ok=True)
unchanged = []
for kind, selected in [('deployments', set(run.APPS)), ('daemonsets', {run.DS})]:
    final = json.loads(run.kube('get', kind, '-o', 'json'))
    run.save(root / f'final-{kind}.json', final)
    before = {x['metadata']['name']: x for x in json.loads((root / f'before-{kind}.json').read_text())['items']}
    assert set(before) == {x['metadata']['name'] for x in final['items']}
    for item in final['items']:
        name = item['metadata']['name']
        old_spec, new_spec = copy.deepcopy(before[name]['spec']), copy.deepcopy(item['spec'])
        if name in selected:
            for spec in [old_spec, new_spec]:
                for container in spec['template']['spec']['containers']:
                    container.pop('image')
        assert old_spec == new_spec, (name, 'Unexpected workload change')
        unchanged.append(name)
        if name not in selected:
            continue
        clean = {key: copy.deepcopy(item[key]) for key in ['apiVersion', 'kind', 'metadata', 'spec']}
        meta = {key: value for key, value in clean['metadata'].items()
                if key in ['name', 'namespace', 'labels', 'annotations']}
        for key in ['kubectl.kubernetes.io/last-applied-configuration', 'deployment.kubernetes.io/revision']:
            meta.get('annotations', {}).pop(key, None)
        if not meta.get('annotations'):
            meta.pop('annotations', None)
        clean['metadata'] = meta
        (manifest / (name + '.yaml')).write_text(yaml.safe_dump(clean, sort_keys=False))

services = json.loads(run.kube('get', 'services', '-o', 'json'))
run.save(root / 'final-services.json', services)
old = {x['metadata']['name']: x['spec'] for x in json.loads((root / 'before-services.json').read_text())['items']}
assert old == {x['metadata']['name']: x['spec'] for x in services['items']}
for path, expected in json.loads((root / 'workflow-source-hashes.json').read_text()).items():
    assert hashlib.sha256((run.REPO / path).read_bytes()).hexdigest() == expected, path

post_ids, warmups, sdk, app_images = [], {}, {}, []
for phase in run.SPEC['phases']:
    directory = root / phase['name']
    result = json.loads((directory / 'result.json').read_text())
    posts = json.loads((directory / 'new-posts.json').read_text())
    assert len(posts) == result['requests']
    post_ids.extend(post['post_id'] for post in posts)
    before = json.loads((directory / 'posts-before.json').read_text())
    warmup = json.loads((directory / 'warmup-run.json').read_text())
    lo, hi = warmup['started_us'] // 1000000, math.ceil(warmup['ended_us'] / 1000000)
    warm = [post for post in before if lo <= int(re.search(r'[0-9a-f]{24}', post['id']).group(0)[:8], 16) < hi]
    assert len(warm) == len({post['post_id'] for post in warm}) == 5
    warmups[phase['name']] = len(warm)
    logs = json.loads((directory / 'log-check.json').read_text())
    sdk[phase['name']] = {key: sum(value['sdk'].get(key, 0) for value in logs.values())
                         for key in ['spans_received', 'spans_sent', 'spans_dropped']}
    expected_spans = (result['requests'] + len(warm)) * 23
    assert sdk[phase['name']] == {'spans_received': expected_spans, 'spans_sent': expected_spans, 'spans_dropped': 0}, sdk
    health = json.loads((directory / 'health-after.json').read_text())
    app_images.append({item['image']: item['image_id'] for item in health['images'] if '-service-' in item['image']})
assert len(post_ids) == len(set(post_ids))
assert app_images[0] == app_images[1] == app_images[2]
monitor = json.loads((root / 'monitoring/latest.json').read_text())
assert not monitor['alerts'] and monitor['ready_pods'] == monitor['total_pods'] == 35
assert monitor['total_restarts'] == 0
verification = {'verified_at': run.stamp(), 'unchanged_workload_specs_except_images': len(unchanged),
    'unchanged_service_specs': len(old), 'service_source_hashes_unchanged': True,
    'identical_application_image_digests_across_phases': True,
    'measured_posts': len(post_ids), 'globally_distinct_measured_post_ids': len(set(post_ids)),
    'warmup_posts': warmups, 'sdk': sdk,
    'monitor': {key: monitor.get(key) for key in ['timestamp', 'cycle', 'ready_pods', 'total_pods', 'total_restarts', 'api', 'alerts']}}
run.save(root / 'final-verification.json', verification)
print(json.dumps(verification, indent=2))
