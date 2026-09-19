#!/usr/bin/env python3
"""Tomislav-RetCtx: real-work, namespace-aware Social Network evaluation.

Smoke checks precede measured ramps. Completed runs survive interruption; an
incomplete run is archived and restarted with fresh state, because later ramp
steps depend on the preceding workload's database/cache state.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import gzip
import hashlib
import json
import retctx_wire
import math
import os
from pathlib import Path
import re
import resource
import signal
import subprocess
import time
import urllib.parse
import urllib.request

import yaml

from prepare_dsb_sn_e2e import REPO, DSB, write_json

LUA = DSB / 'scripts/compose-post.lua'
SEED = DSB / 'scripts/init_social_graph.py'


def now():
    return datetime.now(timezone.utc).isoformat()


def execute(argv, timeout=60, **kwargs):
    return subprocess.run(list(map(str, argv)), check=True, timeout=timeout, **kwargs)


def kube(namespace, *args, timeout=60):
    return execute(['kubectl', '-n', namespace, *args], timeout=timeout,
                   capture_output=True, text=True).stdout


def get_http(url, timeout=10):
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(url, timeout=timeout) as response:
        return response.read().decode()


def milliseconds(value):
    match = re.fullmatch(r'([\d.]+)(us|ms|s|m|h)', value.strip())
    if not match:
        raise ValueError(f'invalid duration: {value}')
    return float(match[1]) * {'us': .001, 'ms': 1, 's': 1000, 'm': 60000, 'h': 3600000}[match[2]]


def parse_wrk(output):
    # The explicit HDR table works for both upstream Max and DSB 99% headers.
    tables = output.split('Latency Distribution (HdrHistogram -')
    if len(tables) != 2:
        raise ValueError('expected exactly one recorded HDR latency distribution')
    hdr = tables[1]
    percentiles = {float(p): milliseconds(v) for p, v in
                   re.findall(r'^\s*(\d+\.\d+)%\s+([\d.]+(?:us|ms|s|m|h))\s*$', hdr, re.M)}
    if not {50., 99., 100.}.issubset(percentiles):
        raise ValueError('missing HDR percentiles')
    stats = re.search(r'^\s*Latency\s+(\S+)', output, re.M)
    mean = re.search(r'#\[Mean\s*=\s*([\d.]+)', hdr)
    p95 = re.search(r'^\s*([\d.]+)\s+0\.950000\s+\d+\s+', hdr, re.M)
    complete = re.search(r'^\s*(\d+) requests in ([\d.]+(?:us|ms|s|m|h)),', output, re.M)
    sent = re.search(r'^Sent (\d+) requests', output, re.M)
    rps = re.search(r'^Requests/sec:\s*([\d.]+)', output, re.M)
    if not (stats and complete and sent and rps and p95):
        raise ValueError('incomplete wrk output: expected -r -L and full percentile spectrum')
    non_2xx = re.search(r'Non-2xx or 3xx responses:\s*(\d+)', output)
    errors = re.search(r'Socket errors: connect (\d+), read (\d+), write (\d+), timeout (\d+)', output)
    completed = int(complete[1])
    seconds = milliseconds(complete[2]) / 1000
    failed = int(non_2xx[1]) if non_2xx else 0
    assert 0 <= failed <= completed <= int(sent[1]) and seconds > 0
    return {
        'completed_requests': completed, 'sent_requests': int(sent[1]), 'wrk_seconds': seconds,
        'completed_rps': float(rps[1]), 'successful_rps': (completed - failed) / seconds,
        'non_2xx_3xx': failed,
        'socket_errors': dict(zip(('connect', 'read', 'write', 'timeout'),
                                 map(int, errors.groups() if errors else ('0',)*4))),
        'mean_ms': float(mean[1]) if mean else milliseconds(stats[1]),
        'p50_ms': percentiles[50.], 'p95_ms': float(p95[1]),
        'p99_ms': percentiles[99.], 'max_ms': percentiles[100.],
    }


def prometheus(text):
    metrics = {}
    for name, value in re.findall(r'^(otelcol_[^\s{]+)(?:\{.*\})?\s+(\S+)', text, re.M):
        number = float(value)
        if math.isfinite(number):
            metrics[name] = metrics.get(name, 0.) + number
    return metrics


def log_metrics(text):
    found = {}
    for line in text.splitlines():
        key = next((key for key in ('_processor_metrics', 'BRIDGES_RT') if key in line), None)
        if key:
            found[key] = {name: float(value) for name, value in
                          re.findall(r'"?(\w+)"?\s*(?:=|:)\s*(-?\d+(?:\.\d+)?(?:e[+-]?\d+)?)', line)}
    return found


def snapshot(namespace, variant, directory):
    directory.mkdir()
    start = now()
    pods = json.loads(kube(namespace, 'get', 'pods', '-l', f'retctx-e2e={variant}', '-o', 'json'))
    write_json(directory / 'pods.json', pods)
    jobs = []
    with ThreadPoolExecutor(max_workers=16) as pool:
        for pod in pods['items']:
            name = pod['metadata']['name']
            ip = pod['status'].get('podIP')
            if ip and name.startswith('jaeger-'):
                jobs.append((pool.submit(get_http, f'http://{ip}:14269/metrics'), 'backend', name))
            if ip and name.startswith('elasticsearch-'):
                jobs.append((pool.submit(get_http, f'http://{ip}:9200/_nodes/stats/thread_pool,jvm,process'),
                             'backend', name))
            if name.startswith('otelcol-') and pod['status'].get('podIP'):
                jobs.append((pool.submit(get_http, f"http://{pod['status']['podIP']}:8888/metrics"),
                             'prometheus', name))
            if '-service-' in name or name.startswith(('otelcol-', 'jaeger-', 'elasticsearch-')):
                jobs.append((pool.submit(kube, namespace, 'logs', name, '--tail=2000'), 'logs', name))
        for i in range(1, 10):
            node = f'node-{i}'
            jobs.append((pool.submit(kube, namespace, 'get', '--raw',
                         f'/api/v1/nodes/{node}/proxy/stats/summary'), 'node', node))
        data = {'started': start, 'collectors': {}, 'sdk': {}, 'cpu': {}, 'errors': []}
        for future, kind, name in jobs:
            try:
                text = future.result()
                with gzip.open(directory / f'{kind}-{name}.txt.gz', 'wt') as stream:
                    stream.write(text)
                if kind == 'prometheus':
                    data['collectors'][name] = prometheus(text)
                elif kind == 'logs':
                    data['sdk'][name] = log_metrics(text)
                elif kind == 'node':
                    for pod in json.loads(text).get('pods', []):
                        ref = pod.get('podRef', {})
                        if ref.get('namespace') == namespace and variant in ref.get('name', ''):
                            data['cpu'][ref['uid']] = {
                                'name': ref['name'], 'node': name,
                                'cpu_ns': sum(c.get('cpu', {}).get('usageCoreNanoSeconds', 0)
                                              for c in pod.get('containers', [])),
                                'working_set_bytes': sum(c.get('memory', {}).get('workingSetBytes', 0)
                                                         for c in pod.get('containers', [])),
                            }
            except Exception as error:
                data['errors'].append({'kind': kind, 'name': name, 'error': str(error)})
    data['finished'] = now()
    data['restarts'] = {p['metadata']['uid']: sum(c.get('restartCount', 0) for c in
                        p['status'].get('containerStatuses', [])) for p in pods['items']}
    write_json(directory / 'snapshot.json', data)
    return data


def counter_deltas(before, after):
    result = {}
    resets = []
    for pod in before['collectors'].keys() | after['collectors'].keys():
        if pod not in before['collectors'] or pod not in after['collectors']:
            resets.append(pod)
            continue
        metrics = before['collectors'][pod]
        current = after['collectors'][pod]
        for name in metrics.keys() | current.keys():
            if not any(word in name for word in ('accepted_spans', 'refused_spans', 'sent_spans',
                                                  'send_failed_spans', 'enqueue_failed_spans')):
                continue
            # Tomislav-RetCtx: counters first appear when an event occurs;
            # disappearing counters are missing evidence, not zero deltas.
            if name not in current:
                resets.append(pod + '/' + name + '/missing')
                continue
            delta = current[name] - metrics.get(name, 0.)
            if delta < 0:
                resets.append(pod + '/' + name)
            else:
                result[name] = result.get(name, 0.) + delta
    return result, resets


def run_wrk(directory, rate, seconds, seed, connections=None):
    directory.mkdir(parents=True, exist_ok=True)
    # Tomislav-RetCtx: the 5k schedule needs 1,250 connections plus worker/Lua
    # descriptors. The login shell's 1,024 soft limit fails during initialization.
    soft, hard = resource.getrlimit(resource.RLIMIT_NOFILE)
    target = soft if soft == resource.RLIM_INFINITY else max(soft, 16384)
    assert hard == resource.RLIM_INFINITY or hard >= target, 'insufficient hard file-descriptor limit'
    resource.setrlimit(resource.RLIMIT_NOFILE, (target, hard))
    if connections is None:
        connections = max(1, math.ceil(rate * rate / 20000))
    threads = max(1, math.ceil(connections / 10))
    argv = ['wrk', '-t', str(threads), '-c', str(connections), '-d', f'{seconds}s',
            '-r', '-L', '-s', str(LUA), 'http://10.10.1.1:23229', '-R', str(rate)]
    record = {'argv': argv, 'seed': seed, 'offered_rps': rate, 'offer_seconds': seconds,
              'started': now(), 'connections': connections, 'threads': threads,
              'file_descriptor_limit': list(resource.getrlimit(resource.RLIMIT_NOFILE))}
    write_json(directory / 'command.json', record)
    usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    start = time.monotonic()
    with (directory / 'wrk.stdout').open('w') as stdout, (directory / 'wrk.stderr').open('w') as stderr:
        process = subprocess.Popen(argv, stdout=stdout, stderr=stderr, start_new_session=True,
                                   env=dict(os.environ, RANDOM_SEED=str(seed), max_user_index='962', LC_ALL='C'))
        try:
            code = process.wait(timeout=seconds + 60)
        except BaseException:
            os.killpg(process.pid, signal.SIGTERM)
            process.wait(timeout=15)
            raise
    assert code == 0, f'wrk exited {code}; inspect {directory}'
    end_usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    record.update(parse_wrk((directory / 'wrk.stdout').read_text()))
    record['finished'] = now()
    record['wall_seconds'] = time.monotonic() - start
    record['generator_cpu_seconds'] = end_usage.ru_utime + end_usage.ru_stime - usage.ru_utime - usage.ru_stime
    write_json(directory / 'result.json', record)
    return record


def owned_resources(namespace):
    items = json.loads(kube(namespace, 'get', 'deployments,daemonsets,services,configmaps', '-o', 'json'))['items']
    return [d for d in items if d['metadata'].get('labels', {}).get('retctx-e2e') or
            (d['metadata'].get('labels', {}).get('io.kompose.service') and d['metadata']['name'].endswith('-ctr'))]


def teardown(namespace, directory):
    documents = owned_resources(namespace)
    if not documents:
        return
    path = directory / 'deleted-resources.json'
    write_json(path, {'apiVersion': 'v1', 'kind': 'List', 'items': [
        {'apiVersion': d['apiVersion'], 'kind': d['kind'], 'metadata': {
            'name': d['metadata']['name'], 'namespace': namespace}} for d in documents]})
    kube(namespace, 'delete', '-f', str(path), '--wait=true', '--timeout=180s', timeout=200)
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        pods = json.loads(kube(namespace, 'get', 'pods', '-o', 'json'))['items']
        if not any(p['metadata'].get('labels', {}).get('io.kompose.service') for p in pods):
            return
        time.sleep(3)
    raise RuntimeError('old application pods did not terminate')


def ready(namespace, variant):
    deadline = time.monotonic() + 600
    while time.monotonic() < deadline:
        pods = json.loads(kube(namespace, 'get', 'pods', '-l', f'retctx-e2e={variant}', '-o', 'json'))['items']
        if len(pods) == 33 and all(any(c['type'] == 'Ready' and c['status'] == 'True'
                                     for c in p['status'].get('conditions', [])) for p in pods):
            return pods
        time.sleep(5)
    raise RuntimeError(f'{variant}: expected 25 deployments and eight Ready collector pods')


def verify_deployment(case, directory, smoke):
    namespace, variant = case['namespace'], case['variant']
    pods = ready(namespace, variant)
    write_json(directory / 'ready-pods.json', {'items': pods})
    collectors = [p for p in pods if p['metadata']['name'].startswith('otelcol-')]
    assert {p['spec']['nodeName'] for p in collectors} == {f'node-{i}' for i in range(1, 9)}
    for pod in pods:
        name = pod['metadata']['name']
        for container in pod['spec']['containers']:
            assert '@sha256:' in container['image'], name
            env = {e['name']: e.get('value') for e in container.get('env', [])}
            if '-service-' in name:
                assert env['BRIDGE_KIND'] == case['kind'] and env['GOMAXPROCS'] == '8'
                assert env['OTLP_RETRY'] == 'off' and env['OTEL_SAMPLE_RATIO'] == '1'
                assert env['REVERSE_TRUSS'] == ('off' if case['kind'] == 'v' else 'on')
                assert env['RT_LEAF_REJECT'] == ('0' if case['kind'] == 'v' else '1')
            if name.startswith('otelcol-'):
                assert container['resources'] == {key: {'cpu': '500m', 'memory': '256Mi'}
                                                  for key in ('requests', 'limits')}
                if case['kind'] != 'v':
                    response = json.loads(get_http(f"http://{pod['status']['podIP']}:8080/getFullConfig"))
                    assert response['config'] == {'cpd_min': 2, 'cpd_max': 6, 'reverse_policy': 'inverse_depth'}
                    write_json(directory / f'discovery-{pod["spec"]["nodeName"]}.json', response)


def deploy_seed(case, directory, smoke):
    namespace, variant = case['namespace'], case['variant']
    teardown(namespace, directory)
    source = Path(case['case']) / 'manifest.yaml'
    expected = json.loads((source.parent / 'build-complete.json').read_text())['manifest_sha256']
    assert hashlib.sha256(source.read_bytes()).hexdigest() == expected
    documents = list(yaml.safe_load_all(source.read_text()))
    if smoke:
        for doc in documents:
            if doc['kind'] == 'Deployment' and '-service-' in doc['metadata']['name']:
                for container in doc['spec']['template']['spec']['containers']:
                    for env in container.get('env', []):
                        if env['name'] == 'RT_SAMPLE':
                            env['value'] = '1'
    manifest = directory / 'applied.yaml'
    manifest.write_text(yaml.safe_dump_all(documents, sort_keys=False))
    kube(namespace, 'apply', '-f', str(manifest), timeout=180)
    verify_deployment(case, directory, smoke)
    with (directory / 'seed.log').open('w') as log:
        execute([os.sys.executable, SEED, '--ip', '10.10.1.1', '--port', '23229', '--limit', '200'],
                cwd='/users/tomislav/DeathStarBench/socialNetwork', timeout=900,
                stdout=log, stderr=subprocess.STDOUT)
    output = (directory / 'seed.log').read_text()
    assert 'Failed:' not in output and re.findall(r'Succeeded:\s*(\d+)', output) == ['962', '37624'], output[-2000:]
    verify_deployment(case, directory, smoke)


def sample_traces(case, directory, window=None):
    pods = json.loads(kube(case['namespace'], 'get', 'pods', '-l', f'retctx-e2e={case["variant"]}', '-o', 'json'))['items']
    jaeger = next(p for p in pods if p['metadata']['name'].startswith('jaeger-'))
    base = f"http://{jaeger['status']['podIP']}:16686/api"
    services = json.loads(get_http(base + '/services'))
    names = [s for s in services.get('data', []) if 'wrk2api' in s]
    assert names, services
    parameters = {'service': names[0], 'limit': 100, 'lookback': '1h'}
    if window:
        parameters.update({key: int(datetime.fromisoformat(window[field]).timestamp() * 1_000_000)
                           for key, field in (('start', 'started'), ('end', 'finished'))})
    write_json(directory / 'trace-query.json', {'parameters': parameters, 'started': now()})
    query = urllib.parse.urlencode(parameters)
    traces = json.loads(get_http(base + '/traces?' + query, timeout=30))
    assert not traces.get('errors'), traces.get('errors')
    with gzip.open(directory / 'sample-traces.json.gz', 'wt') as stream:
        json.dump(traces, stream)
    return traces


def capture_traces(case, directory, window=None):
    # Tomislav-RetCtx: retain a sample at every rate before fresh-state teardown.
    # Query failure is recorded without discarding completed load measurements.
    try:
        return sample_traces(case, directory, window)
    except Exception as error:
        write_json(directory / 'trace-sample-error.json', {'time': now(), 'error': str(error)})
        return None


def settle_trace_samples(case, directory):
    # Tomislav-RetCtx: recent search results can be partial while ES indexes
    # asynchronously. Re-read the same IDs after the ramp, before state reset.
    pods = json.loads(kube(case['namespace'], 'get', 'pods', '-l',
                           f'retctx-e2e={case["variant"]}', '-o', 'json'))['items']
    collectors = [p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('otelcol-')]
    jaeger = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('jaeger-'))
    elastic = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('elasticsearch-'))
    start = time.monotonic()
    observations = []
    stable = 0
    while time.monotonic() - start < 120:
        observation = {'time': now(), 'elapsed_seconds': time.monotonic() - start}
        try:
            with ThreadPoolExecutor(max_workers=8) as pool:
                metrics = list(pool.map(lambda ip: prometheus(get_http(f'http://{ip}:8888/metrics')), collectors))
            observation['collector_queue'] = sum(value for m in metrics for name, value in m.items()
                                                 if 'exporter_queue_size' in name)
            text = get_http(f'http://{jaeger}:14269/metrics')
            observation['jaeger_queue'] = sum(float(v) for v in re.findall(
                r'^jaeger_collector_queue_length(?:\{.*\})?\s+(\S+)', text, re.M))
            nodes = json.loads(get_http(f'http://{elastic}:9200/_nodes/stats/thread_pool'))['nodes']
            observation['elasticsearch_pending_writes'] = sum(
                node['thread_pool']['write']['queue'] + node['thread_pool']['write']['active']
                for node in nodes.values())
            empty = all(observation[key] == 0 for key in
                        ('collector_queue', 'jaeger_queue', 'elasticsearch_pending_writes'))
            stable = stable + 1 if empty else 0
        except Exception as error:
            observation['error'] = str(error)
            stable = 0
        observations.append(observation)
        if stable >= 2 and time.monotonic() - start >= 30:
            break
        time.sleep(5)
    write_json(directory / 'trace-drain.json', {'started': observations[0]['time'], 'finished': now(),
                                               'queues_empty': stable >= 2, 'observations': observations})
    for point in sorted(directory.glob('rate-*')):
        if not (point / 'result.json').exists():
            continue
        original = point / 'sample-traces.json.gz'
        if not original.exists():
            capture_traces(case, point, json.loads((point / 'result.json').read_text()))
        try:
            initial = json.load(gzip.open(original, 'rt'))
            source = str(original.relative_to(directory))
            if not initial.get('data'):
                delayed = point / 'delayed-search'
                delayed.mkdir(exist_ok=True)
                initial = sample_traces(case, delayed, json.loads((point / 'result.json').read_text()))
                source = str((delayed / 'sample-traces.json.gz').relative_to(directory))
            ids = [trace['traceID'] for trace in initial['data']]
            assert ids, 'no indexed traces found in the measured time window after drain'
            traces = []
            for offset in range(0, len(ids), 50):
                query = urllib.parse.urlencode([('traceID', tid) for tid in ids[offset:offset+50]])
                data = json.loads(get_http(f'http://{jaeger}:16686/api/traces?{query}', timeout=30))
                assert not data.get('errors'), data.get('errors')
                traces.extend(data['data'])
            assert {t['traceID'] for t in traces} == set(ids), 'recheck lost sampled IDs'
            with gzip.open(point / 'settled-traces.json.gz', 'wt') as stream:
                json.dump({'data': traces, 'errors': None}, stream)
            write_json(point / 'settled-query.json', {'finished': now(), 'trace_ids': ids,
                                                     'source': source, 'drain_queues_empty': stable >= 2})
            (point / 'settled-traces-error.json').unlink(missing_ok=True)
        except Exception as error:
            write_json(point / 'settled-traces-error.json', {'time': now(), 'error': str(error)})


def stop_observer(root):
    path = Path('/users/tomislav/deployments/dsb-sn/cgpb-es-window-cpd-20260910/monitoring/monitor.pid')
    if path.exists():
        pid = int(path.read_text().strip())
        cmdline = Path(f'/proc/{pid}/cmdline')
        if cmdline.exists() and b'/cgpb-es-window-cpd-20260910/monitor.py' in cmdline.read_bytes():
            os.kill(pid, signal.SIGTERM)
            write_json(root / 'observer-paused.json', {'pid': pid, 'time': now()})


def campaign(root, mode):
    assert json.loads((root / 'image-build-status.json').read_text())['state'] == 'complete'
    plan = json.loads((root / 'plan.json').read_text())
    cases = json.loads((root / 'cases.json').read_text())
    source_hashes = json.loads((root / 'application-source-hashes.json').read_text())
    for name, digest in source_hashes.items():
        assert hashlib.sha256((REPO / name).read_bytes()).hexdigest() == digest, name
    if mode == 'run':
        assert json.loads((root / 'smoke-complete.json').read_text())['passed']
    stop_observer(root)
    status_path = root / f'{mode}-status.json'
    iterations = 1 if mode == 'smoke' else plan['repetitions']
    for repetition in range(iterations):
        order = cases[repetition % 4:] + cases[:repetition % 4]
        for case in order:
            directory = root / mode / f'{repetition+1:02d}-{case["kind"]}'
            if (directory / 'complete.json').exists():
                continue
            if directory.exists():
                directory.rename(directory.with_name(directory.name + '-interrupted-' + str(time.time_ns())))
            directory.mkdir(parents=True)
            state = {'state': 'running', 'mode': mode, 'repetition': repetition + 1,
                     'kind': case['kind'], 'stage': 'deploy-and-seed', 'updated': now()}
            write_json(status_path, state)
            deploy_seed(case, directory, mode == 'smoke')
            seed = plan['seeds'][repetition]
            if mode == 'run':
                state.update(stage='warmup', updated=now()); write_json(status_path, state)
                warm = run_wrk(directory / 'warmup', plan['warmup_rps'], plan['warmup_seconds'], seed,
                               connections=10)
                assert warm['non_2xx_3xx'] == 0 and not any(warm['socket_errors'].values()), warm
            rates = [10] if mode == 'smoke' else plan['ramp_rates']
            for rate in rates:
                state.update(stage='measuring', offered_rps=rate, updated=now()); write_json(status_path, state)
                point = directory / f'rate-{rate:05d}'
                point.mkdir()
                before = snapshot(case['namespace'], case['variant'], point / 'before')
                result = run_wrk(point, rate, 30 if mode == 'smoke' else plan['seconds_per_rate'], seed)
                after = snapshot(case['namespace'], case['variant'], point / 'after')
                result['collector_deltas'], result['counter_resets'] = counter_deltas(before, after)
                result['snapshot_errors'] = before['errors'] + after['errors']
                result['restarts_changed'] = before['restarts'] != after['restarts']
                result.update(kind=case['kind'], repetition=repetition + 1)
                write_json(point / 'result.json', result)
                if mode == 'smoke':
                    assert result['non_2xx_3xx'] == 0 and not any(result['socket_errors'].values()), result
                    assert not result['snapshot_errors'], result['snapshot_errors']
                    traces = sample_traces(case, point)
                    assert traces.get('data'), 'no stored smoke traces'
                    if case['kind'] != 'v':
                        rt = [m['BRIDGES_RT'] for m in after['sdk'].values() if 'BRIDGES_RT' in m]
                        assert sum(m.get('leaf_rejects', 0) for m in rt) > 0, rt
                        assert sum(m.get('checkpoints', 0) for m in rt) > 0, rt
                        assert sum(m.get('received', 0) for m in rt) > 0, rt
                        priority = [m['_processor_metrics'] for name, m in after['sdk'].items()
                                    if name.startswith('otelcol-') and '_processor_metrics' in m]
                        assert len(priority) == 8, priority
                        assert sum(m.get('hp_admitted', 0) for m in priority) > 0, priority
                        assert sum(m.get('lp_admitted', 0) for m in priority) > 0, priority
                        assert all(m['soft_limit_bytes'] == 128 * 1024**2 and
                                   m['hard_limit_bytes'] == 70 * (256 * 1024**2) // 100
                                   for m in priority), priority
                        returned = [t for trace in traces['data'] for span in trace['spans']
                                    for t in retctx_wire.checkpoint_tags(span)]
                        assert returned, 'no stored reverse-checkpoint payloads'
                else:
                    capture_traces(case, point, result)
            if mode == 'run':
                state.update(stage='settle-trace-samples', updated=now()); write_json(status_path, state)
                settle_trace_samples(case, directory)
            state.update(stage='capture-final', updated=now()); write_json(status_path, state)
            snapshot(case['namespace'], case['variant'], directory / 'final')
            if mode == 'smoke':
                sample_traces(case, directory)
            else:
                capture_traces(case, directory)
            write_json(directory / 'complete.json', {'finished': now(), 'points': len(rates), 'seed': seed})
    write_json(status_path, {'state': 'complete', 'updated': now(), 'runs': iterations * len(cases)})
    write_json(root / f'{mode}-complete.json', {'passed': True, 'finished': now(), 'runs': iterations * len(cases)})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('smoke', 'run'))
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    try:
        campaign(args.out.resolve(), args.mode)
    except Exception as error:
        path = args.out / f'{args.mode}-status.json'
        state = json.loads(path.read_text()) if path.exists() else {}
        state.update(state='failed', error=str(error), updated=now())
        write_json(path, state)
        raise
