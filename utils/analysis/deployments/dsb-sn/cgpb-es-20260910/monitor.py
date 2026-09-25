#!/usr/bin/env python3
"""Observe the social-network deployment; issue two timeline reads every 30 seconds."""
import concurrent.futures
import datetime as dt
import fcntl
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import threading
import time
import urllib.parse
import urllib.request

ROOT = Path('/users/tomislav/deployments/dsb-sn/cgpb-es-20260910')
OUT = ROOT / 'monitoring'
NS = 'dsb-sn'
BASE = json.loads((ROOT / 'verification.json').read_text())['frontend_url']
INTERVAL = 30
STOP = threading.Event()
ERROR = re.compile(r'(?i)\b(?:ERROR|FATAL|PANIC)\b|exporting failed|dropping data')


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z')


def command(*args):
    return subprocess.check_output(
        ['kubectl', '--request-timeout=7s', *args], text=True,
        stderr=subprocess.PIPE, timeout=10)


def kube(*args):
    return json.loads(command(*args) if '--raw' in args else command(*args, '-o', 'json'))


def get(url):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=10) as response:
        return json.load(response)


def probe(method, args):
    start = time.monotonic()
    result = get(BASE + '/' + method + '?' + urllib.parse.urlencode(args))
    if result.get('Ret1') or not result.get('Ret0'):
        raise RuntimeError('API returned an error or empty result')
    return {'ok': True, 'latency_ms': round((time.monotonic() - start) * 1000, 2)}


def logs(name, since):
    lines = command('-n', NS, 'logs', name, '--timestamps',
                    '--since-time=' + since, '--tail=2000').splitlines()
    errors = [line[:1500] for line in lines if ERROR.search(line)]
    metrics = [line for line in lines if 'cgpb_processor_metrics ' in line]
    counters = dict((key, int(value)) for key, value in
                    re.findall(r'(\w+)=(\d+)', metrics[-1])) if metrics else {}
    return {'errors': errors[-10:], 'error_count': len(errors), 'sdk': counters,
            'possibly_truncated': len(lines) >= 2000}


def save(record):
    encoded = json.dumps(record)
    with (OUT / 'samples.jsonl').open('a') as stream:
        stream.write(encoded + '\n')
    if record['alerts']:
        with (OUT / 'alerts.jsonl').open('a') as stream:
            stream.write(encoded + '\n')
    temporary = OUT / 'latest.tmp'
    temporary.write_text(json.dumps(record, indent=2) + '\n')
    temporary.replace(OUT / 'latest.json')
    print(json.dumps({key: record.get(key) for key in
                     ('timestamp', 'cycle', 'ready_pods', 'total_pods',
                      'total_restarts', 'api', 'trace_age_s', 'alerts')}), flush=True)


