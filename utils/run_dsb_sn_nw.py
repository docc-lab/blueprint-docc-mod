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
from prepare_dsb_sn_nw import BRIDGES, COLLECTOR_PROFILES, COLLECTOR_RESOURCES, DISCOVERY
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
    jaeger = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('jaeger-'))
    elastic = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('elasticsearch-'))
    return f'http://{jaeger}', f'http://{elastic}:9200'


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


def backend_state(case, directory):
    jaeger, elastic = backend_pods(case)
    state = {'captured': now()}
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
    pods = ready(namespace, variant)  # 25 deployments + 8 collectors
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
                    assert env['GOMAXPROCS'] == str(cpus), env
                else:
                    heap = tuning['elasticsearch_heap']
                    assert env['ES_JAVA_OPTS'] == f'-Xms{heap} -Xmx{heap}', env
            if name.startswith('otelcol-'):
                wanted = COLLECTOR_PROFILES[case.get('collector_profile', 'passthrough')]['resources']
                assert container['resources'] == {key: dict(wanted) for key in ('requests', 'limits')}, \
                    (name, container['resources'])
                assert env['GOMEMLIMIT'] == COLLECTOR_PROFILES[
                    case.get('collector_profile', 'passthrough')]['env']['GOMEMLIMIT'], (name, env)
                if kind in BRIDGES:
                    response = json.loads(get_http(f"http://{pod['status']['podIP']}:8080/getFullConfig"))
                    assert response['config'] == DISCOVERY, response
                    write_json(directory / f'discovery-{pod["spec"]["nodeName"]}.json', response)


def deploy(case, directory):
    namespace = case['namespace']
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
        assert not any('wrk2api' in s or '-service-' in s for s in services), services
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


def campaign(root, mode):
    assert json.loads((root / 'image-build-status.json').read_text())['state'] == 'complete'
    plan = json.loads((root / 'plan.json').read_text())
    cases = json.loads((root / 'cases.json').read_text())
    check_sources(root)
    if mode == 'run':
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
                               connections=plan['warmup_connections'])
                assert warm['non_2xx_3xx'] == 0 and not any(warm['socket_errors'].values()), warm
                if case.get('backend_tuning') and case['kind'] != 'nt' \
                        and case.get('collector_profile') != 'sink':
                    time.sleep(15)  # first refresh of the fresh index
                    verify_index(case, directory)
            rates = [10] if mode == 'smoke' else plan['ramp_rates']
            for rate in rates:
                state.update(stage='measuring', offered_rps=rate, updated=now()); write_json(status_path, state)
                point = directory / f'rate-{rate:05d}'
                point.mkdir()
                before = snapshot(case['namespace'], case['variant'], point / 'before')
                connections = 10 if mode == 'smoke' else connections_for(rate)
                result = run_wrk(point, rate, 30 if mode == 'smoke' else plan['seconds_per_rate'], seed,
                                 connections=connections)
                after = snapshot(case['namespace'], case['variant'], point / 'after')
                result['collector_deltas'], result['counter_resets'] = counter_deltas(before, after)
                result['snapshot_errors'] = before['errors'] + after['errors']
                result['restarts_changed'] = before['restarts'] != after['restarts']
                result['connection_cap'] = CONNECTION_CAP
                result.update(kind=case['kind'], case=name, sample_ratio=case['sample_ratio'], repetition=rep + 1)
                write_json(point / 'result.json', result)
                if mode == 'smoke':
                    assert result['non_2xx_3xx'] == 0 and not any(result['socket_errors'].values()), result
                    assert not result['snapshot_errors'], result['snapshot_errors']
                    if case.get('backend_tuning') and case['kind'] != 'nt' \
                            and case.get('collector_profile') != 'sink':
                        time.sleep(15)
                        verify_index(case, directory)
                    write_json(point / 'smoke-checks.json', smoke_checks(case, point, after))
                elif case['kind'] != 'nt' and case.get('collector_profile') != 'sink':
                    capture_traces(case, point, result)
            if mode == 'run' and case['kind'] != 'nt' and case.get('collector_profile') != 'sink':
                state.update(stage='settle-trace-samples', updated=now()); write_json(status_path, state)
                settle_trace_samples(case, directory)
            state.update(stage='capture-final', updated=now()); write_json(status_path, state)
            snapshot(case['namespace'], case['variant'], directory / 'final')
            if case.get('backend_tuning') and case['kind'] != 'nt' \
                    and case.get('collector_profile') != 'sink':
                backend_state(case, directory)
            if mode == 'run' and case['kind'] != 'nt' and case.get('collector_profile') != 'sink':
                capture_traces(case, directory)
            write_json(directory / 'complete.json', {'finished': now(), 'points': len(rates), 'seed': seed, 'repetition': rep + 1,
                                                     'case': name, 'kind': case['kind'], 'sample_ratio': case['sample_ratio']})
    write_json(status_path, {'state': 'complete', 'updated': now(), 'runs': runs})
    write_json(root / f'{mode}-complete.json', {'passed': True, 'finished': now(), 'runs': runs})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('smoke', 'run'))
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    root = args.out.resolve()
    try:
        campaign(root, args.mode)
    except Exception as error:
        write_json(root / f'{args.mode}-status.json', {'state': 'failed', 'mode': args.mode,
                                                        'updated': now(), 'error': repr(error)})
        raise
