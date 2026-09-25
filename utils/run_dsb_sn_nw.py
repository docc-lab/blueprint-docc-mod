#!/usr/bin/env python3
"""Tomislav-RetCtx: zero-work Social Network ramp runner (paper section 5.4 regime).

Reuses the real-work e2e helpers for wrk, telemetry snapshots, teardown, readiness
and trace capture. Differences: no seeding (services persist nothing), a single
pass over nine cases (five kinds at 100 % plus four traced kinds at 10 % sampling),
a 500..14000 step-500 ramp with a connection cap, passthrough collectors at
1 CPU / 4 GiB, and no trace capture for the no-tracing baseline.
"""
import argparse
import hashlib
import json
import retctx_wire
import math
from pathlib import Path
import time
import urllib.request

from prepare_dsb_sn_e2e import REPO, write_json
from prepare_dsb_sn_nw import BRIDGES, COLLECTOR_PROFILES, COLLECTOR_RESOURCES, DISCOVERY, GATEWAY, cpu_count
from dsb_apps import APPS, app_name, app_of, entry_url  # Tomislav-RetCtx: per-application constants
from run_dsb_sn_e2e import (now, kube, get_http, snapshot, counter_deltas, run_wrk, teardown, ready,
                            sample_traces, capture_traces, settle_trace_samples)

CONNECTION_CAP = 2500


def connections_for(rate, cap=CONNECTION_CAP):
    return min(max(1, math.ceil(rate * rate / 20000)), cap)


def check_sources(root):
    for name, digest in json.loads((root / 'application-source-hashes.json').read_text()).items():
        assert hashlib.sha256((REPO / name).read_bytes()).hexdigest() == digest, name


def millicores(quantity):
    return int(round(float(quantity[:-1]) if quantity.endswith('m') else float(quantity) * 1000))


def http_json(url, method='GET', body=None, timeout=30):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method,
                                     headers={'Content-Type': 'application/json'} if data else {})
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=timeout) as response:
        return json.loads(response.read().decode() or 'null')


def backend_pods(case):
    pods = json.loads(kube(case['namespace'], 'get', 'pods', '-l', f'retctx-e2e={case["variant"]}', '-o', 'json'))['items']
    jaeger = next((p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('jaeger-')), None)
    elastic = next((p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('elasticsearch-')), None)
    # Tomislav-RetCtx: the clickhouse backend has no Elasticsearch pod; callers check for None.
    return (f'http://{jaeger}' if jaeger else None), (f'http://{elastic}:9200' if elastic else None)


def tune_elasticsearch(case, directory):
    """Tomislav-RetCtx: install the order-10 settings template next to Jaeger's order-0
    mapping templates (legacy templates merge by order; a composable template would
    hide Jaeger's mapping entirely). Runs before any span is stored."""
    tuning = case['backend_tuning']
    _, elastic = backend_pods(case)
    deadline = time.monotonic() + 180
    while True:
        try:
            health = http_json(f'{elastic}/_cluster/health')
            templates = http_json(f'{elastic}/_template/jaeger-span,jaeger-service')
            if health['status'] in ('green', 'yellow') and {'jaeger-span', 'jaeger-service'} <= set(templates):
                break
        except Exception:
            if time.monotonic() > deadline:
                raise
        assert time.monotonic() < deadline, 'Elasticsearch or Jaeger templates not ready'
        time.sleep(3)
    assert templates['jaeger-span']['order'] == 0, templates['jaeger-span']['order']
    if not tuning.get('index_template'):
        # Tomislav-RetCtx: real-work store (prepare_dsb_sn_nw.REALWORK_STORE) = Jaeger's own
        # templates only, as in the SN real-work e2e campaign; record that nothing was overridden.
        write_json(directory / 'es-template.json', {'installed': now(), 'health': health,
                                                    'jaeger_templates': templates, 'override': None,
                                                    'store': tuning.get('name')})
        return
    composable = http_json(f'{elastic}/_index_template').get('index_templates', [])
    assert not any(pattern.endswith('jaeger-span-*') for t in composable
                   for pattern in t['index_template']['index_patterns']), 'composable jaeger template present'
    http_json(f'{elastic}/_template/retctx-jaeger', 'PUT', tuning['index_template'])
    installed = http_json(f'{elastic}/_template/retctx-jaeger')['retctx-jaeger']
    wanted = tuning['index_template']['settings']['index']
    assert installed['order'] == tuning['index_template']['order'], installed
    assert installed['settings']['index']['refresh_interval'] == wanted['refresh_interval'], installed
    assert installed['settings']['index']['translog']['durability'] == wanted['translog']['durability'], installed
    write_json(directory / 'es-template.json', {'installed': now(), 'health': health, 'jaeger_templates': templates,
                                                'override': installed})


