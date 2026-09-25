#!/usr/bin/env python3
"""Tomislav-RetCtx: prepare a HotelReservation campaign SOURCE ROOT for the shared DSB tooling.

Produces the same layout prepare_dsb_sn_nw.py produces for Social Network (builds/<kind>/
manifest.yaml + collector.yaml + case.json, cases.json, prepare-status.json) plus the files a
campaign normally inherits from an earlier root (plan.json, application-source-hashes.json,
monitor_nw.py), so build_dsb_sn_nw.py, derive_dsb_sn_nw.py and run_dsb_sn_nw.py run on it
unchanged (they read the app from the `app` field; see dsb_apps.py).

Regime = paper section 5.1/5.2 and the Social Network n=5 matrix, real-work application:
  * kinds nt, v, pb, cgpb, sb at 100 % sampling; bridges with every mechanism on: CPD uniform 2..4
    (the paper's HotelReservation range), response-path truss propagation + unscheduled-leaf
    rejection, reverse routing policy from dsb_apps (depth_cubic for hotel).
  * placement: one application service per node on node-1..8, each with its own database and
    cache (so no two services one RPC hop apart share a node, and every store sits with the
    service that uses it); Jaeger + Elasticsearch alone on node-9; node-local collector DaemonSet
    with internalTrafficPolicy Local; wrk2 on node-0 through the frontend NodePort.
  * resources (req == lim): services 8 cores and GOMAXPROCS 8; databases 8 cores (none is on the
    per-request path of SearchHandler after warm-up; the paper allows up to 16); caches 4 cores;
    Jaeger 12 / Elasticsearch 26 (BACKEND_TUNING, identical to the SN campaigns).
Generation only: nothing is built, pushed or applied here.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
from datetime import datetime, timezone

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
from dsb_apps import APPS, set_cache_args
from prepare_dsb_sn_e2e import REPO, write_json, command
from prepare_dsb_sn_nw import (BACKEND_TUNING, BRIDGES, collector_config, configure_manifests)

APP = 'hotel'
KINDS = ('nt', 'v', 'pb', 'cgpb', 'sb')
# service -> (node, [stores]); store kinds by suffix: -db = mongo, -cache = memcached
PLACEMENT = [
    ('frontend', 'node-1', []),
    ('search', 'node-2', []),
    ('geo', 'node-3', ['geo-db']),
    ('rate', 'node-4', ['rate-db', 'rate-cache']),
    ('profile', 'node-5', ['profile-db', 'profile-cache']),
    ('recomd', 'node-6', ['recomd-db']),
    ('user', 'node-7', ['user-db']),
    ('reserv', 'node-8', ['reserv-db', 'reserv-cache']),
]
CPU = {'service': 8000, 'db': 8000, 'cache': 4000, 'jaeger': BACKEND_TUNING['jaeger_cpus'] * 1000,
       'elasticsearch': BACKEND_TUNING['elasticsearch_cpus'] * 1000}
# call edges of the deployed workflow (FrontEndService, SearchService); checked below
CALL_EDGES = [('frontend', 'search'), ('frontend', 'reserv'), ('frontend', 'profile'), ('frontend', 'recomd'),
              ('frontend', 'user'), ('search', 'geo'), ('search', 'rate')]
# instrumentation tracked alongside the application, as in the SN campaigns
TRACKED_DIRS = ['examples/dsb_hotel/workflow/hotelreservation', 'examples/dsb_hotel/wiring/specs',
                'runtime/plugins/otelcol', 'runtime/core/backend', 'plugins/opentelemetry']


def now():
    return datetime.now(timezone.utc).isoformat()


def pinning(variant):
    nodes = {}
    for service, node, stores in PLACEMENT:
        entries = [{f'{service}-service-{variant}-ctr': {'requests_cpu': CPU['service'], 'limits_cpu': CPU['service']}}]
        for store in stores:
            cpu = CPU['db'] if store.endswith('-db') else CPU['cache']
            entries.append({f'{store}-{variant}-ctr': {'requests_cpu': cpu, 'limits_cpu': cpu}})
        nodes[node] = entries
    nodes['node-9'] = [{f'jaeger-{variant}-ctr': {'requests_cpu': CPU['jaeger'], 'limits_cpu': CPU['jaeger']}},
                       {f'elasticsearch-{variant}-ctr': {'requests_cpu': CPU['elasticsearch'],
                                                         'limits_cpu': CPU['elasticsearch']}}]
    node_of = {service: node for service, node, _ in PLACEMENT}
    for a, b in CALL_EDGES:
        assert node_of[a] != node_of[b], (a, b)
    return nodes


NT_STRIP = ('BRIDGE_KIND', 'REVERSE_TRUSS', 'RT_LEAF_REJECT', 'RT_ROOT', 'RT_SAMPLE', 'RT_POLICY', 'RT_DEPTH')


def finish_env(documents, kind):
    """Match the SN service environment key for key: RT_POLICY / RT_DEPTH (deprecated, set by
    build_deploy_hotel.py) are not present on SN services; the no-tracing baseline carries no
    bridge variables at all (run_dsb_sn_nw.verify_deployment asserts their absence)."""
    for doc in documents:
        if doc.get('kind') != 'Deployment' or '-service-' not in doc['metadata']['name']:
            continue
        for container in doc['spec']['template']['spec']['containers']:
            strip = NT_STRIP if kind == 'nt' else ('RT_POLICY', 'RT_DEPTH')
            container['env'] = [e for e in container.get('env', []) if e['name'] not in strip]


def prepare(root, collector_image, kinds, stamp, nowork=False):
    # Tomislav-RetCtx: nowork = the zero-work variant (workflow/hotelnw, specs docker_<kind>_es_nw, app 'hotelnw':
    # same topology / backends / instrumentation, no DB/cache/compute; nothing is seeded).
    global APP, TRACKED_DIRS
    if nowork:
        APP = 'hotelnw'
        TRACKED_DIRS = [d.replace('workflow/hotelreservation', 'workflow/hotelnw') for d in TRACKED_DIRS]
    app = APPS[APP]
    root.mkdir(parents=True)
    (root / 'logs').mkdir()
    extra = 'x' + stamp
    cases = []
    for kind in kinds:
        name = kind
        write_json(root / 'prepare-status.json', {'state': 'running', 'case': name})
        spec = f'docker_{kind}_es' + ('_nw' if nowork else '')
        suffix = 'hotel_' + spec.removeprefix('docker_') + extra
        variant = suffix.replace('_', '-')
        build = app_build = REPO / 'examples/dsb_hotel' / f'build_{kind}_{APP}_{stamp}'
        case = root / 'builds' / name
        case.mkdir(parents=True)
        pin_path = case / 'node-pinning.yaml'
        pin_path.write_text(yaml.safe_dump(pinning(variant), sort_keys=False))
        argv = [sys.executable, REPO / 'utils/build_deploy_hotel.py', '-s', spec, '--extra', extra,
                '--output', build, '--skip-build', '--frontend-deploy', '--node-pinning', pin_path,
                # NodePort is set by configure_manifests (dsb_apps nodeport; the cluster's range is
                # wider than build_deploy_hotel.py's 30000..32767 check)
                '--gc', 'natural', '--collector', 'passthrough',
                '--collector-image', collector_image, '--namespace', app['namespace']]
        if kind in BRIDGES:
            d = app['discovery']
            argv += ['--cpd-min', str(d['cpd_min']), '--cpd-max', str(d['cpd_max']), '--reverse-truss',
                     '--reverse-policy', d['reverse_policy'], '--rt-leaf-reject', '1']
        with (root / 'logs' / f'prepare-{kind}.log').open('w') as log:
            command(argv, cwd=REPO, stdout=log, stderr=subprocess.STDOUT)
        documents = [d for p in sorted((build / 'k8s').glob('*.yaml')) for d in yaml.safe_load_all(p.read_text())]
        configured = configure_manifests(documents, kind, 1.0, variant, collector_image, app['namespace'], app=APP)
        finish_env(configured, kind)
        assert set_cache_args(configured, app) == 3  # Tomislav-RetCtx: memcached -c (dsb_apps cache_args)
        (case / 'manifest.yaml').write_text(yaml.safe_dump_all(configured, sort_keys=False))
        config = collector_config(kind, variant, app=APP)
        (case / 'collector.yaml').write_text(yaml.safe_dump(config, sort_keys=False))
        command(['docker', 'run', '--rm', '-v', f'{case}/collector.yaml:/config.yaml:ro',
                 collector_image, 'validate', '--config=/config.yaml'])
        entry = {'name': name, 'kind': kind, 'sample_ratio': 1.0, 'variant': variant, 'spec': spec,
                 'build': str(app_build), 'case': str(case), 'image_case': str(case),
                 'collector_image': collector_image, 'namespace': app['namespace'], 'app': APP,
                 'backend_tuning': json.loads(json.dumps(BACKEND_TUNING))}
        write_json(case / 'case.json', entry)
        cases.append(entry)
        write_json(root / 'cases.json', cases)
    hashes = {}
    for directory in TRACKED_DIRS:
        for path in sorted((REPO / directory).glob('*.go')):
            if not path.name.endswith('_test.go'):
                hashes[str(path.relative_to(REPO))] = hashlib.sha256(path.read_bytes()).hexdigest()
    write_json(root / 'application-source-hashes.json', hashes)
    plan = {
        'app': APP,
        'application': 'DSB HotelReservation, real-work Blueprint workflow (examples/dsb_hotel/workflow/'
                       'hotelreservation): MongoDB + memcached backends, data seeded by the service '
                       'constructors and checked after every deploy (dsb_apps.HOTEL_SEED)',
        'request': 'SearchHandler only (examples/dsb_hotel/scripts/search-hotels.lua): 5 RPCs, 11 spans, depth 4',
        'paper_section': '5.2 End-to-end experiment (live HotelReservation, all mechanisms on)',
        'cases': list(kinds), 'namespace': app['namespace'],
        'ramp_rates': list(range(500, 14001, 500)), 'seconds_per_rate': 30,
        'warmup_rps': 100, 'warmup_seconds': 100, 'warmup_connections': 10, 'warmup_threads': 1,
        'repetitions': 1, 'seeds': [1001, 1002, 1003, 1004, 1005],
        'fresh_deploy_per_case': True,
        'seeding': 'service constructors insert the benchmark data; run_dsb_sn_nw checks exact row counts, '
                   'zero service restarts, and a live SearchHandler probe before warm-up',
        'bridge_cpd_min': app['discovery']['cpd_min'], 'bridge_cpd_max': app['discovery']['cpd_max'],
        'reverse_policy': app['discovery']['reverse_policy'],
        'leaf_rejection': 'every unscheduled non-root server leaf',
        'sampler_ratios': [1.0], 'sdk_export_retry': False,
        'collector_image': collector_image, 'app_cpus': 8, 'app_gomaxprocs': 8,
        'db_cpus': CPU['db'] // 1000, 'cache_cpus': CPU['cache'] // 1000,
        'jaeger_cpus': BACKEND_TUNING['jaeger_cpus'], 'elasticsearch_cpus': BACKEND_TUNING['elasticsearch_cpus'],
        'placement': 'one service per node (node-1..8) with its own db/cache; Jaeger/ES node-9; '
                     + ', '.join(f'{s}={n}' for s, n, _ in PLACEMENT),
        'connection_schedule': 'connections = min(ceil(rps^2/20000), 2500); threads = ceil(connections/10) (SN rule)',
        'created': now(),
    }
    if nowork:
        plan['application'] = ('DSB HotelReservation, ZERO-WORK variant (examples/dsb_hotel/workflow/hotelnw): same '
                               'services, interfaces, call graph and deployment; no database/cache/compute (backends '
                               'wired, idle)')
        plan['seeding'] = 'none (zero-work services touch no database)'
    write_json(root / 'plan.json', plan)
    # monitor script travels with a campaign; take the SN one (app-agnostic progress file writer)
    shutil.copy2(REPO / 'utils' / 'monitor_nw.py' if (REPO / 'utils/monitor_nw.py').exists()
                 else sorted(Path('/users/tomislav/deployments/dsb-sn').glob('retctx-nwe2e-*/monitor_nw.py'))[-1],
                 root / 'monitor_nw.py')
    write_json(root / 'prepare-status.json', {'state': 'complete', 'cases': len(cases), 'app': APP})
    return cases


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--collector-image', required=True, help='pinned otelcontribcol digest reference')
    parser.add_argument('--kinds', default=','.join(KINDS))
    parser.add_argument('--nowork', action='store_true', help='zero-work variant (workflow/hotelnw)')
    args = parser.parse_args()
    assert '@sha256:' in args.collector_image
    stamp = args.out.name.rsplit('-', 1)[1].lower()
    try:
        prepare(args.out.resolve(), args.collector_image, [k for k in args.kinds.split(',') if k], stamp, nowork=args.nowork)
    except Exception as error:
        write_json(args.out / 'prepare-status.json', {'state': 'failed', 'error': str(error)})
        raise
