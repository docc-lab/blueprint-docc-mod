#!/usr/bin/env python3
"""Tomislav-RetCtx: prepare isolated, reproducible Social Network ramp manifests.

Uses the existing wiring/build scripts; does not modify application services.
Preparation does not apply Kubernetes resources or start workload traffic.
"""
import argparse
import copy
import json
import os
from pathlib import Path
import subprocess

import yaml

REPO = Path(__file__).resolve().parents[1]
DSB = REPO / 'examples/dsb_sn'
KINDS = ('v', 'pb', 'cgpb', 'sb')
REGISTRY = '10.10.1.1:30000'


def write_json(path, value):
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(value, indent=2) + '\n')
    temporary.replace(path)


def command(argv, **kwargs):
    print('+', ' '.join(map(str, argv)), flush=True)
    return subprocess.run(list(map(str, argv)), check=True, **kwargs)


def environment(container, values):
    env = {item['name']: item for item in container.get('env', [])}
    env.update({key: {'name': key, 'value': str(value)} for key, value in values.items()})
    container['env'] = list(env.values())


def collector_config(kind, variant):
    # Tomislav-RetCtx: identical thresholds, batching and downstream export;
    # the bridge receiver must retain the SDK's bridges-priority metadata.
    receiver = {'protocols': {'grpc': {'endpoint': '0.0.0.0:4317', 'include_metadata': True}}}
    config = {
        'receivers': {'otlp': receiver},
        'processors': {'batch': {'send_batch_size': 8192, 'timeout': '200ms'}},
        'exporters': {'otlp': {
            'endpoint': f'jaeger-{variant}-ctr:4317', 'tls': {'insecure': True},
            'retry_on_failure': {'enabled': True, 'initial_interval': '5s',
                                 'max_interval': '30s', 'max_elapsed_time': '0s'},
            'sending_queue': {'enabled': True, 'num_consumers': 10,
                              'queue_size': 1000, 'block_on_overflow': False},
        }},
        'service': {
            'telemetry': {
                'logs': {'level': 'info'},
                'metrics': {'level': 'detailed', 'readers': [{'pull': {'exporter': {
                    'prometheus': {'host': '0.0.0.0', 'port': 8888}}}}]},
            },
            'pipelines': {'traces': {'receivers': ['otlp'], 'exporters': ['otlp']}},
        },
    }
    if kind == 'v':
        config['processors']['memory_limiter'] = {
            'check_interval': '100ms', 'limit_percentage': 70, 'spike_limit_percentage': 20}
        config['service']['pipelines']['traces']['processors'] = ['memory_limiter', 'batch']
    else:
        config['processors']['priority'] = {
            'check_interval': '100ms', 'soft_percentage': 50, 'hard_percentage': 70,
            'cp_safety_factor': 1, 'force_gc': True,
            'gc_soft_interval': '1s', 'gc_ultrasoft_interval': '0s',
        }
        config['receivers']['configdiscovery'] = {
            'endpoint': ':8080',
            'config_map': {'cpd_min': 2, 'cpd_max': 6, 'reverse_policy': 'inverse_depth'},
        }
        config['exporters']['debug/config'] = {'verbosity': 'basic'}
        config['service']['pipelines']['logs/configdiscovery'] = {
            'receivers': ['configdiscovery'], 'processors': [], 'exporters': ['debug/config']}
        config['service']['pipelines']['traces']['processors'] = ['priority', 'batch']
    return config


def configure_manifests(documents, kind, variant, collector_image, namespace):
    out = []
    config_name = f'otelcol-{variant}-config'
    config = collector_config(kind, variant)
    for original in documents:
        if not isinstance(original, dict):
            continue
        doc = copy.deepcopy(original)
        name = doc['metadata']['name']
        # The paper's workload does not issue auxiliary tracepressure calls.
        if name.startswith('tracepressure-'):
            continue
        doc['metadata']['namespace'] = namespace
        doc['metadata'].setdefault('labels', {})['retctx-e2e'] = variant
        if doc['kind'] == 'Service':
            if name.startswith('otelcol-'):
                doc['spec']['internalTrafficPolicy'] = 'Local'
            elif name.startswith('wrk2api-'):
                doc['spec']['type'] = 'NodePort'
                for port in doc['spec']['ports']:
                    if port['port'] == 2000:
                        port['nodePort'] = 23229
        if doc['kind'] in ('Deployment', 'DaemonSet', 'StatefulSet'):
            template = doc['spec']['template']
            template['metadata'].setdefault('labels', {})['retctx-e2e'] = variant
            pod = template['spec']
            if doc['kind'] == 'Deployment':
                doc['spec']['strategy'] = {'type': 'Recreate'}
                doc['spec']['replicas'] = 1
            for container in pod['containers']:
                if name.startswith('otelcol-'):
                    assert doc['kind'] == 'DaemonSet'
                    container['image'] = collector_image
                    container['args'] = ['--config=/etc/otelcol/e2e/config.yaml']
                    environment(container, {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0})
                    container['resources'] = {key: {'cpu': '500m', 'memory': '256Mi'}
                                              for key in ('requests', 'limits')}
                    container.setdefault('volumeMounts', []).append({
                        'name': 'e2e-config', 'mountPath': '/etc/otelcol/e2e', 'readOnly': True})
                    pod.setdefault('volumes', []).append({
                        'name': 'e2e-config', 'configMap': {'name': config_name}})
                    pod.setdefault('affinity', {})['nodeAffinity'] = {
                        'requiredDuringSchedulingIgnoredDuringExecution': {'nodeSelectorTerms': [{
                            'matchExpressions': [{'key': 'kubernetes.io/hostname', 'operator': 'In',
                                                  'values': [f'node-{i}' for i in range(1, 9)]}]}]}}
                elif '-service-' in name:
                    environment(container, {
                        'BRIDGE_KIND': kind, 'GOMAXPROCS': 8, 'GOGC': 100,
                        'GC_INTERVAL_SEC': 0, 'OTLP_RETRY': 'off', 'OTEL_SAMPLE_RATIO': 1,
                        'REVERSE_TRUSS': 'off' if kind == 'v' else 'on',
                        'RT_LEAF_REJECT': 0 if kind == 'v' else 1,
                        'RT_ROOT': 'off', 'RT_SAMPLE': 10000,
                    })
                    assert container['resources'] == {
                        key: {'cpu': '8000m'} for key in ('requests', 'limits')}, (name, container['resources'])
                ports = container.get('ports', [])
                if ports:
                    probe = {'tcpSocket': {'port': ports[0]['containerPort']}, 'periodSeconds': 5}
                    container.setdefault('readinessProbe', dict(probe, failureThreshold=3))
                    container.setdefault('startupProbe', dict(probe, failureThreshold=60))
            if not name.startswith('otelcol-'):
                assert pod.get('nodeSelector', {}).get('kubernetes.io/hostname'), name
        out.append(doc)
    out.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {
        'name': config_name, 'namespace': namespace, 'labels': {'retctx-e2e': variant}},
        'data': {'config.yaml': yaml.safe_dump(config, sort_keys=False)}})
    assert sum(d['kind'] == 'Deployment' for d in out) == 25
    assert sum(d['kind'] == 'DaemonSet' for d in out) == 1
    return out