def verify_index(case, directory):
    """After the first spans are stored: the live jaeger-span index must carry the tuned
    settings and object-field tags (tags-as-fields), else stop before measuring."""
    tuning = case['backend_tuning']
    _, elastic = backend_pods(case)
    settings = http_json(f'{elastic}/jaeger-span-*/_settings')
    mappings = http_json(f'{elastic}/jaeger-span-*/_mapping')
    assert settings, 'no jaeger-span index yet'
    if not tuning.get('index_template'):
        # Tomislav-RetCtx: real-work store: Jaeger defaults must be in effect (no tuned leftovers)
        for name, item in settings.items():
            index = item['settings']['index']
            assert index.get('refresh_interval') in (None, '1s'), (name, index)
            assert 'translog' not in index or index['translog'].get('durability') != 'async', (name, index)
        state = {'checked': now(), 'settings': settings, 'store': tuning.get('name'),
                 'health': http_json(f'{elastic}/_cluster/health')}
        write_json(directory / 'es-index.json', state)
        return state
    for name, item in settings.items():
        index = item['settings']['index']
        assert index['number_of_shards'] == str(tuning['jaeger_env']['ES_NUM_SHARDS']), (name, index)
        assert index['number_of_replicas'] == str(tuning['jaeger_env']['ES_NUM_REPLICAS']), (name, index)
        assert index['refresh_interval'] == tuning['index_template']['settings']['index']['refresh_interval'], (name, index)
        assert index['translog']['durability'] == 'async', (name, index)
        assert index['merge']['scheduler']['max_thread_count'] == \
            str(tuning['index_template']['settings']['index']['merge']['scheduler']['max_thread_count']), (name, index)
        tag_fields = mappings[name]['mappings']['properties'].get('tag', {}).get('properties', {})
        assert tag_fields, f'{name}: no object tag fields; tags-as-fields not in effect'
    state = {'checked': now(), 'settings': settings,
             'tag_fields': {name: sorted(m['mappings']['properties']['tag']['properties']) for name, m in mappings.items()},
             'health': http_json(f'{elastic}/_cluster/health')}
    write_json(directory / 'es-index.json', state)
    return state


def knee_windows(case, directory, plan, measured, seed, state, status_path):
    """Tomislav-RetCtx (user 2026-09-24): adaptive repeat windows in THIS kind's knee region, after its climb.
    Region: the measured rates from from_fraction x (its best delivered rate) up to and including the first rate that
    delivered < 97 pct of offered. Per rate, 30 s windows (same deployment, new Lua seed per window, knee_gap_seconds
    apart) are added until the bootstrap 5-95 pct band of the POOLED p99 (wrk2 HdrHistogram spectra merged, see
    pool_latency.py) is within +-ci of the pooled value, with at least min_windows and at most max_windows (the
    climb's window counts). Extra windows are wrk only (rate-XXXXX/window-N/); loss comes from the climb's window."""
    import random
    from pool_latency import spectrum, cdf
    cfg = plan['knee_windows']
    best = max(r['completed_rps'] for r in measured)
    region = []
    for r in measured:
        if r['offered_rps'] >= cfg['from_fraction'] * best:
            region.append(r['offered_rps'])
            if r['completed_rps'] < 0.97 * r['offered_rps']:
                break
    rng = random.Random(seed)
    def pooled_q(wins, q=0.99):
        total = sum(n for _, n in wins)
        grid = sorted({v for w, _ in wins for v, _ in w})
        return next((x for x in grid if sum(n * cdf(w, x) for w, n in wins) / total >= q), grid[-1])
    state.update(stage='knee-settle', updated=now()); write_json(status_path, state)
    time.sleep(cfg.get('settle_seconds', 30))  # the climb ended past the plateau: let the backlog drain
    summary = {}
    for rate in region:
        point = directory / f'rate-{rate:05d}'
        wins, n, band = [spectrum(point / 'wrk.stdout')], 1, None
        while True:
            if n >= cfg['min_windows']:
                pooled = pooled_q(wins)
                boots = sorted(pooled_q([rng.choice(wins) for _ in wins]) for _ in range(cfg.get('bootstrap', 200)))
                band = (boots[int(0.05 * len(boots))], boots[int(0.95 * len(boots)) - 1])
                if (band[1] - band[0]) / 2 <= cfg['ci'] * pooled:
                    break
            if n >= cfg['max_windows']:
                break
            n += 1
            state.update(stage='knee-windows', offered_rps=rate, window=n, updated=now()); write_json(status_path, state)
            window = point / f'window-{n}'
            result = run_wrk(window, rate, plan['seconds_per_rate'], seed + 100 * n, connections=connections_for(rate),
                             app=app_name(case))
            write_json(window / 'result.json', result)
            wins.append(spectrum(window / 'wrk.stdout'))
            time.sleep(cfg.get('gap_seconds', 8))
        summary[str(rate)] = {'windows': n, 'pooled_p99_ms': pooled_q(wins), 'band_5_95_ms': band}
        write_json(directory / 'knee-windows.json', {'region': region, 'best_delivered': best, 'config': cfg,
                                                     'summary': summary, 'updated': now()})


