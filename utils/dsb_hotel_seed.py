#!/usr/bin/env python3
"""Tomislav-RetCtx (user 2026-10-29): a larger HotelReservation dataset and a mixed request workload
without changing the application.

Seeding.  The hotel services insert their stock rows at start and three of them (geo,
recommendation, user) read their collection once at construction, so extra data has to be in the
databases BEFORE the services start, and nothing may restart afterwards (a restarted service
re-inserts its stock rows; run_dsb_sn_nw.check_initialized asserts both).  `install` rewrites a
derived case's manifest for that: a ConfigMap with examples/dsb_hotel/scripts/seed_hotels.js and a
driver shell script, a Job (the deployment's own pinned mongo image) that runs the script against
each of the six databases as soon as it answers, and an init container on each of the six
database-backed services that waits for the `seed_marker` document the script leaves.  One
`kubectl apply` of the manifest still brings everything up, in the right order.

Workloads.  `workload_for` picks the wrk2 script and its environment per application:
hotel 'search' (the paper's SearchHandler-only script) or 'mixed' (DeathStarBench's
60/39/0.5/0.5 search/recommend/login/reserve mix, examples/dsb_hotel/scripts/mixed-workload.lua),
Social Network 'compose' or 'mixed' (DSB's 60/30/10 home-timeline/user-timeline/compose mix).  The
hotel mix needs the seeded ranges (users for logins, hotels for reservations), which is why the
seed spec is an input here.
"""
import copy
from pathlib import Path

from dsb_apps import APPS, DSB_HOTEL, DSB_SN, HOTEL_SEED

SEED_SCRIPT = DSB_HOTEL / 'scripts/seed_hotels.js'

# base of the database Deployment / Service name -> seed_hotels.js TARGET and the database name
TARGETS = {'geo-db': ('geo', 'geo-db'), 'rate-db': ('rate', 'rate-db'), 'profile-db': ('profile', 'profile-db'),
           'recomd-db': ('recommendation', 'recommendation-db'), 'reserv-db': ('reservation', 'reservation-db'),
           'user-db': ('user', 'user-db')}
# the service that owns each database (prepare_dsb_hotel.PLACEMENT); gets the wait-seed init container
SERVICE_OF = {'geo-db': 'geo', 'rate-db': 'rate', 'profile-db': 'profile', 'recomd-db': 'recomd',
              'reserv-db': 'reserv', 'user-db': 'user'}
STOCK = {'hotels': 80, 'users': 500}
SPEC_DEFAULTS = {'hotels': 80, 'users': 500, 'reservations': 0, 'seed': 1, 'indexes': 1, 'description_bytes': 1000}
SPEC_FLOATS = ('lat_min', 'lat_max', 'lon_min', 'lon_max')


def parse_spec(text):
    """'hotels=2000,users=5000,reservations=20[,seed=1,indexes=1,description_bytes=1000,lat_min=..]'
    -> dict.  reservations is the mean number of per-night reservation rows per hotel and night."""
    spec = dict(SPEC_DEFAULTS)
    for item in filter(None, (text or '').split(',')):
        key, _, value = item.partition('=')
        key = key.strip()
        if key in SPEC_FLOATS:
            spec[key] = float(value)
        elif key in SPEC_DEFAULTS:
            spec[key] = int(value)
        else:
            raise ValueError(f'unknown hotel seed parameter {key!r}')
    assert spec['hotels'] >= STOCK['hotels'] and spec['users'] >= STOCK['users'], spec
    assert spec['reservations'] >= 0 and spec['indexes'] in (0, 1), spec
    return spec


def spec_text(spec):
    return ','.join(f'{k}={v}' for k, v in spec.items())


