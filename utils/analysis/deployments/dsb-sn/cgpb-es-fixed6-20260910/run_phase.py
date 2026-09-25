#!/usr/bin/env python3
"""Run one coordinated CPD phase using the repository's unchanged wrk Lua script."""
import argparse
import concurrent.futures
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
OLD = ROOT.parent / 'cgpb-es-window-cpd-20260910'
SPEC = json.loads((ROOT / 'experiment.json').read_text())
NS = SPEC['namespace']
DS = 'otelcol-' + SPEC['variant'] + '-ctr'
APPS = json.loads((ROOT / 'application-images.json').read_text())
COLLECTORS = json.loads((ROOT / 'collector-images.json').read_text())
LUA = REPO / 'examples/dsb_sn/scripts/compose-post.lua'
BEFORE = json.loads((ROOT / 'before-pods.json').read_text())['items']
BACKEND_UIDS = {p['metadata']['uid'] for p in BEFORE
                if p['metadata']['labels'].get('io.kompose.service') not in APPS
                and p['metadata']['labels'].get('io.kompose.service') != DS}
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def stamp():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z')


def report(message):
    print(stamp(), message, flush=True)


def command(*args, timeout=45):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f'{args}: {result.stderr[-3000:]} {result.stdout[-1000:]}')
    return result.stdout


def kube(*args):
    return command('kubectl', '--request-timeout=30s', '-n', NS, *args)


def pods():
    return json.loads(kube('get', 'pods', '-o', 'json'))


def label(p):
    return p['metadata']['labels'].get('io.kompose.service')


def ready(p):
    return not p['metadata'].get('deletionTimestamp') and any(
        c['type'] == 'Ready' and c['status'] == 'True'
        for c in p['status'].get('conditions', []))


def get(url):
    with OPENER.open(url, timeout=15) as response:
        return json.load(response)


def wait_for(description, predicate, timeout=600):
    deadline = time.monotonic() + timeout
    last_report = 0
    while True:
        value = predicate()
        if value:
            report(description + ': verified')
            return value
        if time.monotonic() > deadline:
            save(ROOT / 'failure-pods.json', pods())
            raise TimeoutError(description)
        if time.monotonic() - last_report > 20:
            report('Waiting: ' + description)
            last_report = time.monotonic()
        time.sleep(3)


def digest(image):
    host, rest = image.split('/', 1)
    repo, tag = rest.rsplit(':', 1)
    req = urllib.request.Request(f'http://{host}/v2/{repo}/manifests/{tag}',
        method='HEAD', headers={'Accept': 'application/vnd.docker.distribution.manifest.v2+json'})
    with OPENER.open(req, timeout=15) as response:
        return response.headers['Docker-Content-Digest']


def preflight():
    assert len(APPS) == 14
    for path, expected in json.loads((ROOT / 'workflow-source-hashes.json').read_text()).items():
        assert hashlib.sha256((REPO / path).read_bytes()).hexdigest() == expected, path
    images = list(APPS.values()) + list(COLLECTORS['phases'].values())
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        digests = dict(zip(images, pool.map(digest, images)))
    save(ROOT / 'image-digests.json', digests)
    return digests


def pause_monitor():
    path = OLD / 'monitoring/monitor.pid'
    if not path.exists():
        return
    pid = int(path.read_text())
    proc = Path(f'/proc/{pid}/cmdline')
    if not proc.exists():
        return
    cmdline = proc.read_bytes()
    assert str(OLD / 'monitor.py').encode() in cmdline, (pid, cmdline)
    os.kill(pid, signal.SIGTERM)
    wait_for('background timeline probes stopped',
             lambda: (OLD / 'monitoring/stopped-at.txt').exists(), timeout=60)
    save(ROOT / 'monitor-paused.json', {'timestamp': stamp(), 'pid': pid})


def health(directory, suffix, expected_images, digests):
    snapshot = pods()
    save(directory / f'pods-{suffix}.json', snapshot)
    items = snapshot['items']
    assert len(items) == 35 and all(ready(p) for p in items), 'Expected 35/35 Ready'
    assert {p['metadata']['uid'] for p in items if label(p) not in APPS and label(p) != DS} == BACKEND_UIDS, 'Backend pod changed'
    restarts = sum(c['restartCount'] for p in items for c in p['status'].get('containerStatuses', []))
    assert restarts == 0, f'{restarts} restarts'
    actual = []
    for p in items:
        if label(p) in expected_images:
            image = expected_images[label(p)]
            container = p['spec']['containers'][0]
            status = p['status']['containerStatuses'][0]
            assert container['image'] == image, p['metadata']['name']
            assert status['imageID'].endswith('@' + digests[image]), (p['metadata']['name'], status['imageID'])
            actual.append({'pod': p['metadata']['name'], 'node': p['spec']['nodeName'],
                           'image': image, 'image_id': status['imageID']})
    assert len(actual) == 23
    result = {'timestamp': stamp(), 'ready_pods': len(items), 'total_pods': len(items),
              'restarts': restarts, 'unchanged_backend_pods': len(BACKEND_UIDS), 'images': actual}
    save(directory / f'health-{suffix}.json', result)
    return snapshot