def backend_state(case, directory):
    jaeger, elastic = backend_pods(case)
    state = {'captured': now(), 'backend': case.get('backend', 'jaeger')}
    if elastic is None:
        # Tomislav-RetCtx: clickhouse backend; the shim's :14269/metrics carries the saved-span count.
        try:
            text = get_http(f'{jaeger}:14269/metrics')
            state['jaeger'] = {line.split(' ')[0]: float(line.rsplit(' ', 1)[1])
                               for line in text.splitlines() if line and not line.startswith('#')}
        except Exception as error:
            state['error'] = repr(error)
        write_json(directory / 'backend-final.json', state)
        return state
    try:
        state['indices'] = http_json(f'{elastic}/_cat/indices/jaeger-*?format=json&h=index,docs.count,store.size,pri,rep')
        state['stats'] = http_json(f'{elastic}/_stats/indexing,merge,refresh,flush?level=cluster')['_all']['primaries']
        state['write_pool'] = http_json(f'{elastic}/_cat/thread_pool/write?format=json&h=active,queue,rejected,completed')
        state['health'] = http_json(f'{elastic}/_cluster/health')
        text = get_http(f'{jaeger}:14269/metrics')
        totals = {}
        for line in text.splitlines():
            if line.startswith('jaeger_collector_spans_') or line.startswith('jaeger_bulk_index_'):
                name, value = line.split(' ')[0].split('{')[0], float(line.rsplit(' ', 1)[1])
                totals[name] = totals.get(name, 0.) + value
        state['jaeger'] = totals
    except Exception as error:
        state['error'] = repr(error)
    write_json(directory / 'backend-final.json', state)
    return state


def verify_deployment(case, directory):
    namespace, variant, kind = case['namespace'], case['variant'], case['kind']
    pods = ready(namespace, variant, case.get('expected_pods', 33))  # 25 deployments + 8 collectors (34 with clickhouse)
    write_json(directory / 'ready-pods.json', {'items': pods})
    tuning = case.get('backend_tuning')
    collectors = [p for p in pods if p['metadata']['name'].startswith('otelcol-')]
    assert {p['spec']['nodeName'] for p in collectors} == {f'node-{i}' for i in range(1, 9)}
    for pod in pods:
        name = pod['metadata']['name']
        for container in pod['spec']['containers']:
            assert '@sha256:' in container['image'], name
            env = {e['name']: e.get('value') for e in container.get('env', [])}
            if '-service-' in name:
                assert env['GOMAXPROCS'] == '8', name
                # Tomislav-RetCtx: SDK retry policy exactly as the case records it (absent = off).
                assert env.get('BRIDGES_RETRY') == case.get('sdk_retry_mode'), (name, env.get('BRIDGES_RETRY'))
                if kind == 'nt':
                    # No app-side SDK: bridge variables must be absent. The build script
                    # bakes OTLP_RETRY/OTEL_SAMPLE_RATIO into every service; inert here.
                    assert not {'BRIDGE_KIND', 'REVERSE_TRUSS', 'RT_LEAF_REJECT'} & set(env), name
                else:
                    assert env['BRIDGE_KIND'] == kind and env['OTLP_RETRY'] == 'off', name
                    assert float(env['OTEL_SAMPLE_RATIO']) == case['sample_ratio'], (name, env['OTEL_SAMPLE_RATIO'])
                    reverse = case.get('reverse_truss', 'on') if kind in BRIDGES else 'off'
                    assert env['REVERSE_TRUSS'] == reverse, (name, env['REVERSE_TRUSS'], reverse)
                    assert env['RT_LEAF_REJECT'] == ('1' if reverse == 'on' else '0'), (name, env)
            if tuning and name.startswith(('jaeger-', 'elasticsearch-')):
                cpus = tuning['jaeger_cpus' if name.startswith('jaeger-') else 'elasticsearch_cpus']
                # The API server normalises quantities (26000m -> 26); compare in millicores.
                assert {key: millicores(container['resources'][key]['cpu']) for key in ('requests', 'limits')} == \
                    {'requests': cpus * 1000, 'limits': cpus * 1000}, (name, container['resources'])
                if name.startswith('jaeger-'):
                    assert all(env[k] == str(v) for k, v in tuning['jaeger_env'].items()), (name, env)
                    assert not set(tuning.get('jaeger_env_absent', ())) & set(env), (name, env)
                    assert env['GOMAXPROCS'] == str(cpus), env
                else:
                    heap = tuning['elasticsearch_heap']
                    assert env['ES_JAVA_OPTS'] == f'-Xms{heap} -Xmx{heap}', env
            # Tomislav-RetCtx: memcached must run with the app's server arguments (dsb_apps
            # cache_args: hotel -c 65536); a stock 1024-connection limit fails requests under load.
            if '-cache-' in name and app_of(case).get('cache_args'):
                assert container.get('args') == app_of(case)['cache_args'], (name, container.get('args'))
            # Tomislav-RetCtx: the gateway's GOMAXPROCS and image follow the same switches as the agents.
            if name.startswith('otelgw-'):
                gw_cpu = str(case.get('gateway_cpu') or GATEWAY['resources']['cpu'])
                if case.get('collector_gomaxprocs') == 'auto':
                    assert env.get('GOMAXPROCS') == str(cpu_count(gw_cpu)), (name, env.get('GOMAXPROCS'))
                if case.get('collector_image_override'):
                    assert container['image'] == case['collector_image_override'], (name, container['image'])
            if name.startswith('otelcol-'):
                wanted = COLLECTOR_PROFILES[case.get('collector_profile', 'passthrough')]['resources']
                assert container['resources'] == {key: dict(wanted) for key in ('requests', 'limits')}, \
                    (name, container['resources'])
                assert env['GOMEMLIMIT'] == COLLECTOR_PROFILES[
                    case.get('collector_profile', 'passthrough')]['env']['GOMEMLIMIT'], (name, env)
                # Tomislav-RetCtx: GOMAXPROCS pinned to the CPU limit when the case asks for it.
                if case.get('collector_gomaxprocs') == 'auto':
                    assert env.get('GOMAXPROCS') == str(cpu_count(wanted['cpu'])), (name, env.get('GOMAXPROCS'))
                if case.get('collector_image_override'):
                    assert container['image'] == case['collector_image_override'], (name, container['image'])
                if kind in BRIDGES:
                    response = json.loads(get_http(f"http://{pod['status']['podIP']}:8080/getFullConfig"))
                    assert response['config'] == (case.get('discovery') or app_of(case)['discovery']), response
                    write_json(directory / f'discovery-{pod["spec"]["nodeName"]}.json', response)


