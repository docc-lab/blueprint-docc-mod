#!/usr/bin/env python3
"""Tomislav-RetCtx: verify sampled OTLP values through a temporary local collector."""
import base64
import collections
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

REPO = Path('/users/tomislav/blueprint-docc-mod')
BIN = REPO / 'utils/spanload/bin/spanload'
COLLECTOR = Path('/users/tomislav/opentelemetry-collector-contrib/bin/otelcontribcol_linux_amd64')
IMAGE = 'spanload:distributions-20260913'
ART = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(tempfile.mkdtemp(prefix='spanload-distributions-20260913-', dir='/users/tomislav/deployments/collector-load'))
print(ART, flush=True)
if Path(__file__).resolve() != (ART / 'verify.py').resolve():
    shutil.copy2(__file__, ART / 'verify.py')

def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')

def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            value.update(chunk)
    return value.hexdigest()

def scrape(url):
    with urllib.request.urlopen(url, timeout=2) as response:
        return response.read().decode()

def metric(text, name):
    return sum(float(m.group(1)) for m in re.finditer(
        rf'^{name}(?:_total)?(?:\{{[^\n]*\}})?\s+([0-9.eE+-]+)', text, re.M))

MASK = (1 << 64) - 1
GAMMA = 0x9e3779b97f4a7c15

def mix(x):
    x = ((x ^ (x >> 30)) * 0xbf58476d1ce4e5b9) & MASK
    x = ((x ^ (x >> 27)) * 0x94d049bb133111eb) & MASK
    return x ^ (x >> 31)

def expected_size(spec, ordinal):
    x = mix((42 + (ordinal + 1) * GAMMA) & MASK)
    if spec['type'] == 'uniform':
        width = spec['max'] - spec['min'] + 1
        while x < ((-width) & MASK) % width:
            x = mix((x + GAMMA) & MASK)
        return spec['min'] + x % width
    if spec['type'] == 'empirical':
        weights = collections.Counter(spec['samples'])
    else:
        scale = max(v['weight'] for v in spec['values'])
        weights = collections.defaultdict(float)
        for item in spec['values']:
            if item['weight']:
                weights[item['bytes']] += item['weight'] / scale
    total = sum(weights[n] for n in sorted(weights))
    cumulative = 0
    u = (x >> 11) / (1 << 53)
    for size in sorted(weights):
        cumulative += weights[size]
        if cumulative / total > u:
            return size
    raise AssertionError('invalid CDF')

specs = {
    'discrete': json.loads((REPO / 'utils/spanload/profiles/payload-distribution-example.json').read_text()),
    'empirical': {'source': 'Boundary/sampling fixture, not workload measurements', 'type': 'empirical', 'samples': [0, 0, 16, 16, 32, 128]},
    'uniform': {'source': 'Inclusive bounds and grouped-histogram fixture', 'type': 'uniform', 'min': 0, 'max': 128},
}
for name, spec in specs.items():
    write_json(ART / f'{name}.json', spec)

