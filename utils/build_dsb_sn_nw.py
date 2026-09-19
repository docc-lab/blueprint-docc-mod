#!/usr/bin/env python3
"""Tomislav-RetCtx: build the zero-work Social Network images and pin deployed digests.

Mirrors build_dsb_sn_e2e.py for the no-work cases: one image set per bridge kind
(both sampling tiers of a kind share images; only environment differs), backend
images reused by digest from the pinned real-work e2e manifests, every deployed
reference pinned by digest, manifest hash recorded for the runner.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

import yaml

from prepare_dsb_sn_e2e import command, write_json, REGISTRY
from build_dsb_sn_e2e import registry_digest, prepare_dockerfile, tree_hash


def backend_digests(pinned_manifest):
    backends = {}
    for doc in yaml.safe_load_all(pinned_manifest.read_text()):
        if not doc or doc.get('kind') != 'Deployment':
            continue
        name = doc['metadata']['name']
        if '-service-' in name or name.startswith('otelcol-'):
            continue
        image = doc['spec']['template']['spec']['containers'][0]['image']
        assert '@sha256:' in image, image
        backends[re.sub(r'-(?:cgpb|pb|sb|v)-es.*-ctr$', '', name)] = image
    assert len(backends) == 12, sorted(backends)  # 5 mongo, 5 redis, jaeger, elasticsearch
    return backends


def build(root, pinned_manifest):
    assert json.loads((root / 'prepare-status.json').read_text())['state'] == 'complete'
    cases = json.loads((root / 'cases.json').read_text())
    backends = backend_digests(pinned_manifest)
    bases_path = root / 'base-image-digests.json'
    bases = json.loads(bases_path.read_text()) if bases_path.exists() else {}
    total = 13 * len({c['kind'] for c in cases})
    completed = 0
    for case in cases:
        kind, variant = case['kind'], case['variant']
        directory = Path(case['case'])
        manifest = directory / 'manifest.yaml'
        documents = list(yaml.safe_load_all(manifest.read_text()))
        compose_dir = Path(case['build']) / 'docker'
        compose = yaml.safe_load((compose_dir / 'docker-compose.yml').read_text())
        images_path = Path(case['image_case']) / 'images.json'  # shared across sampling tiers
        images = json.loads(images_path.read_text()) if images_path.exists() else {}
        app_count = 0
        for doc in documents:
            if doc['kind'] not in ('Deployment', 'DaemonSet'):
                continue
            name = doc['metadata']['name']
            for container in doc['spec']['template']['spec']['containers']:
                if name.startswith('otelcol-'):
                    assert '@sha256:' in container['image']
                elif '-service-' not in name:
                    base = name.removesuffix('-' + variant + '-ctr')
                    container['image'] = backends[base]
                else:
                    app_count += 1
                    service = next(s for s in compose['services'] if s.replace('_', '-') == name)
                    configuration = compose['services'][service]
                    context = configuration['build']
                    if isinstance(context, dict):
                        context = context['context']
                    context = compose_dir / context
                    prepare_dockerfile(context / 'Dockerfile', bases)
                    write_json(bases_path, bases)
                    digest = tree_hash(context)
                    if name not in images:
                        image = f'{REGISTRY}/{name}:latest'
                        write_json(root / 'image-build-status.json', {
                            'state': 'running', 'case': case['name'], 'service': name,
                            'completed_app_images': completed, 'total_app_images': total})
                        with (root / 'logs' / f'build-{name}.log').open('w') as log:
                            command(['docker', 'build', '--progress=plain', '-t', image, context],
                                    env=dict(os.environ, DOCKER_BUILDKIT='1'), stdout=log, stderr=subprocess.STDOUT)
                            command(['docker', 'push', image], stdout=log, stderr=subprocess.STDOUT)
                        frozen = registry_digest(image)
                        info = json.loads(command(['docker', 'image', 'inspect', image],
                                                   capture_output=True, text=True).stdout)[0]
                        assert frozen in info['RepoDigests']
                        images[name] = {'image': frozen, 'image_id': info['Id'], 'context_sha256': digest}
                        write_json(images_path, images)
                        completed += 1
                    assert images[name]['context_sha256'] == digest, f'build context changed: {name}'
                    container['image'] = images[name]['image']
                container['imagePullPolicy'] = 'IfNotPresent'
        assert app_count == 13, (case['name'], app_count)
        manifest.write_text(yaml.safe_dump_all(documents, sort_keys=False))
        assert all('@sha256:' in c['image'] for d in documents
                   if d['kind'] in ('Deployment', 'DaemonSet')
                   for c in d['spec']['template']['spec']['containers'])
        write_json(directory / 'build-complete.json', {
            'app_images': app_count, 'manifest_sha256': hashlib.sha256(manifest.read_bytes()).hexdigest()})
    write_json(root / 'image-build-status.json', {'state': 'complete', 'app_images': completed,
                                                  'total_app_images': total})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--backend-manifest', type=Path, required=True,
                        help='pinned real-work manifest supplying backend image digests')
    args = parser.parse_args()
    try:
        build(args.out.resolve(), args.backend_manifest.resolve())
    except Exception as error:
        write_json(args.out / 'image-build-status.json', {'state': 'failed', 'error': str(error)})
        raise
