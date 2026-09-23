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
from dsb_apps import APPS  # Tomislav-RetCtx: per-application constants (default app 'sn')

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
    # admission500m: identical processors, thresholds and memory budget to
    # 'admission'; only the CPU limit changes (1 -> 500m).
    'admission500m': {'resources': {'cpu': '500m', 'memory': '256Mi'},
                      'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # admission6040: same 1 CPU / 256 Mi budget as 'admission'; only the shedding
    # thresholds move. Every configuration idles at 70-77 MiB working set, and at
    # soft 50 / hard 70 (115 / 161 MiB) only transient peaks cross, so vanilla and
    # the response-path-off bridges do not start shedding until the application has
    # already plateaued. soft 40 / hard 60 (92 / 138 MiB) sits above the idle floor
    # but well below those peaks, so every configuration sheds before its knee.
    'admission6040': {'resources': {'cpu': '1', 'memory': '256Mi'},
                      'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # Tomislav-RetCtx: admission6040fr = admission6040 with a fast exporter retry. Jaeger's
    # 10,000-span collector queue overflows on any Elasticsearch hiccup and answers
    # Unavailable; with the default 5 s initial / 30 s max backoff each rejection parks one of
    # the ten sending-queue consumers for 5-28 s, the exporter drain falls (79.8k spans/s clean
    # at 3,500 req/s, 71.5k under pushback at 4,000) and the admitted spans pile into heap until
    # the soft watermark refuses everything, checkpoints included. 200 ms / 1 s keeps the
    # drain at the store's real rate so heap grows only when arrivals exceed it. Jaeger's queue
    # is left small on purpose: the collector's 256 Mi stays the only buffer in the path.
    'admission6040fr': {'resources': {'cpu': '1', 'memory': '256Mi'},
                        'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # Tomislav-RetCtx: admissionotel = admission6040 with the OpenTelemetry Collector Helm chart's
    # default memory_limiter percentages (limit 80, spike 25 -> refuse-all at 55 percent, GC at 80;
    # 141 / 205 MiB of the 256 Mi cgroup) instead of 40/60. Same CPU, memory, GOMEMLIMIT, retry,
    # batch, pipeline. Motivation (2026-09-22): at 40/60 the priority processor's derived LP-shed
    # level on the composepost agent (soft minus an HP-rate margin of ~56 MiB) sits BELOW that
    # agent's steady heap, so it sheds two thirds of its LP with an idle pipeline behind it.
    'admissionotel': {'resources': {'cpu': '1', 'memory': '256Mi'},
                      'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    # Tomislav-RetCtx (user 2026-09-23, hotel real-work): the SN real-work agent budget
    # (500m / 256 Mi, as admission500m) with the OpenTelemetry Collector Helm defaults of
    # admissionotel (priority soft 55 / hard 80, memory_limiter 80 / 25 -> 141 / 205 MiB).
    'admissionotel500m': {'resources': {'cpu': '500m', 'memory': '256Mi'},
                          'env': {'GOMEMLIMIT': '230MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
    'sink': {'resources': {'cpu': '1', 'memory': '4Gi'},
             'env': {'GOMEMLIMIT': '3276MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}},
}
# profile -> (priority soft%, priority hard%, memory_limiter limit%, spike%)
ADMISSION_THRESHOLDS = {'admission6040': (40, 60, 60, 20), 'admission6040fr': (40, 60, 60, 20),
                        'admissionotel': (55, 80, 80, 25), 'admissionotel500m': (55, 80, 80, 25)}
# Tomislav-RetCtx: profile -> (exporter retry initial_interval, max_interval); default 5s / 30s.
EXPORTER_RETRY = {'admission6040fr': ('200ms', '1s')}
# Tomislav-RetCtx: alternative trace store. 'jaeger' is the Blueprint default (Jaeger v1 +
# Elasticsearch on node-9). 'clickhouse' replaces both with (a) a GATEWAY collector on node-9
# running the same priority processor / memory_limiter as the agents plus the ClickHouse
# exporter (the paper's Step 5 collector in front of the backend), (b) a single ClickHouse
# server, and (c) a Jaeger-API shim (utils/retctx_jaeger_shim.py) that keeps the pod name
# jaeger-<variant>-ctr and serves /api/services, /api/traces and :14269/metrics from
# ClickHouse, so the runner, the trace samplers and pb_jaeger_recon are untouched. Agents
# forward the per-batch bridges-priority gRPC header to the gateway (headers_setter
# extension + batch metadata_keys) so the gateway's priority processor sees LP vs HP.
BACKENDS = ('jaeger', 'clickhouse')
CLICKHOUSE = {
    'image': '10.10.1.1:30000/clickhouse-server@sha256:ba372d984df2746f0b2e853a80c60c7846f76de344fc0b4c7e6f1489e3868101',
    'cpus': 16, 'memory': '64Gi', 'node': 'node-9',
    # The stock image confines the passwordless `default` user to localhost; the entrypoint
    # creates this user with network access instead (CLICKHOUSE_USER / CLICKHOUSE_PASSWORD).
    'user': 'otel', 'password': 'otel',
}
SHIM_IMAGE = '10.10.1.1:30000/python@sha256:9165ebb7bc67992e4676c44d79c681d27689ea923e6863e4ece8052aa1ebb061'
# Gateway collector budget. Its watermarks are the same percentages as the agents' profile,
# applied to this cgroup: at 2 GiB and 40/60 the LP-shed ceiling is 819 MiB, refuse-all 1229 MiB.
GATEWAY = {'resources': {'cpu': '4', 'memory': '2Gi'},
           'env': {'GOMEMLIMIT': '1843MiB', 'GOGC': 100, 'GC_INTERVAL_SEC': 0}, 'node': 'node-9'}
COLLECTOR_RESOURCES = COLLECTOR_PROFILES['passthrough']['resources']
COLLECTOR_ENV = COLLECTOR_PROFILES['passthrough']['env']
DISCOVERY = dict(APPS['sn']['discovery'])  # SN; other apps: APPS[app]['discovery']
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


def collector_config(kind, variant, profile='passthrough', backend='jaeger', app='sn', priority_extra=None,
                     priority_receiver=False, priority_queue=False, pprof=False, compression=None):
    """Collector pipeline for a no-work case. See COLLECTOR_PROFILES for the two regimes.
    Tomislav-RetCtx: priority_extra (e.g. {'us_margin_mode': 'backlog'}) is merged into the
    bridges' priority processor config; None keeps the pre-existing config byte for byte."""
    assert profile in COLLECTOR_PROFILES, profile
    assert backend in BACKENDS, backend
    config = {
        'receivers': {'otlp': {'protocols': {'grpc': {'endpoint': '0.0.0.0:4317', 'include_metadata': True}}}},
        'processors': {'batch': {'send_batch_size': 8192, 'timeout': '200ms'}},
        'exporters': {'otlp': {
            'endpoint': f'jaeger-{variant}-ctr:4317', 'tls': {'insecure': True},
            'retry_on_failure': {'enabled': True, 'initial_interval': EXPORTER_RETRY.get(profile, ('5s', '30s'))[0],
                                 'max_interval': EXPORTER_RETRY.get(profile, ('5s', '30s'))[1], 'max_elapsed_time': '0s'},
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
        # Thresholds copied from prepare_dsb_sn_e2e.collector_config, except where a
        # profile overrides them (see ADMISSION_THRESHOLDS).
        soft, hard, limit, spike = ADMISSION_THRESHOLDS.get(profile, (50, 70, 70, 20))
        if kind in BRIDGES:
            config['processors']['priority'] = {
                'check_interval': '100ms', 'soft_percentage': soft, 'hard_percentage': hard,
                'cp_safety_factor': 1, 'force_gc': True,
                'gc_soft_interval': '1s', 'gc_ultrasoft_interval': '0s',
            }
            config['processors']['priority'].update(priority_extra or {})
            config['service']['pipelines']['traces']['processors'] = ['priority', 'batch']
        else:  # vanilla and no-tracing have no bridges-priority metadata to act on
            config['processors']['memory_limiter'] = {
                'check_interval': '100ms', 'limit_percentage': limit,
                'spike_limit_percentage': spike}
            config['service']['pipelines']['traces']['processors'] = ['memory_limiter', 'batch']
    if backend == 'clickhouse':
        # Tomislav-RetCtx: agents export to the gateway. The priority processor classifies a
        # batch by the bridges-priority gRPC header the SDK stamps on the request; the OTLP
        # exporter does not forward client metadata on its own, so the batch processor keeps
        # one batcher per header value and headers_setter re-emits it on the outgoing request.
        config['exporters']['otlp']['endpoint'] = f'otelgw-{variant}-ctr:4317'
        if kind in BRIDGES:
            config['exporters']['otlp']['auth'] = {'authenticator': 'headers_setter'}
            config['extensions'] = {'headers_setter': {'headers': [
                {'action': 'upsert', 'key': 'bridges-priority', 'from_context': 'bridges-priority'}]}}
            config['service']['extensions'] = ['headers_setter']
            config['processors']['batch']['metadata_keys'] = ['bridges-priority']
            config['processors']['batch']['metadata_cardinality_limit'] = 8
    if priority_receiver and kind in BRIDGES and 'priority' in config['processors']:
        use_priority_receiver(config)
    if priority_queue and kind in BRIDGES and 'priority' in config['processors']:
        use_priority_queue(config)
    if pprof:
        use_pprof(config)
    if compression and 'otlp' in config['exporters']:
        # Tomislav-RetCtx: OTLP exporter compression (default gzip). Profiles (2026-09-23): gzip was
        # 61 % of a node agent's CPU and gunzip 39 % of the gateway's; the hop stays in-cluster.
        config['exporters']['otlp']['compression'] = compression
    if kind in BRIDGES:
        config['receivers']['configdiscovery'] = {'endpoint': ':8080', 'config_map': dict(APPS[app]['discovery'])}
        config['exporters']['debug/config'] = {'verbosity': 'basic'}
        config['service']['pipelines']['logs/configdiscovery'] = {
            'receivers': ['configdiscovery'], 'processors': [], 'exporters': ['debug/config']}
    return config


def use_pprof(config):
    """Tomislav-RetCtx: pprof extension on :1777 (CPU profiles of the collector; measurement only)."""
    config.setdefault('extensions', {})['pprof'] = {'endpoint': '0.0.0.0:1777'}
    config['service'].setdefault('extensions', [])
    if 'pprof' not in config['service']['extensions']:
        config['service']['extensions'].append('pprof')


def use_priority_queue(config, exporter='otlp'):
    """Tomislav-RetCtx: strict-priority queue stage (contrib fork priorityprocessor mode: queue).
    Pipeline priority -> batch -> priority/queue, and the exporter's sending_queue OFF so the
    backlog builds in the queue stage's separate HP/LP FIFOs (HP always sent first, queued LP
    evicted to admit a checkpoint at soft/hard) instead of in the exporter's single FIFO. The
    queue stage's dispatch_workers takes over the exporter queue's num_consumers."""
    batch = config['processors']['batch']  # one batcher per priority, so every batch is HP or LP
    batch.setdefault('metadata_keys', ['bridges-priority'])
    batch.setdefault('metadata_cardinality_limit', 8)
    assert batch['metadata_keys'] == ['bridges-priority'], batch
    if exporter == 'otlp' and 'headers_setter' in config.get('extensions', {}):
        # the queue stage stamps bridges-hp-waiting=1 on sends while checkpoints wait behind them;
        # the next hop (gateway) refuses LP while it sees the mark
        config['extensions']['headers_setter']['headers'].append(
            {'action': 'upsert', 'key': 'bridges-hp-waiting', 'from_context': 'bridges-hp-waiting'})
    exporter = config['exporters'][exporter]
    workers = exporter['sending_queue']['num_consumers']
    exporter['sending_queue'] = {'enabled': False}
    config['processors']['priority/queue'] = {'mode': 'queue', 'dispatch_workers': workers}
    pipeline = config['service']['pipelines']['traces']
    assert pipeline['processors'] == ['priority', 'batch'], pipeline['processors']
    pipeline['processors'] = ['priority', 'batch', 'priority/queue']


def use_priority_receiver(config, max_recv_msg_size_mib=None):
    """Tomislav-RetCtx: swap the stock OTLP receiver for 'priorityotlp' (the priority processor's
    pre-decode admission receiver, contrib fork processor/priorityprocessor/priorityotlpreceiver)
    on the traces pipeline. Same endpoint, include_metadata on; only bridges carry priorities."""
    grpc = {'endpoint': '0.0.0.0:4317', 'include_metadata': True}
    if max_recv_msg_size_mib:
        grpc['max_recv_msg_size_mib'] = max_recv_msg_size_mib
    config['receivers'].pop('otlp')
    config['receivers']['priorityotlp'] = {'grpc': grpc}
    config['service']['pipelines']['traces']['receivers'] = ['priorityotlp']


def gateway_config(kind, variant, profile, priority_extra=None, priority_receiver=False, priority_queue=False,
                   pprof=False):
    """Tomislav-RetCtx: the node-9 gateway collector: OTLP in, same admission processor as the
    agents, large batches, ClickHouse out. Client metadata is kept so the priority header the
    agents forward reaches the priority processor."""
    soft, hard, limit, spike = ADMISSION_THRESHOLDS.get(profile, (50, 70, 70, 20))
    config = {
        'receivers': {'otlp': {'protocols': {'grpc': {'endpoint': '0.0.0.0:4317', 'include_metadata': True,
                                                      'max_recv_msg_size_mib': 64}}}},
        'processors': {'batch': {'send_batch_size': 20000, 'send_batch_max_size': 20000, 'timeout': '1s'}},
        'exporters': {'clickhouse': {
            'endpoint': f'tcp://clickhouse-{variant}-ctr:9000?dial_timeout=10s',
            'username': CLICKHOUSE['user'], 'password': CLICKHOUSE['password'],
            'database': 'otel', 'traces_table_name': 'otel_traces', 'create_schema': True,
            'async_insert': False, 'compress': 'lz4', 'ttl': '0s', 'timeout': '30s',
            'retry_on_failure': {'enabled': True, 'initial_interval': '1s', 'max_interval': '5s',
                                 'max_elapsed_time': '0s'},
            'sending_queue': {'enabled': True, 'num_consumers': 4, 'queue_size': 400, 'block_on_overflow': False},
        }},
        'service': {
            'telemetry': {
                'logs': {'level': 'info'},
                'metrics': {'level': 'detailed', 'readers': [{'pull': {'exporter': {
                    'prometheus': {'host': '0.0.0.0', 'port': 8888}}}}]},
            },
            'pipelines': {'traces': {'receivers': ['otlp'], 'processors': ['batch'], 'exporters': ['clickhouse']}},
        },
    }
    if profile.startswith('admission'):
        if kind in BRIDGES:
            config['processors']['priority'] = {
                'check_interval': '100ms', 'soft_percentage': soft, 'hard_percentage': hard,
                'cp_safety_factor': 1, 'force_gc': True,
                'gc_soft_interval': '1s', 'gc_ultrasoft_interval': '0s',
            }
            config['processors']['priority'].update(priority_extra or {})
            config['service']['pipelines']['traces']['processors'] = ['priority', 'batch']
            if priority_receiver:
                use_priority_receiver(config, max_recv_msg_size_mib=64)
            if priority_queue:
                use_priority_queue(config, exporter='clickhouse')
        else:
            config['processors']['memory_limiter'] = {
                'check_interval': '100ms', 'limit_percentage': limit, 'spike_limit_percentage': spike}
            config['service']['pipelines']['traces']['processors'] = ['memory_limiter', 'batch']
    if pprof:
        use_pprof(config)
    return config


def _probe(port, failure=3):
    return {'tcpSocket': {'port': port}, 'periodSeconds': 5, 'failureThreshold': failure}


def _meta(name, variant, namespace, extra=None):
    labels = {'io.kompose.service': name, 'retctx-e2e': variant}
    labels.update(extra or {})
    return {'name': name, 'namespace': namespace, 'labels': labels}


def _deployment(name, variant, namespace, container, node, volumes=None):
    return {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': _meta(name, variant, namespace),
            'spec': {'replicas': 1, 'strategy': {'type': 'Recreate'},
                     'selector': {'matchLabels': {'io.kompose.service': name}},
                     'template': {'metadata': {'labels': {'io.kompose.service': name, 'retctx-e2e': variant}},
                                  'spec': {'containers': [container], 'restartPolicy': 'Always',
                                           'nodeSelector': {'kubernetes.io/hostname': node},
                                           'volumes': volumes or []}}}}


def _service(name, variant, namespace, ports):
    return {'apiVersion': 'v1', 'kind': 'Service', 'metadata': _meta(name, variant, namespace),
            'spec': {'selector': {'io.kompose.service': name},
                     'ports': [{'name': str(p), 'port': p, 'targetPort': p} for p in ports]}}


def install_clickhouse_backend(documents, kind, variant, profile, namespace, gateway_cpu=None, priority_extra=None,
                               gomaxprocs=None, priority_receiver=False, priority_queue=False, pprof=False):
    """Tomislav-RetCtx: rewrite a Blueprint manifest in place for the 'clickhouse' backend.

    Removes Elasticsearch, turns the jaeger-<variant>-ctr Deployment into the Jaeger-API shim
    (same name and Service, so pod discovery by prefix keeps working), and adds the ClickHouse
    server plus the gateway collector with its ConfigMap. Returns the gateway config.
    gateway_cpu overrides GATEWAY['resources']['cpu']: capping the gateway's CPU makes it a
    fixed-rate sink (measured 2026-09-22: ~0.64 core + 0.015 core per 1k spans/s), so the agents'
    queues fill exactly when arrivals exceed that rate, with no store stalls in the loop."""
    gw_resources = dict(GATEWAY['resources'])
    if gateway_cpu:
        gw_resources['cpu'] = str(gateway_cpu)
    collector_image = next(c['image'] for d in documents if d and d.get('kind') == 'DaemonSet'
                           for c in d['spec']['template']['spec']['containers'])
    shim_source = (Path(__file__).resolve().parent / 'retctx_jaeger_shim.py').read_text()
    kept, jaeger_seen = [], False
    for doc in documents:
        if not doc:
            continue
        name = doc['metadata']['name']
        if name.startswith('elasticsearch-'):
            continue
        if doc.get('kind') == 'Deployment' and name.startswith('jaeger-'):
            jaeger_seen = True
            pod = doc['spec']['template']['spec']
            pod['containers'] = [{
                'name': name, 'image': SHIM_IMAGE, 'imagePullPolicy': 'IfNotPresent',
                'command': ['python3', '-u', '/shim/retctx_jaeger_shim.py'],
                'env': [{'name': 'CLICKHOUSE_URL', 'value': f'http://clickhouse-{variant}-ctr:8123'},
                        {'name': 'CLICKHOUSE_USER', 'value': CLICKHOUSE['user']},
                        {'name': 'CLICKHOUSE_PASSWORD', 'value': CLICKHOUSE['password']},
                        {'name': 'CLICKHOUSE_DB', 'value': 'otel'}, {'name': 'CLICKHOUSE_TABLE', 'value': 'otel_traces'}],
                'ports': [{'containerPort': 16686, 'protocol': 'TCP'}, {'containerPort': 14269, 'protocol': 'TCP'}],
                'resources': {key: {'cpu': '1', 'memory': '1Gi'} for key in ('requests', 'limits')},
                'readinessProbe': _probe(16686), 'startupProbe': _probe(16686, 60),
                'volumeMounts': [{'name': 'shim', 'mountPath': '/shim', 'readOnly': True}],
            }]
            pod['volumes'] = [{'name': 'shim', 'configMap': {'name': f'jaegershim-{variant}-config'}}]
            pod['nodeSelector'] = {'kubernetes.io/hostname': CLICKHOUSE['node']}
            pod.pop('affinity', None)
            doc['spec']['strategy'] = {'type': 'Recreate'}
        kept.append(doc)
    assert jaeger_seen, 'no jaeger Deployment to turn into the shim'
    documents[:] = kept
    gateway = gateway_config(kind, variant, profile, priority_extra, priority_receiver, priority_queue, pprof)
    documents.append({'apiVersion': 'v1', 'kind': 'ConfigMap',
                      'metadata': _meta(f'jaegershim-{variant}-config', variant, namespace),
                      'data': {'retctx_jaeger_shim.py': shim_source}})
    documents.append({'apiVersion': 'v1', 'kind': 'ConfigMap',
                      'metadata': _meta(f'otelgw-{variant}-config', variant, namespace),
                      'data': {'config.yaml': yaml.safe_dump(gateway, sort_keys=False)}})
    ch_name = f'clickhouse-{variant}-ctr'
    documents.append(_deployment(ch_name, variant, namespace, {
        'name': ch_name, 'image': CLICKHOUSE['image'], 'imagePullPolicy': 'IfNotPresent',
        'env': [{'name': 'CLICKHOUSE_USER', 'value': CLICKHOUSE['user']},
                {'name': 'CLICKHOUSE_PASSWORD', 'value': CLICKHOUSE['password']}],
        'ports': [{'containerPort': 9000, 'protocol': 'TCP'}, {'containerPort': 8123, 'protocol': 'TCP'}],
        'resources': {key: {'cpu': f"{CLICKHOUSE['cpus']}000m", 'memory': CLICKHOUSE['memory']}
                      for key in ('requests', 'limits')},
        'readinessProbe': _probe(8123), 'startupProbe': _probe(8123, 60),
    }, CLICKHOUSE['node']))
    documents.append(_service(ch_name, variant, namespace, [9000, 8123]))
    gw_name = f'otelgw-{variant}-ctr'
    documents.append(_deployment(gw_name, variant, namespace, {
        'name': gw_name, 'image': collector_image, 'imagePullPolicy': 'IfNotPresent',
        'args': ['--config=/etc/otelcol/gw/config.yaml'],
        # Tomislav-RetCtx: gomaxprocs='auto' pins GOMAXPROCS to the gateway's CPU limit (Go 1.24 does
        # not read the cgroup quota; unset it uses every core of the node).
        'env': [{'name': k, 'value': str(v)} for k, v in GATEWAY['env'].items()] +
               ([{'name': 'GOMAXPROCS', 'value': str(cpu_count(gw_resources['cpu']))}] if gomaxprocs == 'auto' else []),
        'ports': [{'containerPort': 4317, 'protocol': 'TCP'}, {'containerPort': 8888, 'protocol': 'TCP'}],
        'resources': {key: dict(gw_resources) for key in ('requests', 'limits')},
        'readinessProbe': _probe(4317), 'startupProbe': _probe(4317, 60),
        'volumeMounts': [{'name': 'gw-config', 'mountPath': '/etc/otelcol/gw', 'readOnly': True}],
    }, GATEWAY['node'], volumes=[{'name': 'gw-config', 'configMap': {'name': f'otelgw-{variant}-config'}}]))
    # Wait for ClickHouse before the exporter's create_schema runs, instead of crash-looping.
    documents[-1]['spec']['template']['spec']['initContainers'] = [{
        'name': 'wait-clickhouse', 'image': SHIM_IMAGE, 'imagePullPolicy': 'IfNotPresent',
        'command': ['python3', '-c',
                    'import socket,sys,time\n'
                    f'h,p="clickhouse-{variant}-ctr",9000\n'
                    'for i in range(240):\n'
                    '  try:\n    socket.create_connection((h,p),timeout=2).close(); print("clickhouse up"); sys.exit(0)\n'
                    '  except OSError: time.sleep(1)\n'
                    'sys.exit(1)'],
    }]
    documents.append(_service(gw_name, variant, namespace, [4317, 8888]))
    return gateway


def cpu_count(quantity):
    """Tomislav-RetCtx: whole CPUs for GOMAXPROCS from a k8s CPU quantity ('500m' -> 1, '2' -> 2)."""
    import math
    q = str(quantity)
    return max(1, math.ceil(float(q[:-1]) / 1000 if q.endswith('m') else float(q)))


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


# Tomislav-RetCtx (user 2026-09-23, "I want real-work"): the store of the SN REAL-WORK e2e
# campaign, copied from its pinned manifests (retctx-e2e-cpd2-6-inverse-20260915T041603Z,
# builds/*/manifest.yaml): Jaeger 24 cores with its stock bulk writer (10 workers) and nested
# tags, Elasticsearch 8 cores with a 4 GiB heap, no index-template override. BACKEND_TUNING above
# is the no-work matrix's store, sized for ~180k spans/s; this one is the paper's section 5.1 budget.
REALWORK_STORE = {
    'name': 'realwork',
    'jaeger_cpus': 24, 'elasticsearch_cpus': 8, 'elasticsearch_heap': '4g',
    'jaeger_env': {'ES_BULK_WORKERS': 10, 'ES_BULK_SIZE': 10000000, 'ES_BULK_ACTIONS': 5000,
                   'ES_BULK_FLUSH_INTERVAL': '200ms', 'COLLECTOR_QUEUE_SIZE': 10000,
                   'COLLECTOR_NUM_WORKERS': 100},
    # present in the no-work tuning, absent from the real-work manifests
    'jaeger_env_absent': ['ES_TAGS_AS_FIELDS_ALL', 'ES_NUM_SHARDS', 'ES_NUM_REPLICAS'],
    'index_template': None,
    'provenance': '/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z/builds/*/manifest.yaml',
}
STORE_TUNINGS = {'nowork': BACKEND_TUNING, 'realwork': REALWORK_STORE}


def tune_backend(doc, tuning=BACKEND_TUNING):
    """Apply BACKEND_TUNING to a Jaeger or Elasticsearch Deployment document in place."""
    name = doc['metadata']['name']
    if doc.get('kind') != 'Deployment' or not name.startswith(('jaeger-', 'elasticsearch-')):
        return False
    for container in doc['spec']['template']['spec']['containers']:
        if name.startswith('jaeger-'):
            cpus = tuning['jaeger_cpus']
            environment(container, dict(tuning['jaeger_env'], GOMAXPROCS=cpus))
            absent = set(tuning.get('jaeger_env_absent', ()))
            container['env'] = [e for e in container['env'] if e['name'] not in absent]
        else:
            cpus = tuning['elasticsearch_cpus']
            heap = tuning['elasticsearch_heap']
            environment(container, {'ES_JAVA_OPTS': f'-Xms{heap} -Xmx{heap}', 'GOMAXPROCS': cpus})
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


def configure_manifests(documents, kind, ratio, variant, collector_image, namespace, app='sn'):
    out = []
    spec = APPS[app]
    config_name = f'otelcol-{variant}-config'
    config = collector_config(kind, variant, app=app)
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
            elif name.startswith(spec['entry_prefix']):
                doc['spec']['type'] = 'NodePort'
                for port in doc['spec']['ports']:
                    if port['port'] == 2000:
                        port['nodePort'] = spec['nodeport']
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
    assert sum(d['kind'] == 'Deployment' for d in out) == spec['deployments'], sum(d['kind'] == 'Deployment' for d in out)
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