def generate():
    # Reserve distinct loopback ports together; release immediately before launch.
    reservations = []
    for _ in range(3):
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        reservations.append(listener)
    grpc_port, http_port, metrics_port = [s.getsockname()[1] for s in reservations]
    config = (REPO / 'utils/spanload/collector.yaml').read_text()
    config = config.replace('14317', str(grpc_port)).replace('14318', str(http_port)).replace('18888', str(metrics_port))
    config = config.replace('path: /dev/null', f'path: {ART / "exported.jsonl"}')
    (ART / 'collector.yaml').write_text(config)
    for listener in reservations:
        listener.close()
    metrics_url = f'http://127.0.0.1:{metrics_port}/metrics'
    log = (ART / 'collector.log').open('w')
    process = subprocess.Popen([str(COLLECTOR), '--config', str(ART / 'collector.yaml')],
                               stdout=log, stderr=subprocess.STDOUT, env={**os.environ, 'GOMAXPROCS': '1'})
    runs = []
    try:
        deadline = time.monotonic() + 20
        while True:
            assert process.poll() is None, 'collector startup failed; see collector.log'
            try:
                scrape(metrics_url)
                break
            except OSError:
                assert time.monotonic() < deadline, 'collector startup timed out'
                time.sleep(.05)
        for kind, bridge in [('discrete', 'pb'), ('empirical', 'cgpb'), ('uniform', 'sb')]:
            for transport in ['grpc', 'http']:
                name = f'{kind}-{transport}'
                run_dir = ART / name
                args = [str(BIN), '--bridge', bridge, '--cpd', '3', '--bridge-distribution', str(ART / f'{kind}.json'),
                        '--payload-seed', '42', '--rates', '0', '--spans', '600', '--duration', '5s',
                        '--workers', '1' if transport == 'grpc' else '4', '--batch-size', '31' if transport == 'grpc' else '47',
                        '--report-interval', '0', '--metrics-url', metrics_url, '--settle', '300ms', '--out', str(run_dir)]
                if transport == 'grpc':
                    args += ['--endpoint', f'127.0.0.1:{grpc_port}', '--insecure']
                else:
                    args += ['--protocol', 'http', '--endpoint', f'http://127.0.0.1:{http_port}/v1/traces', '--profile', 'semconv10-example']
                with (ART / f'{name}.stdout.jsonl').open('w') as stdout, (ART / f'{name}.stderr').open('w') as stderr:
                    subprocess.run(args, stdout=stdout, stderr=stderr, timeout=20, check=True)
                manifest = json.loads((run_dir / 'manifest.json').read_text())
                records = [json.loads(line) for line in (run_dir / 'results.jsonl').read_text().splitlines()]
                phase, = [r for r in records if r['type'] == 'phase']
                assert phase['acknowledged_spans'] == 600 and phase['attempted_checkpoint_spans'] == 200
                assert not records[-1]['problems']
                runs.append({'name': name, 'run_id': manifest['run_id'], 'bridge': bridge, 'distribution': kind,
                             'profile': manifest['config']['profile'], 'phase': phase})
        final_metrics = scrape(metrics_url)
        (ART / 'final-metrics.prom').write_text(final_metrics)
    finally:
        if process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        log.close()

    return runs, final_metrics

if len(sys.argv) > 1:
    # Revalidate saved wire/metrics artifacts without sending any new spans.
    runs = []
    for kind, bridge in [('discrete', 'pb'), ('empirical', 'cgpb'), ('uniform', 'sb')]:
        for transport in ['grpc', 'http']:
            name = f'{kind}-{transport}'
            manifest = json.loads((ART / name / 'manifest.json').read_text())
            records = [json.loads(line) for line in (ART / name / 'results.jsonl').read_text().splitlines()]
            phase, = [r for r in records if r['type'] == 'phase']
            assert phase['acknowledged_spans'] == 600 and phase['attempted_checkpoint_spans'] == 200
            assert not records[-1]['problems']
            runs.append({'name': name, 'run_id': manifest['run_id'], 'bridge': bridge, 'distribution': kind,
                         'profile': manifest['config']['profile'], 'phase': phase})
    final_metrics = (ART / 'final-metrics.prom').read_text()
else:
    runs, final_metrics = generate()

spans = collections.defaultdict(list)
for line in (ART / 'exported.jsonl').read_text().splitlines():
    for rs in json.loads(line)['resourceSpans']:
        for scope in rs['scopeSpans']:
            for span in scope['spans']:
                spans[span['traceId'][:16]].append(span)