def eval_params(spec, target):
    """The --eval prologue seed_hotels.js reads its parameters from."""
    names = {'hotels': 'HOTELS', 'users': 'USERS', 'reservations': 'RES_ROWS_PER_NIGHT', 'seed': 'SEED',
             'indexes': 'INDEXES', 'description_bytes': 'DESCRIPTION_BYTES'}
    parts = [f"var TARGET='{target}'"] + [f'{names[k]}={spec[k]}' for k in names] + \
            [f'{k.upper()}={spec[k]}' for k in SPEC_FLOATS if k in spec]
    return ', '.join(parts) + ';'


def driver_script(spec):
    """The Job's shell script: wait for each database, seed it.  Hosts come from SEED_HOSTS
    ('<target>=<host> ...'), so the same script runs against one local mongod in tests."""
    lines = ['#!/bin/sh', '# Tomislav-RetCtx: seed every HotelReservation database before its service starts (dsb_hotel_seed.py).',
             'set -eu', 'for pair in $SEED_HOSTS; do', '  target=${pair%%=*}; host=${pair#*=}',
             '  i=0; until mongo --host "$host" --quiet --eval \'db.runCommand({ping:1}).ok\' >/dev/null 2>&1; do',
             '    i=$((i+1)); [ $i -ge 300 ] && { echo "$host: not answering" >&2; exit 1; }; sleep 2; done',
             '  case $target in']
    for target in sorted({t for t, _ in TARGETS.values()}):
        lines.append(f'    {target}) params="{eval_params(spec, target)}" ;;')
    lines += ['    *) echo "unknown target $target" >&2; exit 1 ;;', '  esac',
              '  echo "seeding $target on $host"', '  mongo --host "$host" --quiet --eval "$params" /seed/seed_hotels.js',
              'done', 'echo "seeding complete"']
    return '\n'.join(lines) + '\n'


def wait_command(host, database):
    """The init container's command: wait until the database holds the seed marker."""
    return ['sh', '-c',
            'i=0; until [ "$(mongo --host ' + host + ' --quiet --eval \'db.getSiblingDB("' + database +
            '").seed_marker.countDocuments({_id:"seed"})\' 2>/dev/null)" = "1" ]; do '
            'i=$((i+1)); [ $i -ge 900 ] && exit 1; sleep 2; done; echo seeded']


def install(documents, variant, namespace, spec, script_text=None):
    """Add the seeding Job, its ConfigMap and the services' wait-seed init containers to a hotel
    manifest (list of documents, modified in place).  Returns what was installed."""
    names = {d['metadata']['name'] for d in documents if d}
    images = {d['metadata']['name']: d['spec']['template']['spec']['containers'][0]['image']
              for d in documents if d and d.get('kind') == 'Deployment'}
    hosts, databases = {}, {}
    for base, (target, database) in TARGETS.items():
        host = f'{base}-{variant}-ctr'
        assert host in names and host in images, f'{host}: no database Deployment/Service in the manifest'
        assert 'mongo' in images[host], (host, images[host])
        hosts[target], databases[base] = host, database
    image = images[f'geo-db-{variant}-ctr']
    config_name = f'hotel-seed-{variant}'
    script_text = script_text if script_text is not None else SEED_SCRIPT.read_text()
    documents.append({'apiVersion': 'v1', 'kind': 'ConfigMap',
                      'metadata': {'name': config_name, 'namespace': namespace, 'labels': {'retctx-e2e': variant}},
                      'data': {'seed_hotels.js': script_text, 'seed.sh': driver_script(spec)}})
    documents.append({
        'apiVersion': 'batch/v1', 'kind': 'Job',
        'metadata': {'name': config_name, 'namespace': namespace, 'labels': {'retctx-e2e': variant}},
        'spec': {'backoffLimit': 0, 'template': {
            'metadata': {'labels': {'retctx-e2e': variant, 'retctx-role': 'hotel-seed'}},
            'spec': {'restartPolicy': 'Never',
                     'containers': [{'name': 'seed', 'image': image, 'imagePullPolicy': 'IfNotPresent',
                                     'command': ['sh', '/seed/seed.sh'],
                                     'env': [{'name': 'SEED_HOSTS',
                                              'value': ' '.join(f'{t}={h}' for t, h in sorted(hosts.items()))}],
                                     'volumeMounts': [{'name': 'seed', 'mountPath': '/seed', 'readOnly': True}]}],
                     'volumes': [{'name': 'seed', 'configMap': {'name': config_name}}]}}}})
    waited = []
    for base, service in SERVICE_OF.items():
        name = f'{service}-service-{variant}-ctr'
        doc = next((d for d in documents if d and d.get('kind') == 'Deployment' and d['metadata']['name'] == name), None)
        assert doc is not None, f'{name}: no service Deployment in the manifest'
        pod = doc['spec']['template']['spec']
        pod['initContainers'] = [c for c in pod.get('initContainers', []) if c['name'] != 'wait-seed'] + [{
            'name': 'wait-seed', 'image': image, 'imagePullPolicy': 'IfNotPresent',
            'command': wait_command(f'{base}-{variant}-ctr', databases[base])}]
        waited.append(name)
    return {'job': config_name, 'configmap': config_name, 'image': image, 'hosts': hosts,
            'init_containers': waited, 'spec': dict(spec), 'spec_text': spec_text(spec),
            'marker': 'seed_marker/_id=seed in every database (inserted counts per collection)'}