def prepare(root, namespace, resume=False, kinds=KINDS):
    build_status = json.loads((root / 'collector-build-status.json').read_text())
    assert build_status['state'] == 'complete', 'collector build must finish first'
    image = json.loads(command(['docker', 'image', 'inspect', f'{REGISTRY}/otelcontribcol:latest'],
                               capture_output=True, text=True).stdout)[0]
    collector_image = next(d for d in image['RepoDigests'] if d.startswith(REGISTRY + '/otelcontribcol@'))
    stamp = root.name.rsplit('-', 1)[1].lower()
    extra = 'rtx' + stamp
    cases = []
    for kind in kinds:
        build_name = f'{kind}_e2e_{stamp}'
        build = DSB / ('build_' + build_name)
        case = root / 'builds' / kind
        argv = [REPO / 'utils/build_deploy_dsb.sh', '-s', f'docker_{kind}_es', '-n', build_name,
                '--extra', extra, '--gc', 'natural', '--anti-affinity', '--wrk2api-deploy', '--skip-build']
        if kind != 'v':
            argv += ['--cpd-min', '2', '--cpd-max', '6', '--reverse-policy', 'inverse_depth']
        write_json(root / 'prepare-status.json', {'state': 'running', 'kind': kind})
        env = dict(os.environ, OTEL_SAMPLE_RATIO='1')
        if build.exists():
            # Only reuse a generated build owned by this preparation attempt.
            assert resume and (case / 'manifest.yaml').exists(), f'refusing to overwrite {build}'
        else:
            with (root / 'logs' / f'prepare-{kind}.log').open('w') as log:
                command(argv, cwd=REPO, env=env, stdout=log, stderr=subprocess.STDOUT)
        compose = yaml.safe_load((build / 'docker/docker-compose.yml').read_text())
        service = next(s for s in compose['services'] if s.startswith('otelcol_'))
        variant = service[len('otelcol_'):-len('_ctr')].replace('_', '-')
        documents = [d for p in sorted((build / 'k8s').glob('*.yaml'))
                     for d in yaml.safe_load_all(p.read_text())]
        documents = configure_manifests(documents, kind, variant, collector_image, namespace)
        case.mkdir(exist_ok=resume)
        (case / 'manifest.yaml').write_text(yaml.safe_dump_all(documents, sort_keys=False))
        (case / 'collector.yaml').write_text(yaml.safe_dump(collector_config(kind, variant), sort_keys=False))
        command(['docker', 'run', '--rm', '-v', f'{case}/collector.yaml:/config.yaml:ro',
                 collector_image, 'validate', '--config=/config.yaml'])
        entry = {'kind': kind, 'variant': variant, 'build': str(build), 'case': str(case),
                 'collector_image': collector_image, 'namespace': namespace}
        write_json(case / 'case.json', entry)
        cases.append(entry)
        write_json(root / 'cases.json', cases)
    write_json(root / 'prepare-status.json', {'state': 'complete', 'cases': len(cases)})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--namespace', default='dsb-sn')
    parser.add_argument('--resume', action='store_true')
    parser.add_argument('--kinds', help='comma-separated subset of v,pb,cgpb,sb (default: all)')
    args = parser.parse_args()
    kinds = tuple(k for k in (args.kinds or '').split(',') if k) or KINDS
    try:
        prepare(args.out.resolve(), args.namespace, args.resume, kinds)
    except Exception as error:
        write_json(args.out / 'prepare-status.json', {'state': 'failed', 'error': str(error)})
        raise