results = []
sequences = {}
unique = set()
for run in runs:
    observed = spans[run['run_id']]
    assert len(observed) == 600
    hist = collections.Counter()
    sizes = {}
    for span in observed:
        identity = (span['traceId'], span['spanId'])
        assert identity not in unique
        unique.add(identity)
        position = int(span['spanId'], 16) - 1
        attrs = span['attributes']
        assert len(attrs) == (1 if run['profile'] == 'zero' else 11)
        bridge_attr, = [a for a in attrs if a['key'] in ('_br', '_d', '_o')]
        assert set(bridge_attr['value']) == {'bytesValue'}
        raw = base64.b64decode(bridge_attr['value']['bytesValue'], validate=True)
        if position % 3 == 0:
            assert bridge_attr['key'] == '_br'
            assert len(raw) == expected_size(specs[run['distribution']], position // 3)
            hist[len(raw)] += 1
            sizes[position] = len(raw)
        else:
            expected_key, expected_bytes = ('_o', b'\x01\x06') if run['bridge'] == 'sb' else ('_d', b'\x06')
            assert bridge_attr['key'] == expected_key and raw == expected_bytes
    payload = run['phase']['attempted_checkpoint_payload']
    total_bytes = sum(n * count for n, count in hist.items())
    assert payload['count'] == 200 and payload['total_bytes'] == total_bytes
    assert payload['min_bytes'] == min(hist) and payload['max_bytes'] == max(hist)
    assert payload['mean_bytes'] == total_bytes / 200
    assert len(payload['histogram']) <= 128
    for bucket in payload['histogram']:
        assert bucket['count'] == sum(count for size, count in hist.items() if bucket['min_bytes'] <= size <= bucket['max_bytes'])
    assert len(hist) > 1
    kind = run['distribution']
    if kind in sequences:
        assert sequences[kind] == sizes, 'transport/workers/batches changed samples'
    sequences[kind] = sizes
    results.append({key: run[key] for key in ('name', 'run_id', 'bridge', 'profile')} |
                   {'spans': 600, 'checkpoints': 200, 'payload_bytes': total_bytes, 'mean_bytes': payload['mean_bytes'],
                    'min_bytes': min(hist), 'max_bytes': max(hist), 'distinct_sizes': len(hist), 'histogram': dict(sorted(hist.items()))})
assert len(unique) == 3600
counters = {name: metric(final_metrics, name) for name in ('otelcol_receiver_accepted_spans', 'otelcol_exporter_sent_spans')}
assert all(value == 3600 for value in counters.values()), counters

# Scratch-container smoke: mount an input JSON as its unprivileged user.
# The temp parent is private, so grant traversal only for this test mount directory.
os.chmod(ART, 0o755)
container = subprocess.run(['docker', 'run', '--rm', '-v', f'{ART}/discrete.json:/distribution.json:ro', IMAGE,
                            '--dry-run', '--bridge', 'sb', '--bridge-distribution', '/distribution.json'],
                           capture_output=True, text=True, timeout=30, check=True)
(ART / 'docker-dry-run.jsonl').write_text(container.stdout)
(ART / 'docker-dry-run.stderr').write_text(container.stderr)
docker_records = [json.loads(line) for line in container.stdout.splitlines()]
assert abs(docker_records[0]['resolved_profile']['checkpoint_distribution']['expected_mean_bytes'] - 24.8) < 1e-12
image_details = json.loads(subprocess.check_output(['docker', 'image', 'inspect', IMAGE], text=True))[0]
write_json(ART / 'image.json', image_details)
for path, name in [(BIN, 'binary.sha256'), (COLLECTOR, 'collector-binary.sha256')]:
    (ART / name).write_text(f'{digest(path)}  {path}\n')
summary = {'status': 'passed', 'spans_exported': len(unique), 'checkpoint_spans': 1200,
           'collector_counters': counters, 'runs': results, 'same_seed_sizes_match_across_transport_workers_and_batches': True,
           'image': IMAGE, 'image_id': image_details['Id'], 'docker_dry_run': 'passed',
           'note': 'Local correctness check with serialized JSON output; not a throughput benchmark. Temporary collector stopped.'}
write_json(ART / 'SUMMARY.json', summary)
print(json.dumps({k: v for k, v in summary.items() if k != 'runs'}, indent=2), flush=True)