def namespace_exists(namespace):
    import subprocess
    return subprocess.run(['kubectl', 'get', 'namespace', namespace], capture_output=True).returncode == 0


def check_initialized(case, directory):
    """Tomislav-RetCtx: apps whose services seed their own databases (hotel) are verified before
    any load: exact row counts per collection (a restarted service re-runs its initializer and
    duplicates rows), zero restarts on every pod, and one live request through the entry service
    that must return data. Written to init-check.json; raises on any mismatch."""
    app = app_of(case)
    if app.get('social_graph'):
        # Tomislav-RetCtx: real-work Social Network: seed the social graph exactly as run_dsb_sn_e2e.deploy_seed
        # (962 users, 37624 follows through the wrk2api entry service); raise unless both counts come back.
        import re, subprocess, sys as _sys
        log = directory / 'seed.log'
        with log.open('w') as out:
            subprocess.run([_sys.executable, '/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/init_social_graph.py',
                            '--ip', '10.10.1.1', '--port', str(app['nodeport']), '--limit', '200'],
                           cwd='/users/tomislav/DeathStarBench/socialNetwork', timeout=900, stdout=out, stderr=subprocess.STDOUT, check=True)
        text = log.read_text()
        assert 'Failed:' not in text and re.findall(r'Succeeded:\s*(\d+)', text) == ['962', '37624'], text[-2000:]
        write_json(directory / 'init-check.json', {'checked': now(), 'social_graph': 'users 962, follows 37624'})
        return None
    if not app['seed']:
        return None
    namespace, variant = case['namespace'], case['variant']
    pods = json.loads(kube(namespace, 'get', 'pods', '-l', f'retctx-e2e={variant}', '-o', 'json'))['items']
    by_base = {p['metadata']['name'].split('-' + variant + '-ctr')[0]: p for p in pods}
    result = {'checked': now(), 'counts': {}, 'restarts': {}, 'probe': None}
    for base, collections in app['seed'].items():
        pod = by_base[base]['metadata']['name']
        for database, collection, expected in collections:
            out = kube(namespace, 'exec', pod, '--', 'mongo', '--quiet', '--eval',
                       f'db.getSiblingDB("{database}").getCollection("{collection}").countDocuments({{}})', timeout=60)
            count = int(out.strip().splitlines()[-1])
            result['counts'][f'{database}.{collection}'] = {'expected': expected, 'found': count}
    # Tomislav-RetCtx: restarts summed per base over ALL its pods (the collector DaemonSet has 8;
    # keying by base alone kept only one of them). The trace store is recorded but not asserted:
    # Jaeger exits until Elasticsearch accepts connections, which gives jaeger exactly 2 restarts
    # on every Jaeger+ES deployment of the SN campaigns too (ready-pods.json of retctx-nwe2e-m5-*).
    # The rule exists for the seeding services (a restart re-runs the initializer) and the
    # databases/caches (a restart loses the seeded data), and those are all asserted.
    for pod in pods:
        base = pod['metadata']['name'].split('-' + variant + '-ctr')[0]
        result['restarts'][base] = result['restarts'].get(base, 0) + sum(
            c.get('restartCount', 0) for c in pod['status'].get('containerStatuses', []))
    STORE = ('jaeger', 'elasticsearch')
    result['restarts_not_asserted'] = {b: n for b, n in result['restarts'].items() if b in STORE}
    if app_name(case) == 'hotel':
        url = (entry_url(app) + '/SearchHandler?inDate=2015-04-09&outDate=2015-04-10'
               '&lat=37.7867&lon=-122.4112')  # hotel 1's coordinates
        body = json.loads(get_http(url, timeout=30))
        hotels = body.get('Ret0') or []
        result['probe'] = {'url': url, 'hotels_returned': len(hotels),
                           'ids': sorted(str(h.get('ID', '?')) for h in hotels)}
        # Tomislav-RetCtx: pre-fill the geo service's latitude cache one request at a time.
        # github.com/hailocab/go-geoindex (the DSB geo service's index) memoizes the length of a
        # degree of longitude per 0.1-degree latitude bin in a package-global map with no lock
        # (point.go lonDegreeDistance.get). The first concurrent queries that land in a new bin
        # race on it ("fatal error: concurrent map read and map write"): the geo service exits
        # and restarts, failing requests (seen twice, both in 10 rps smokes right after deploy).
        # Once every bin the workload touches exists the map is read-only, and concurrent reads
        # are safe. search-hotels.lua draws lat in 38.0235 +- 0.2405, so a sequential sweep of
        # 37.70..38.40 fills every bin before any concurrent load. No application code changes.
        swept = []
        for i in range(15):
            lat = 37.70 + 0.05 * i
            sweep = (entry_url(app) + '/SearchHandler?inDate=2015-04-09&outDate=2015-04-10'
                     f'&lat={lat:.4f}&lon=-122.0950')
            swept.append({'lat': round(lat, 4), 'hotels': len(json.loads(get_http(sweep, timeout=30)).get('Ret0') or [])})
        result['geo_cache_warmup'] = {'note': 'sequential latitude sweep, see comment in check_initialized',
                                      'requests': swept}
    write_json(directory / 'init-check.json', result)
    bad = {k: v for k, v in result['counts'].items() if v['expected'] != v['found']}
    assert not bad, f'seeded row counts differ: {bad}'
    restarted = {b: n for b, n in result['restarts'].items() if n and b not in STORE}
    assert not restarted, f"pods restarted during start-up: {restarted}"
    assert result['probe'] is None or result['probe']['hotels_returned'] > 0, result['probe']
    return result