def expected_counts(stock, markers):
    """Exact row counts per 'db.collection' after the services started: the constructors' stock rows
    (dsb_apps.HOTEL_SEED) plus what the seeding recorded in each database's marker document."""
    expected = {}
    for base, collections in stock.items():
        inserted = ((markers or {}).get(base) or {}).get('inserted', {})
        for database, collection, count in collections:
            expected[f'{database}.{collection}'] = count + int(inserted.get(collection, 0))
    return expected


def marker_query():
    return 'JSON.stringify(db.getSiblingDB(DB).seed_marker.findOne({_id:"seed"}))'


WORKLOADS = {
    'hotel': {
        'search': {'lua': DSB_HOTEL / 'scripts/search-hotels.lua', 'lua_env': {},
                   'request': 'SearchHandler only (examples/dsb_hotel/scripts/search-hotels.lua): 5 RPCs, 11 spans, depth 4'},
        'mixed': {'lua': DSB_HOTEL / 'scripts/mixed-workload.lua', 'lua_env': {'max_user_index': '500', 'max_hotel_id': '80'},
                  'request': "DeathStarBench's mix (examples/dsb_hotel/scripts/mixed-workload.lua): search 60% (11 spans), "
                             'recommend 39% (5 spans), login 0.5% (3 spans), reserve 0.5% (5 spans)'},
    },
    'sn': {
        'compose': {'lua': DSB_SN / 'scripts/compose-post.lua', 'lua_env': {'max_user_index': '962'},
                    'request': 'ComposePost only (examples/dsb_sn/scripts/compose-post.lua): 23 spans'},
        'mixed': {'lua': DSB_SN / 'scripts/mixed-workload.lua', 'lua_env': {'max_user_index': '962'},
                  'request': "DeathStarBench's mix (examples/dsb_sn/scripts/mixed-workload.lua): read home timeline 60%, "
                             'read user timeline 30%, compose post 10%'},
    },
}
WORKLOADS['hotelnw'] = WORKLOADS['hotel']
WORKLOADS['snrw'] = WORKLOADS['sn']


def workload_for(app, name, spec=None):
    """The plan's workload block run_wrk reads: script path, environment, description."""
    table = WORKLOADS[app]
    assert name in table, (app, name, sorted(table))
    chosen = copy.deepcopy(table[name])
    if app in ('hotel', 'hotelnw') and name == 'mixed' and spec:
        chosen['lua_env'] = {'max_user_index': str(spec['users']), 'max_hotel_id': str(spec['hotels'])}
    assert Path(chosen['lua']).exists(), chosen['lua']
    return {'name': name, 'lua': str(chosen['lua']), 'lua_env': chosen['lua_env'], 'request': chosen['request']}
