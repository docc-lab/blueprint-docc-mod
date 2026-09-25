"""Prepare this social-network deployment and build its exact manifest images."""
import json
from pathlib import Path
import shutil
import sys
import time

import yaml

REPO = Path('/users/tomislav/blueprint-docc-mod')
BUILD = REPO / 'examples/dsb_sn/build_cgpb_es_k8s_20260910'
OUTPUT = Path(__file__).resolve().parent
VARIANT = 'cgpb-es-sn20260910'
NAMESPACE = 'dsb-sn'
sys.path.insert(0, str(REPO / 'utils'))
from build_deploy_hotel import build_images, env_map


def read(path):
    return yaml.safe_load(path.read_text())


def write(path, value):
    path.write_text(yaml.safe_dump(value, sort_keys=False))


docker = BUILD / 'docker'
manifests = BUILD / 'k8s'
compose = read(docker / 'docker-compose.yml')
settings = dict(BRIDGE_KIND='cgpb', GOGC='100', GC_INTERVAL_SEC='0',
                OTLP_RETRY='off', OTEL_SAMPLE_RATIO='1',
                REVERSE_TRUSS='off', RT_ROOT='off')
for name, service in compose['services'].items():
    if '_service_' in name:
        service['environment'] = {**env_map(service.get('environment')), **settings}
write(docker / 'docker-compose.yml', compose)

collector = docker / 'otelcol_cgpb_es_sn20260910_ctr/config.yaml'
config = read(collector)
config['receivers']['otlp']['protocols']['grpc']['include_metadata'] = True
config['receivers'].pop('otlp/highprio', None)
config['service']['telemetry']['logs']['level'] = 'info'
config['exporters']['debug']['verbosity'] = 'basic'
write(collector, config)

revision = str(time.time_ns())
workloads = []
for path in sorted(manifests.glob('*.yaml')):
    doc = read(path)
    name = doc['metadata']['name']
    doc['metadata']['namespace'] = NAMESPACE
    labels = {'app.kubernetes.io/part-of': 'dsb-sn',
              'app.kubernetes.io/instance': VARIANT}
    doc['metadata'].setdefault('labels', {}).update(labels)
    if doc['kind'] in ('Deployment', 'DaemonSet'):
        workloads.append(name)
        template = doc['spec']['template']
        template['metadata'].setdefault('labels', {}).update(labels)
        template['metadata'].setdefault('annotations', {})['blueprint.uservices/build'] = revision
        if doc['kind'] == 'Deployment':
            doc['spec']['strategy'] = {'type': 'Recreate'}
        if name.startswith('tracepressure-service-'):
            template['spec']['nodeSelector'] = {'kubernetes.io/hostname': 'node-1'}
        for container in template['spec']['containers']:
            container['imagePullPolicy'] = 'Always'
            if '-service-' in name:
                env = {entry['name']: entry for entry in container.get('env', [])}
                env.update({key: {'name': key, 'value': value} for key, value in settings.items()})
                container['env'] = list(env.values())
            port = 4317 if name.startswith('otelcol-') else container['ports'][0]['containerPort']
            container['startupProbe'] = {'tcpSocket': {'port': port},
                                         'periodSeconds': 2, 'failureThreshold': 150}
            container['readinessProbe'] = {'tcpSocket': {'port': port}, 'periodSeconds': 5}
    if doc['kind'] == 'Service' and name == f'wrk2api-service-{VARIANT}-ctr':
        doc['spec']['type'] = 'NodePort'
    write(path, doc)

for name in ('docker', 'k8s'):
    target = OUTPUT / name
    if not target.exists():
        target.symlink_to(BUILD / name, target_is_directory=True)
shutil.copy2(REPO / 'examples/dsb_sn/node-pinning-cgpb_es_k8s_20260910.yaml',
             OUTPUT / 'node-pinning.yaml')
(OUTPUT / 'build.json').write_text(json.dumps({
    'build': str(BUILD), 'namespace': NAMESPACE, 'variant': VARIANT,
    'image_tag': 'social-20260910', 'settings': settings,
    'cpd': 2, 'collector': 'passthrough', 'workloads': workloads,
    'placement': 'anti-affinity',
}, indent=2) + '\n')

build_images(compose, docker, manifests)
print('All social-network images built and pushed successfully.', flush=True)
