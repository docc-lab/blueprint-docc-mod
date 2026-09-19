#!/usr/bin/env python3
"""Tomislav-RetCtx: prepare the zero-work ("no-work") Social Network ramp manifests.

Paper section 5.4 / July 2026 campaign regime: the `snnw` workflow (identical call
graph, no database/cache/compute work), passthrough collectors (batch only) at
1 CPU / 4 GiB per node, application services at 8 cores / GOMAXPROCS=8, a
no-tracing baseline (`nt`), and every traced variant at 100 % and 10 % head
sampling. Bridge settings match the real-work e2e run: CPD uniform 2..6,
inverse_depth reverse returns, unscheduled-leaf rejection.

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

from prepare_dsb_sn_e2e import REPO, DSB, REGISTRY, write_json, command, environment

KINDS = ('nt', 'v', 'pb', 'cgpb', 'sb')
BRIDGES = ('pb', 'cgpb', 'sb')
# (kind, head-sampling ratio) -> case name. Order is the campaign order (N=1).
CASES = [(kind, 1.0) for kind in KINDS] + [(kind, 0.1) for kind in ('v', 'pb', 'cgpb', 'sb')]
# Tomislav-RetCtx: two collector profiles.
#  passthrough - batch only, nothing sheds on purpose (original 5.4 overhead regime).
#  admission   - the real-work e2e pipeline: memory_limiter (vanilla / no-tracing) or the
#                priority processor (bridges), so shedding is explicit, attributable and
#                checkpoint-aware instead of the backpressure artefact the passthrough runs
#                produced. Memory settings are the e2e ones verbatim (256 Mi, GOMEMLIMIT
#                230 MiB, soft 50 / hard 70) so the admission decisions are identical; CPU
#                stays at the no-work 1 core because e2e's 500m starves the collector at
#                no-work span rates and CPU starvation sheds by backpressure, not by policy.
COLLECTOR_PROFILES = {
    'passthrough': {'resources': {'cpu': '1', 'memory': '4Gi'},
                    'env': {'GOMEMLIMIT': '3276MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    'admission': {'resources': {'cpu': '1', 'memory': '256Mi'},
                  'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # admission2g: same processors and thresholds, larger memory budget. At 256 Mi the zero-work
    # workload (~10x the real-work request rate into the same budget) starts refusing at offered
    # 1,000, one sixth of the knee, and the application then waits on the export path instead of
    # saturating (20 of ~104 application cores in use at offered 9,000). 2 GiB moves the onset near
    # the knee so the application stays the bottleneck where the ramp is read.
    'admission2g': {'resources': {'cpu': '1', 'memory': '2Gi'},
                    'env': {'GOMEMLIMIT': '1843MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # sink: accept every span and discard it immediately (nop exporter, no store). Nothing is
    # ever refused and nothing can backpressure, so the application runs at full export load
    # against a collector that always answers OK. Isolates instrumentation cost, and is the
    # control that tells us whether refusals are what slow the request path.
    'sink': {'resources': {'cpu': '1', 'memory': '4Gi'},
             'env': {'GOMEMLIMIT': '3276MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
}
COLLECTOR_RESOURCES = COLLECTOR_PROFILES['passthrough']['resources']
COLLECTOR_ENV = COLLECTOR_PROFILES['passthrough']['env']
DISCOVERY = {'cpd_min': 2, 'cpd_max': 6, 'reverse_policy': 'inverse_depth'}
# Tomislav-RetCtx: trace-store tuning (2026-09-16). The n=3 run showed the ingest
# ceiling (~30k spans/s) was Elasticsearch at 7.5 of 8 cores while Jaeger used 2.5
# of 24: Jaeger's default nested tag mapping made ~12 Lucene docs per span. Store
# tags as object fields, move the CPU to Elasticsearch, drop the unplaceable
# replica, and relax refresh/translog for an ingest-only store. Response-time
# measurements are unaffected (node-9 only); trace completeness is the goal.
# 2026-09-16 round 2: round 1 (Jaeger 6 / ES 26) lifted the ceiling from 30k to 88k spans/s
# but pinned Jaeger at 4.6-5.8 of its 6 cores while Elasticsearch idled at 9-13 of 26. Jaeger
# costs 54-78 us/span and Elasticsearch ~119 us/span, so 12 + 26 = 38 of node-9's 40 allocatable
# cores should clear the ~180k spans/s that full sampling produces near the bridge knee.
BACKEND_TUNING = {
    'jaeger_cpus': 12, 'elasticsearch_cpus': 26, 'elasticsearch_heap': '16g',
    # ES_BULK_WORKERS: round 2 ran 8-9 of the default 10 bulk requests in flight while
    # Elasticsearch used 15 of 26 cores, so the writer's concurrency was the last store limit.
    'jaeger_env': {'ES_TAGS_AS_FIELDS_ALL': 'true', 'ES_NUM_SHARDS': 12, 'ES_NUM_REPLICAS': 0,
                   'ES_BULK_WORKERS': 24},
    # Legacy (order-merged) template so Jaeger's own order-0 mapping template still applies.
    # merge.scheduler.max_thread_count: round 1 throttled 2326 s of 6940 s of merge time at the
    # ES 7 default of 4 threads; node-9's data disk is SSD (rotational=0), so raise it.
    'index_template': {'order': 10, 'index_patterns': ['*jaeger-span-*', '*jaeger-service-*'],
                       'settings': {'index': {'refresh_interval': '10s',
                                              'merge': {'scheduler': {'max_thread_count': 8}},
                                              'translog': {'durability': 'async', 'sync_interval': '30s'}}}},
}


def case_name(kind, ratio):
    return kind if ratio == 1 else f'{kind}-s{int(round(ratio * 100))}'


def collector_config(kind, variant, profile='passthrough'):
    """Collector pipeline for a no-work case. See COLLECTOR_PROFILES for the two regimes."""
    assert profile in COLLECTOR_PROFILES, profile
    config = {
        'receivers': {'otlp': {'protocols': {'grpc': {'endpoint': '0.0.0.0:4317', 'include_metadata': True}}}},
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
            'pipelines': {'traces': {'receivers': ['otlp'], 'processors': ['batch'], 'exporters': ['otlp']}},
        },
    }
    if profile == 'sink':
        config['exporters'] = {'nop': {}}
        config['service']['pipelines']['traces']['exporters'] = ['nop']
    if profile.startswith('admission'):
        # Thresholds copied from prepare_dsb_sn_e2e.collector_config.
        if kind in BRIDGES:
            config['processors']['priority'] = {
                'check_interval': '100ms', 'soft_percentage': 50, 'hard_percentage': 70,
                'cp_safety_factor': 1, 'force_gc': True,
                'gc_soft_interval': '1s', 'gc_ultrasoft_interval': '0s',
            }
            config['service']['pipelines']['traces']['processors'] = ['priority', 'batch']
        else:  # vanilla and no-tracing have no bridges-priority metadata to act on
            config['processors']['memory_limiter'] = {
                'check_interval': '100ms', 'limit_percentage': 70, 'spike_limit_percentage': 20}
            config['service']['pipelines']['traces']['processors'] = ['memory_limiter', 'batch']
    if kind in BRIDGES:
        config['receivers']['configdiscovery'] = {'endpoint': ':8080', 'config_map': dict(DISCOVERY)}
        config['exporters']['debug/config'] = {'verbosity': 'basic'}
        config['service']['pipelines']['logs/configdiscovery'] = {
            'receivers': ['configdiscovery'], 'processors': [], 'exporters': ['debug/config']}
    return config


def service_environment(kind, ratio):
    env = {'GOMAXPROCS': 8, 'GOGC': 100, 'GC_INTERVAL_SEC': 0}
    if kind == 'nt':
        return env  # no app-side SDK at all; tracing variables would be inert
    env.update({
        'BRIDGE_KIND': kind, 'OTLP_RETRY': 'off', 'OTEL_SAMPLE_RATIO': ratio,
        'REVERSE_TRUSS': 'on' if kind in BRIDGES else 'off',
        'RT_LEAF_REJECT': 1 if kind in BRIDGES else 0,
        'RT_ROOT': 'off', 'RT_SAMPLE': 10000,
    })
    return env


def tune_backend(doc, tuning=BACKEND_TUNING):
    """Apply BACKEND_TUNING to a Jaeger or Elasticsearch Deployment document in place."""
    name = doc['metadata']['name']
    if doc.get('kind') != 'Deployment' or not name.startswith(('jaeger-', 'elasticsearch-')):
        return False
    for container in doc['spec']['template']['spec']['containers']:
        if name.startswith('jaeger-'):
            cpus = tuning['jaeger_cpus']
            environment(container, dict(tuning['jaeger_env'], GOMAXPROCS=cpus))
        else:
            cpus = tuning['elasticsearch_cpus']
            heap = tuning['elasticsearch_heap']
            environment(container, {'ES_JAVA_OPTS': f'-Xms{heap} -Xmx{heap}'})
        container['resources'] = {key: {'cpu': f'{cpus}000m'} for key in ('requests', 'limits')}
    return True


def set_reverse(doc, reverse):
    """Turn the response path (reverse truss) on or off for every application service.

    REVERSE_TRUSS=off leaves the reverse wrapper uninstalled, so there is no response-path
    propagation and no leaf rejection. Checkpoint selection then comes from the CPD schedule
    plus isPathCheckpoint's rule that a childless server span is always a checkpoint --
    i.e. "leaves are always checkpointed". CPD itself is unchanged (collector discovery).
    """
    name = doc['metadata']['name']
    if doc.get('kind') != 'Deployment' or '-service-' not in name:
        return False
    touched = False
    for container in doc['spec']['template']['spec']['containers']:
        env = {e['name']: e.get('value') for e in container.get('env', [])}
        if 'REVERSE_TRUSS' not in env:
            continue  # no-tracing services carry no SDK
        environment(container, {'REVERSE_TRUSS': reverse,
                                'RT_LEAF_REJECT': 1 if reverse == 'on' else 0})
        touched = True
    return touched


def configure_manifests(documents, kind, ratio, variant, collector_image, namespace):
    out = []
    config_name = f'otelcol-{variant}-config'
    config = collector_config(kind, variant)
    for original in documents:
        if not isinstance(original, dict):
            continue
        doc = copy.deepcopy(original)
        name = doc['metadata']['name']
        if name.startswith('tracepressure-'):  # auxiliary pump; not part of the workload
            continue
        doc['metadata']['namespace'] = namespace
        # Same ownership label key as the real-work run so the shared teardown /
        # snapshot helpers recognise these resources.
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
                    container['args'] = ['--config=/etc/otelcol/nw/config.yaml']
                    environment(container, COLLECTOR_ENV)
                    container['resources'] = {key: dict(COLLECTOR_RESOURCES) for key in ('requests', 'limits')}
                    container.setdefault('volumeMounts', []).append({
                        'name': 'nw-config', 'mountPath': '/etc/otelcol/nw', 'readOnly': True})
                    pod.setdefault('volumes', []).append({
                        'name': 'nw-config', 'configMap': {'name': config_name}})
                    pod.setdefault('affinity', {})['nodeAffinity'] = {
                        'requiredDuringSchedulingIgnoredDuringExecution': {'nodeSelectorTerms': [{
                            'matchExpressions': [{'key': 'kubernetes.io/hostname', 'operator': 'In',
                                                  'values': [f'node-{i}' for i in range(1, 9)]}]}]}}
                elif '-service-' in name:
                    environment(container, service_environment(kind, ratio))
                    assert container['resources'] == {
                        key: {'cpu': '8000m'} for key in ('requests', 'limits')}, (name, container['resources'])
                ports = container.get('ports', [])
                if ports:
                    probe = {'tcpSocket': {'port': ports[0]['containerPort']}, 'periodSeconds': 5}
                    container.setdefault('readinessProbe', dict(probe, failureThreshold=3))
                    container.setdefault('startupProbe', dict(probe, failureThreshold=60))
            if not name.startswith('otelcol-'):
                assert pod.get('nodeSelector', {}).get('kubernetes.io/hostname'), name
            tune_backend(doc)
        out.append(doc)
    out.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {
        'name': config_name, 'namespace': namespace, 'labels': {'retctx-e2e': variant}},
        'data': {'config.yaml': yaml.safe_dump(config, sort_keys=False)}})
    assert sum(d['kind'] == 'Deployment' for d in out) == 25, sum(d['kind'] == 'Deployment' for d in out)
    assert sum(d['kind'] == 'DaemonSet' for d in out) == 1
    return out


def prepare(root, namespace, collector_image, resume=False, kinds=None):
    stamp = root.name.rsplit('-', 1)[1].lower()
    extra = 'x' + stamp
    cases = []
    generated = {}
    selected = [(kind, ratio) for kind, ratio in CASES if not kinds or kind in kinds]
    for kind, ratio in selected:
        name = case_name(kind, ratio)
        build_name = f'{kind}_nw_{stamp}'
        build = DSB / ('build_' + build_name)
        case = root / 'builds' / name
        write_json(root / 'prepare-status.json', {'state': 'running', 'case': name})
        if kind not in generated:
            argv = [REPO / 'utils/build_deploy_dsb.sh', '-s', f'docker_{kind}_es_nw', '-n', build_name,
                    '--extra', extra, '--gc', 'natural', '--collector', 'passthrough',
                    '--anti-affinity', '--wrk2api-deploy', '--skip-build']
            if kind in BRIDGES:
                argv += ['--cpd-min', '2', '--cpd-max', '6', '--reverse-policy', 'inverse_depth']
            if build.exists():
                assert resume and (root / 'builds' / kind / 'manifest.yaml').exists(), f'refusing to overwrite {build}'
            else:
                with (root / 'logs' / f'prepare-{kind}.log').open('w') as log:
                    command(argv, cwd=REPO, env=dict(os.environ, OTEL_SAMPLE_RATIO='1'),
                            stdout=log, stderr=subprocess.STDOUT)
            compose = yaml.safe_load((build / 'docker/docker-compose.yml').read_text())
            service = next(s for s in compose['services'] if s.startswith('otelcol_'))
            variant = service[len('otelcol_'):-len('_ctr')].replace('_', '-')
            documents = [d for p in sorted((build / 'k8s').glob('*.yaml'))
                         for d in yaml.safe_load_all(p.read_text())]
            generated[kind] = (build, variant, documents)
        build, variant, documents = generated[kind]
        configured = configure_manifests(documents, kind, ratio, variant, collector_image, namespace)
        case.mkdir(parents=True, exist_ok=resume)
        (case / 'manifest.yaml').write_text(yaml.safe_dump_all(configured, sort_keys=False))
        (case / 'collector.yaml').write_text(yaml.safe_dump(collector_config(kind, variant), sort_keys=False))
        command(['docker', 'run', '--rm', '-v', f'{case}/collector.yaml:/config.yaml:ro',
                 collector_image, 'validate', '--config=/config.yaml'])
        entry = {'name': name, 'kind': kind, 'sample_ratio': ratio, 'variant': variant,
                 'spec': f'docker_{kind}_es_nw', 'build': str(build), 'case': str(case),
                 'image_case': str(root / 'builds' / kind),
                 'collector_image': collector_image, 'namespace': namespace,
                 'backend_tuning': copy.deepcopy(BACKEND_TUNING)}
        write_json(case / 'case.json', entry)
        cases.append(entry)
        write_json(root / 'cases.json', cases)
    write_json(root / 'prepare-status.json', {'state': 'complete', 'cases': len(cases)})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--namespace', default='dsb-sn')
    parser.add_argument('--collector-image', required=True,
                        help='pinned otelcontribcol digest reference (reuse the validated e2e collector)')
    parser.add_argument('--resume', action='store_true')
    parser.add_argument('--kinds', help='comma-separated subset of nt,v,pb,cgpb,sb (default: all)')
    args = parser.parse_args()
    assert '@sha256:' in args.collector_image
    kinds = [k for k in (args.kinds or '').split(',') if k] or None
    try:
        prepare(args.out.resolve(), args.namespace, args.collector_image, args.resume, kinds)
    except Exception as error:
        write_json(args.out / 'prepare-status.json', {'state': 'failed', 'error': str(error)})
        raise