def main():
    OUT.mkdir(exist_ok=True)
    lock = (OUT / 'monitor.lock').open('w')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    (OUT / 'monitor.pid').write_text(str(os.getpid()) + '\n')
    (OUT / 'stopped-at.txt').unlink(missing_ok=True)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: STOP.set())
    baseline = json.loads((ROOT / 'verification-pods.json').read_text())['items']
    previous_restarts = {
        p['metadata']['uid']: sum(c['restartCount'] for c in
                                 p['status'].get('containerStatuses', []))
        for p in baseline}
    previous_uids = set(previous_restarts)
    previous_events = None
    previous_sdk = {}
    since = now()
    started = time.monotonic()
    cycle = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        while not STOP.is_set():
            tick = time.monotonic()
            cycle += 1
            stamp = now()
            record = {'timestamp': stamp, 'cycle': cycle, 'alerts': [], 'api': {}}
            alerts = record['alerts']
            try:
                futures = {
                    'pods': pool.submit(kube, '-n', NS, 'get', 'pods'),
                    'nodes': pool.submit(kube, 'get', 'nodes'),
                    'events': pool.submit(kube, '-n', NS, 'get', 'events',
                                          '--field-selector=type=Warning'),
                    'ReadUserTimeline': pool.submit(probe, 'ReadUserTimeline',
                        {'userId': 1, 'start': 0, 'stop': 10}),
                    'ReadHomeTimeline': pool.submit(probe, 'ReadHomeTimeline',
                        {'userId': 2, 'start': 0, 'stop': 10}),
                }
                results = {}
                for key, future in futures.items():
                    try:
                        results[key] = future.result()
                        if key in ('ReadUserTimeline', 'ReadHomeTimeline'):
                            record['api'][key] = results[key]
                    except Exception as error:
                        alerts.append(f'{key}: {error}')
                        if key in ('ReadUserTimeline', 'ReadHomeTimeline'):
                            record['api'][key] = {'ok': False, 'error': str(error)}
                pods = results.get('pods', {}).get('items', [])
                record['total_pods'] = len(pods)
                record['ready_pods'] = sum(
                    any(c['type'] == 'Ready' and c['status'] == 'True'
                        for c in p['status'].get('conditions', []))
                    and not p['metadata'].get('deletionTimestamp') for p in pods)
                if record['ready_pods'] != len(baseline) or len(pods) != len(baseline):
                    alerts.append(f"Ready pods: {record['ready_pods']}/{len(pods)}; expected {len(baseline)}")
                restarts = {}
                pod_summary = []
                for p in pods:
                    uid, name = p['metadata']['uid'], p['metadata']['name']
                    count = sum(c['restartCount'] for c in p['status'].get('containerStatuses', []))
                    restarts[uid] = count
                    if count > previous_restarts.get(uid, 0):
                        alerts.append(f'{name}: {count - previous_restarts.get(uid, 0)} new restart(s)')
                    pod_summary.append({'name': name, 'uid': uid,
                        'node': p['spec'].get('nodeName'), 'phase': p['status']['phase'],
                        'restarts': count})
                if pods:
                    if set(restarts) != previous_uids:
                        alerts.append('Pod membership changed; inspect pod snapshot')
                    previous_uids = set(restarts)
                    previous_restarts = restarts
                record['pods'] = pod_summary
                record['total_restarts'] = sum(restarts.values())
                for n in results.get('nodes', {}).get('items', []):
                    for condition in n['status']['conditions']:
                        unhealthy = ((condition['type'] == 'Ready' and condition['status'] != 'True')
                            or (condition['type'].endswith('Pressure') and condition['status'] != 'False'))
                        if unhealthy:
                            alerts.append(f"{n['metadata']['name']}: {condition['type']}={condition['status']}")
                if 'events' in results:
                    events = results['events']['items']
                    current = {e['metadata']['uid']: (e.get('count', 1), e.get('lastTimestamp'),
                        e.get('series', {}).get('count', 0)) for e in events}
                    record['new_warnings'] = []
                    for e in events:
                        if previous_events is not None and current[e['metadata']['uid']] != previous_events.get(e['metadata']['uid']):
                            message = f"{e['involvedObject']['name']}: {e['reason']}: {e['message']}"
                            record['new_warnings'].append(message)
                            alerts.append(message)
                    previous_events = current
                if pods:
                    jaeger = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('jaeger-'))
                    es = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('elasticsearch-'))
                    futures = {
                        'elasticsearch': pool.submit(get, f'http://{es}:9200/_cluster/health'),
                        'traces': pool.submit(get, f'http://{jaeger}:16686/api/traces?' + urllib.parse.urlencode({
                            'service': 'unknown_service:wrk2api_service_cgpb_es_sn20260910_proc',
                            'limit': 5, 'lookback': '1h'})),
                    }
                    if cycle % 2 == 1:
                        futures['resources'] = pool.submit(kube, 'get', '--raw',
                            '/apis/metrics.k8s.io/v1beta1/namespaces/' + NS + '/pods')
                        for p in pods:
                            name = p['metadata']['name']
                            futures['logs/' + name] = pool.submit(logs, name, since)
                        since = stamp
                    record['sdk'] = {}
                    record['log_errors'] = {}
                    for key, future in futures.items():
                        try:
                            value = future.result()
                            if key == 'elasticsearch':
                                record[key] = value
                                if value['status'] == 'red' or value.get('timed_out'):
                                    alerts.append('Elasticsearch unhealthy: ' + value['status'])
                            elif key == 'traces':
                                if value.get('errors'):
                                    alerts.append('Jaeger query error: ' + str(value['errors']))
                                traces = value.get('data') or []
                                newest = max((s['startTime'] for t in traces for s in t['spans']), default=0) / 1e6
                                record['trace_age_s'] = round(time.time() - newest, 1) if newest else None
                                record['recent_trace_ids'] = [t['traceID'] for t in traces]
                                if time.monotonic() - started > 180 and (not newest or record['trace_age_s'] > 180):
                                    alerts.append('No frontend trace newer than 180 seconds')
                            elif key == 'resources':
                                record[key] = [{'name': p['metadata']['name'],
                                    'containers': p['containers']} for p in value.get('items', [])]
                            else:
                                name = key.removeprefix('logs/')
                                if value['error_count']:
                                    record['log_errors'][name] = value
                                    alerts.append(f"{name}: {value['error_count']} error log line(s)")
                                if value['possibly_truncated']:
                                    alerts.append(f'{name}: log sampling reached 2000-line limit')
                                if value['sdk']:
                                    record['sdk'][name] = value['sdk']
                                    previous = previous_sdk.get(name, {})
                                    for counter, count in value['sdk'].items():
                                        if ('dropped' in counter or counter.startswith('send_')) and count > previous.get(counter, 0):
                                            alerts.append(f'{name}: {counter} increased to {count}')
                                    previous_sdk[name] = value['sdk']
                        except Exception as error:
                            alerts.append(f'{key}: {error}')
            except Exception as error:
                alerts.append(f'Monitor cycle failed: {type(error).__name__}: {error}')
            record['duration_s'] = round(time.monotonic() - tick, 2)
            save(record)
            STOP.wait(max(0, INTERVAL - (time.monotonic() - tick)))
    (OUT / 'stopped-at.txt').write_text(now() + '\n')


if __name__ == '__main__':
    main()