def configure(phase, directory, digests):
    report('Scaling all 14 applications to zero before changing collector configuration')
    kube('scale', 'deployment', *APPS, '--replicas=0')
    wait_for('all old application pods terminated',
             lambda: not any(label(p) in APPS for p in pods()['items']))
    collector = COLLECTORS['phases'][phase['name']]
    kube('set', 'image', 'daemonset/' + DS, DS + '=' + collector)

    def collectors_ready():
        items = [p for p in pods()['items'] if label(p) == DS]
        return items if len(items) == 9 and all(ready(p) and p['spec']['containers'][0]['image'] == collector for p in items) else None

    collectors = wait_for('9 collector pods running the new configuration image', collectors_ready)
    discovered = []
    for p in collectors:
        config = get(f'http://{p["status"]["podIP"]}:8080/getFullConfig')
        assert config['config'] == phase['config_map'], config
        discovered.append({'pod': p['metadata']['name'], 'node': p['spec']['nodeName'],
                           'response': config})
    save(directory / 'collector-config-discovery.json', discovered)
    report('All 9 collector config endpoints match; updating and starting the applications')
    for name, image in APPS.items():
        kube('set', 'image', 'deployment/' + name, name + '=' + image)
    kube('scale', 'deployment', *APPS, '--replicas=1')

    def applications_ready():
        items = [p for p in pods()['items'] if label(p) in APPS]
        return items if len(items) == 14 and all(ready(p) and p['spec']['containers'][0]['image'] == APPS[label(p)] for p in items) else None

    apps = wait_for('14 applications Ready with the new SDK images', applications_ready)
    logdir = directory / 'startup-logs'
    logdir.mkdir()
    discovered = []
    for p in apps:
        name = p['metadata']['name']
        logs = kube('logs', name, '--timestamps')
        (logdir / (name + '.log')).write_text(logs)
        lines = [line for line in logs.splitlines() if 'Successfully discovered full config' in line]
        assert len(lines) == 1, (name, lines)
        fields = {key: int(value) for key, value in re.findall(r'\b(checkpoint_distance|cpd_min|cpd_max)=(\d+)', lines[0])}
        low = phase['config_map'].get('cpd_min', 0)
        high = phase['config_map'].get('cpd_max', 0)
        assert fields == {'checkpoint_distance': 0 if high else phase['config_map']['cpd'],
                          'cpd_min': low, 'cpd_max': high}, (name, fields)
        assert 'Continuing with empty config map' not in logs, name
        discovered.append({'pod': name, 'node': p['spec']['nodeName'], 'sdk_config': fields, 'log': lines[0]})
    save(directory / 'sdk-config-discovery.json', discovered)
    expected = dict(APPS, **{DS: collector})
    health(directory, 'before', expected, digests)
    report('All 14 SDKs fetched the expected configuration; 35/35 pods Ready, zero restarts')
    return expected


def mongo_posts():
    pod = next(p['metadata']['name'] for p in pods()['items'] if p['metadata']['name'].startswith('post-db-'))
    js = 'var p=db.getSiblingDB("post").getCollection("post"); print(JSON.stringify(p.find({},{_id:1,postid:1}).toArray().map(function(d){return {id:d._id.toString(),post_id:d.postid.toString()};})));'
    return json.loads(kube('exec', pod, '--', 'mongo', '--quiet', '--eval', js))