def deploy(case, directory):
    namespace = case['namespace']
    # Tomislav-RetCtx: one application on the cluster at a time -- evict every other app's
    # campaign resources (e.g. a Social Network stack in dsb-sn) before deploying this one.
    for other in sorted({a['namespace'] for a in APPS.values()} - {namespace}):
        if namespace_exists(other):
            (directory / f'teardown-{other}').mkdir(exist_ok=True)
            teardown(other, directory / f'teardown-{other}')
    if not namespace_exists(namespace):
        import subprocess
        subprocess.run(['kubectl', 'create', 'namespace', namespace], check=True, capture_output=True)
    teardown(namespace, directory)
    source = Path(case['case']) / 'manifest.yaml'
    expected = json.loads((source.parent / 'build-complete.json').read_text())['manifest_sha256']
    assert hashlib.sha256(source.read_bytes()).hexdigest() == expected
    manifest = directory / 'applied.yaml'
    manifest.write_bytes(source.read_bytes())
    kube(namespace, 'apply', '-f', str(manifest), timeout=180)
    verify_deployment(case, directory)
    if case.get('backend_tuning') and case.get('collector_profile') != 'sink':
        tune_elasticsearch(case, directory)
    check_initialized(case, directory)


def collector_totals(snap):
    totals = {}
    for metrics in snap['collectors'].values():
        for name, value in metrics.items():
            if any(word in name for word in ('accepted_spans', 'refused_spans')):
                totals[name] = totals.get(name, 0.) + value
    return totals


def smoke_checks(case, point, after):
    kind = case['kind']
    profile = case.get('collector_profile', 'passthrough')
    totals = collector_totals(after)
    assert totals.get('otelcol_receiver_refused_spans_total', 0) == 0, totals
    pods = json.loads(kube(case['namespace'], 'get', 'pods', '-l', f'retctx-e2e={case["variant"]}', '-o', 'json'))['items']
    jaeger = next(p for p in pods if p['metadata']['name'].startswith('jaeger-'))
    services = json.loads(get_http(f"http://{jaeger['status']['podIP']}:16686/api/services")).get('data') or []
    if kind == 'nt':
        assert not any(app_of(case)['entry_trace_service'] in s or '_service_' in s for s in services), services
        assert totals.get('otelcol_receiver_accepted_spans_total', 0) == 0, totals
        return {'services': services, 'collector_totals': totals}
    assert totals.get('otelcol_receiver_accepted_spans_total', 0) > 0, totals
    if profile == 'sink':
        sdk = [m['_processor_metrics'] for name, m in after['sdk'].items()
               if '-service-' in name and '_processor_metrics' in m]
        assert sdk, 'no SDK metrics logged'
        summary = {'services': services, 'collector_totals': totals, 'stored_traces': 'none (nop exporter)',
                   'sdk_spans_sent': sum(m.get('spans_sent', 0) for m in sdk),
                   'sdk_spans_dropped': sum(m.get('spans_dropped', 0) for m in sdk)}
        assert summary['sdk_spans_sent'] > 0, summary
        assert summary['sdk_spans_dropped'] == 0, summary
        if kind in BRIDGES:
            rt = [m['BRIDGES_RT'] for m in after['sdk'].values() if 'BRIDGES_RT' in m]
            summary['reverse'] = {key: sum(m.get(key, 0) for m in rt)
                                  for key in ('leaf_rejects', 'checkpoints', 'received')}
        return summary
    traces = sample_traces(case, point)
    assert traces.get('data'), 'no stored smoke traces'
    sdk = [m['_processor_metrics'] for name, m in after['sdk'].items() if '-service-' in name and '_processor_metrics' in m]
    assert sdk, 'no SDK metrics logged'
    summary = {'services': services, 'collector_totals': totals, 'traces': len(traces['data']),
               'sdk_spans_sent': sum(m.get('spans_sent', 0) for m in sdk),
               'sdk_spans_dropped': sum(m.get('spans_dropped', 0) for m in sdk)}
    assert summary['sdk_spans_dropped'] == 0, summary
    if kind in BRIDGES and case.get('reverse_truss', 'on') == 'on':
        rt = [m['BRIDGES_RT'] for m in after['sdk'].values() if 'BRIDGES_RT' in m]
        summary['reverse'] = {key: sum(m.get(key, 0) for m in rt) for key in ('leaf_rejects', 'checkpoints', 'received')}
        assert all(v > 0 for v in summary['reverse'].values()), summary['reverse']
        returned = [t for trace in traces['data'] for span in trace['spans']
                    for t in retctx_wire.checkpoint_tags(span)]
        assert returned, 'no stored reverse-checkpoint payloads'
        summary['reverse_payloads_in_sample'] = len(returned)
    elif kind in BRIDGES:
        # Response path off: no reverse payloads should exist at all, and every childless
        # server span must be a checkpoint. Assert the absence rather than skip silently.
        returned = [t for trace in traces['data'] for span in trace['spans']
                    for t in retctx_wire.checkpoint_tags(span)]
        assert not returned, f'{len(returned)} reverse payloads with REVERSE_TRUSS=off'
        summary['reverse'] = 'off: no response-path propagation, no leaf rejection'
    if profile == 'admission' and kind in BRIDGES:
        # The priority processor logs hp/lp counters; all eight collectors must report them,
        # otherwise the pipeline silently fell back to plain batching.
        counters = [m['_processor_metrics'] for name, m in after['sdk'].items()
                    if name.startswith('otelcol-') and '_processor_metrics' in m]
        assert len(counters) == 8, len(counters)
        assert sum(m.get('hp_admitted', 0) for m in counters) > 0, counters
        assert sum(m.get('lp_admitted', 0) for m in counters) > 0, counters
        summary['priority'] = {key: sum(m.get(key, 0) for m in counters)
                               for key in ('hp_admitted', 'lp_admitted', 'hp_refused', 'lp_refused', 'gc_count')}
        assert summary['priority']['hp_refused'] == 0, summary['priority']
    return summary