def workload(directory, name, count, rate, seed):
    assert count % rate == 0
    run = {'command': ['/usr/local/bin/wrk', '-t', '1', '-c', '1', '-d', f'{count // rate}s',
                      '-R', str(rate), '-s', str(LUA), SPEC['api_url']],
           'random_seed': seed, 'max_user_index': 962, 'target_requests': count,
           'rate_rps': rate, 'started_at': stamp(), 'started_us': time.time_ns() // 1000}
    env = dict(os.environ, RANDOM_SEED=str(seed), max_user_index='962')
    with (directory / f'{name}-wrk.log').open('w') as output:
        result = subprocess.run(run['command'], env=env, stdout=output, stderr=subprocess.STDOUT,
                                timeout=count // rate + 30)
    run.update(ended_at=stamp(), ended_us=time.time_ns() // 1000, exit_code=result.returncode)
    logs = (directory / f'{name}-wrk.log').read_text()
    match = re.search(r'\b(\d+) requests in', logs)
    run['completed_requests'] = int(match.group(1)) if match else None
    run['http_errors'] = 'ERROR: Status' in logs or 'Non-2xx or 3xx responses' in logs
    run['socket_errors'] = 'Socket errors:' in logs
    save(directory / f'{name}-run.json', run)
    # wrk stops on wall-clock duration; its final request can complete on the
    # server after client statistics have closed. Preserve and report both
    # counts, and verify server completion independently through Mongo/traces.
    assert result.returncode == 0 and count - 1 <= run['completed_requests'] <= count and not run['http_errors'] and not run['socket_errors'], run
    report(f'{name}: {run["completed_requests"]} Lua POST responses acknowledged, no HTTP or socket errors')
    return run


def fetch_traces(directory, run):
    jaeger = next(p['status']['podIP'] for p in pods()['items'] if p['metadata']['name'].startswith('jaeger-'))
    url = f'http://{jaeger}:16686/api/traces?' + urllib.parse.urlencode({
        'service': 'unknown_service:wrk2api_service_cgpb_es_sn20260910_proc',
        'operation': 'Wrk2APIServiceServer_ComposePost', 'limit': 500,
        'start': run['started_us'], 'end': run['ended_us']})
    time.sleep(10)
    previous = None
    stable = 0

    def settled():
        nonlocal previous, stable
        response = get(url)
        traces = response.get('data', [])
        save(directory / 'traces.json', traces)
        signature = sorted((t['traceID'], len(t['spans'])) for t in traces)
        if len(traces) == run['server_completed_requests'] and signature == previous:
            stable += 1
        else:
            stable = 0
        previous = signature
        return traces if stable >= 2 else None

    traces = wait_for('all measured ComposePost traces present and settled', settled, timeout=180)
    assert len({t['traceID'] for t in traces}) == run['server_completed_requests']
    return traces


def collect_logs(directory, since):
    logdir = directory / 'measurement-logs'
    logdir.mkdir()
    items = [p for p in pods()['items'] if label(p) in APPS or label(p) == DS]

    def collect(p):
        name = p['metadata']['name']
        logs = kube('logs', name, '--timestamps', '--since-time=' + since)
        (logdir / (name + '.log')).write_text(logs)
        errors = [line for line in logs.splitlines() if re.search(r'(?i)\b(ERROR|FATAL|PANIC)\b|exporting failed|dropping data', line)]
        metrics = [line for line in logs.splitlines() if 'cgpb_processor_metrics ' in line]
        counters = {key: int(value) for key, value in re.findall(r'(\w+)=(\d+)', metrics[-1])} if metrics else {}
        return name, {'errors': errors, 'sdk': counters}

    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        logs = dict(pool.map(collect, items))
    save(directory / 'log-check.json', logs)
    errors = sum(len(value['errors']) for value in logs.values())
    dropped = sum(value['sdk'].get('spans_dropped', 0) for value in logs.values())
    assert not errors and not dropped, f'{errors} log errors, {dropped} SDK dropped spans'
    report('SDK and collector measurement logs: no errors or dropped spans')


def finish_measurement(phase, directory, expected, digests, before, run):
    after = mongo_posts()
    save(directory / 'posts-after.json', after)
    before_ids = {p['id'] for p in before}
    new_posts = [p for p in after if p['id'] not in before_ids]
    save(directory / 'new-posts.json', new_posts)
    assert run['completed_requests'] <= len(new_posts) <= run['target_requests'], f'{len(new_posts)} new stored posts'
    assert len({p['post_id'] for p in new_posts}) == len(new_posts), 'Duplicate generated post IDs'
    run['server_completed_requests'] = len(new_posts)
    run['completed_after_client_cutoff'] = len(new_posts) - run['completed_requests']
    save(directory / 'measured-run.json', run)
    traces = fetch_traces(directory, run)
    collect_logs(directory, run['started_at'])
    snapshot = health(directory, 'after', expected, digests)
    result = {'phase': phase, 'completed_at': stamp(), 'requests': len(new_posts),
              'client_acknowledged_responses': run['completed_requests'],
              'completed_after_client_cutoff': run['completed_after_client_cutoff'],
              'distinct_stored_posts': len(new_posts), 'traces': len(traces),
              'total_spans': sum(len(t['spans']) for t in traces),
              'ready_pods': len(snapshot['items']), 'restarts': 0}
    save(directory / 'result.json', result)
    report(json.dumps(result))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=[p['name'] for p in SPEC['phases']], required=True)
    args = parser.parse_args()
    phase = next(p for p in SPEC['phases'] if p['name'] == args.phase)
    directory = ROOT / phase['name']
    directory.mkdir()  # Refuse accidental reruns of writes.
    save(directory / 'phase.json', dict(phase, started_at=stamp()))
    digests = preflight()
    pause_monitor()
    expected = configure(phase, directory, digests)
    workload(directory, 'warmup', SPEC['warmup_requests'], 1, 999)
    time.sleep(5)
    before = mongo_posts()
    save(directory / 'posts-before.json', before)
    run = workload(directory, 'measured', SPEC['requests_per_phase'], SPEC['rate_rps'], SPEC['lua_seed'])
    finish_measurement(phase, directory, expected, digests, before, run)


if __name__ == '__main__':
    main()