def campaign(root, mode, skip_smoke=False):
    assert json.loads((root / 'image-build-status.json').read_text())['state'] == 'complete'
    plan = json.loads((root / 'plan.json').read_text())
    cases = json.loads((root / 'cases.json').read_text())
    check_sources(root)
    if mode == 'run':
        # Tomislav-RetCtx: --skip-smoke (user 2026-09-23): each case's deploy is still verified
        # (verify_deployment, check_initialized) before its warm-up; only the 10 rps smoke ramp is skipped.
        if skip_smoke:
            write_json(root / 'smoke-skipped.json', {'skipped': now(), 'reason': 'run --skip-smoke'})
        else:
            assert json.loads((root / 'smoke-complete.json').read_text())['passed']
        selected = cases
    else:
        selected = [c for c in cases if c['sample_ratio'] == 1]
    status_path = root / f'{mode}-status.json'
    # Tomislav-RetCtx: repetitions rotate the case order (as the real-work run does)
    # so no kind always runs first; each repetition uses its own workload seed.
    repetitions = 1 if mode == 'smoke' else int(plan.get('repetitions', 1))
    runs = 0
    for rep in range(repetitions):
        seed = plan['seeds'][rep]
        order = selected[rep % len(selected):] + selected[:rep % len(selected)]
        for case in order:
            name = case['name']
            directory = root / mode / f'{rep + 1:02d}-{name}'
            runs += 1
            if (directory / 'complete.json').exists():
                continue
            if directory.exists():
                directory.rename(directory.with_name(directory.name + '-interrupted-' + str(time.time_ns())))
            directory.mkdir(parents=True)
            state = {'state': 'running', 'mode': mode, 'repetition': rep + 1, 'case': name, 'kind': case['kind'],
                     'sample_ratio': case['sample_ratio'], 'stage': 'deploy', 'updated': now()}
            write_json(status_path, state)
            deploy(case, directory)
            if mode == 'run':
                state.update(stage='warmup', updated=now()); write_json(status_path, state)
                warm = run_wrk(directory / 'warmup', plan['warmup_rps'], plan['warmup_seconds'], seed,
                               connections=plan['warmup_connections'], app=app_name(case))
                assert warm['non_2xx_3xx'] == 0 and not any(warm['socket_errors'].values()), warm
                if case.get('backend_tuning') and case['kind'] != 'nt' \
                        and case.get('collector_profile') != 'sink':
                    time.sleep(15)  # first refresh of the fresh index
                    verify_index(case, directory)
            rates = [10] if mode == 'smoke' else plan['ramp_rates']
            # Tomislav-RetCtx (user 2026-09-24): every kind stops at ITS OWN plateau (plan['plateau_stop']) and then
            # gets adaptive repeat windows in its knee region (plan['knee_windows']); both absent = the fixed ramp.
            plateau = plan.get('plateau_stop') if mode == 'run' else None
            # Tomislav-RetCtx (user 2026-09-24): or N full ramps back to back in the one deployment
            # (plan['ramp_passes']): pass 1 climbs to the kind's plateau and fixes its grid, passes 2..N re-run exactly
            # that grid (same number of trials at every point), gap_seconds apart so the overloaded tail drains. Pass 1
            # is <case>/rate-*, pass k is <case>/pass-<kk>/rate-* (each laid out like a case directory).
            passes = plan.get('ramp_passes') if mode == 'run' else None
            assert not (passes and plan.get('knee_windows')), 'ramp_passes and knee_windows are alternatives'
            # Tomislav-RetCtx (user 2026-09-24, SN real-work: fresh databases, caches and seeding every pass): every
            # repetition is its own fresh deployment; repetition 1 fixes this kind's grid (its plateau stop), later
            # repetitions re-run exactly that grid, so every point has the same number of trials.
            if mode == 'run' and rep > 0 and plan.get('repeat_grid'):
                first = root / mode / f'01-{name}'
                assert (first / 'complete.json').exists(), f'repeat_grid: {first} not complete'
                rates = sorted(json.loads(f.read_text())['offered_rps'] for f in first.glob('rate-*/result.json'))
                assert rates, first
                plateau = None

            def climb(base, rates, seed, plateau, pass_no):
                best, flat, measured = 0.0, 0, []
                for rate in rates:
                    state.update(stage='measuring', offered_rps=rate, updated=now(), **({'pass': pass_no} if passes else {}))
                    write_json(status_path, state)
                    point = base / f'rate-{rate:05d}'
                    point.mkdir(parents=True)
                    before = snapshot(case['namespace'], case['variant'], point / 'before')
                    # Tomislav-RetCtx: a bursty generator is sized for its PEAK epoch rate so the
                    # connection pool never throttles a spike; smoke stays fixed-interval.
                    generator = None if mode == 'smoke' else plan.get('generator')
                    peak = rate * (generator or {}).get('peak_multiplier', 1.0)
                    connections = 10 if mode == 'smoke' else connections_for(peak)
                    result = run_wrk(point, rate, 30 if mode == 'smoke' else plan['seconds_per_rate'], seed,
                                     connections=connections, generator=generator, app=app_name(case))
                    after = snapshot(case['namespace'], case['variant'], point / 'after')
                    result['collector_deltas'], result['counter_resets'] = counter_deltas(before, after)
                    result['snapshot_errors'] = before['errors'] + after['errors']
                    result['restarts_changed'] = before['restarts'] != after['restarts']
                    result['connection_cap'] = CONNECTION_CAP
                    result.update(kind=case['kind'], case=name, sample_ratio=case['sample_ratio'], repetition=rep + 1,
                                  **({'pass': pass_no} if passes else {}))
                    write_json(point / 'result.json', result)
                    if mode == 'smoke':
                        assert result['non_2xx_3xx'] == 0 and not any(result['socket_errors'].values()), result
                        assert not result['snapshot_errors'], result['snapshot_errors']
                        if case.get('backend_tuning') and case['kind'] != 'nt' \
                                and case.get('collector_profile') != 'sink':
                            time.sleep(15)
                            verify_index(case, directory)
                        write_json(point / 'smoke-checks.json', smoke_checks(case, point, after))
                    elif case['kind'] != 'nt' and case.get('collector_profile') != 'sink' and plan.get('trace_capture', True):
                        capture_traces(case, point, result)
                    measured.append(result)
                    if plateau:
                        if result['completed_rps'] >= best * (1 + plateau['min_gain']):
                            best, flat = result['completed_rps'], 0
                        else:
                            flat += 1
                        if flat >= plateau['flat_points']:
                            write_json(base / 'plateau-stop.json', {'stopped_after': rate, 'best_delivered': best,
                                                                    'rule': plateau, 'at': now()})
                            break
                return measured

            # Tomislav-RetCtx: first_pass > 1 tops up an existing n=1 sweep (its ramp is pass 1): this root's case
            # directory holds pass first_pass, pass-kk the rest, and every pass keeps the seed of its pass number.
            first = (passes or {}).get('first_pass', 1)
            measured = climb(directory, rates, seed + 1000 * (first - 1), plateau, first)
            points = len(measured)
            if passes:
                grid = [r['offered_rps'] for r in measured]
                for k in range(first + 1, first + passes['passes']):
                    state.update(stage='pass-gap', updated=now()); write_json(status_path, state)
                    time.sleep(passes['gap_seconds'])
                    points += len(climb(directory / f'pass-{k:02d}', grid, seed + 1000 * (k - 1), None, k))
                write_json(directory / 'ramp-passes.json', {'passes': passes['passes'], 'grid': grid, 'config': passes,
                                                            'pass_seeds': [seed + 1000 * (k - 1) for k in range(first, first + passes['passes'])],
                                                            'finished': now()})
            if mode == 'run' and plan.get('knee_windows') and measured:
                knee_windows(case, directory, plan, measured, seed, state, status_path)
            # Tomislav-RetCtx (user 2026-09-24): plan trace_capture False = no trace sampling in performance runs
            if mode == 'run' and case['kind'] != 'nt' and case.get('collector_profile') != 'sink' and plan.get('trace_capture', True):
                state.update(stage='settle-trace-samples', updated=now()); write_json(status_path, state)
                settle_trace_samples(case, directory)
            state.update(stage='capture-final', updated=now()); write_json(status_path, state)
            snapshot(case['namespace'], case['variant'], directory / 'final')
            if case.get('backend_tuning') and case['kind'] != 'nt' \
                    and case.get('collector_profile') != 'sink':
                backend_state(case, directory)
            if mode == 'run' and case['kind'] != 'nt' and case.get('collector_profile') != 'sink' and plan.get('trace_capture', True):
                capture_traces(case, directory)
            write_json(directory / 'complete.json', {'finished': now(), 'points': points, 'seed': seed, 'repetition': rep + 1,
                                                     'case': name, 'kind': case['kind'], 'sample_ratio': case['sample_ratio']})
    write_json(status_path, {'state': 'complete', 'updated': now(), 'runs': runs})
    write_json(root / f'{mode}-complete.json', {'passed': True, 'finished': now(), 'runs': runs})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('smoke', 'run'))
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--skip-smoke', action='store_true', help='run without a prior smoke (deploy checks still run)')
    args = parser.parse_args()
    root = args.out.resolve()
    try:
        campaign(root, args.mode, skip_smoke=args.skip_smoke)
    except Exception as error:
        write_json(root / f'{args.mode}-status.json', {'state': 'failed', 'mode': args.mode,
                                                        'updated': now(), 'error': repr(error)})
        raise
