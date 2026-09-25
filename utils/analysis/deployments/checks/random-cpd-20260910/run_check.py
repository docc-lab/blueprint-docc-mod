"""Exercise the built collector's config pipeline with the actual Blueprint SDK."""
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.request

import yaml

ROOT = Path(__file__).resolve().parent
BINARY = '/tmp/otelcol-random-cpd-20260910'
HOST = '127.0.0.2'
with socket.socket() as sock:
    sock.bind((HOST, 0))
    port = sock.getsockname()[1]
config = {
    'receivers': {
        'configdiscovery': {'endpoint': f'{HOST}:{port}', 'config_map': {'cpd_min': 2, 'cpd_max': 6}},
        'otlp': {'protocols': {'grpc': {'endpoint': f'{HOST}:4317'}}},
    },
    'exporters': {'debug': {}},
    'service': {
        'telemetry': {'logs': {'level': 'error'}, 'metrics': {'level': 'none'}},
        'pipelines': {
            'logs/config': {'receivers': ['configdiscovery'], 'exporters': ['debug']},
            'traces': {'receivers': ['otlp'], 'exporters': ['debug']},
        },
    },
}
path = ROOT / 'collector.yaml'
path.write_text(yaml.safe_dump(config))
result = subprocess.run([BINARY, 'validate', '--config', str(path)], capture_output=True, text=True, timeout=30)
(ROOT / 'validate.log').write_text(result.stdout + result.stderr)
assert result.returncode == 0, result.stderr
config['receivers']['configdiscovery']['config_map']['cpd_max'] = 257
invalid = ROOT / 'invalid-collector.yaml'
invalid.write_text(yaml.safe_dump(config))
result = subprocess.run([BINARY, 'validate', '--config', str(invalid)], capture_output=True, text=True, timeout=30)
(ROOT / 'invalid-validate.log').write_text(result.stdout + result.stderr)
assert result.returncode != 0 and 'cpd_max <= 256' in result.stderr, result.stderr
print('Collector binary accepts 2..6 and rejects 2..257', flush=True)
with (ROOT / 'collector.log').open('w') as log:
    process = subprocess.Popen([BINARY, '--config', str(path)], stdout=log, stderr=subprocess.STDOUT)
try:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 20
    while True:
        try:
            with opener.open(f'http://{HOST}:{port}/getFullConfig', timeout=1) as response:
                body = json.load(response)
            break
        except OSError:
            if process.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError((ROOT / 'collector.log').read_text())
            time.sleep(.1)
    assert body['config'] == {'cpd_min': 2, 'cpd_max': 6}, body
    (ROOT / 'discovered-config.json').write_text(json.dumps(body, indent=2) + '\n')
    result = subprocess.run(['go', 'run', str(ROOT / 'sdk_probe.go'), HOST, str(port)],
        cwd='/users/tomislav/blueprint-docc-mod', capture_output=True, text=True, timeout=90,
        env={**os.environ, 'REVERSE_TRUSS': 'off', 'OTEL_SAMPLE_RATIO': '1', 'OTLP_RETRY': 'off'})
    (ROOT / 'sdk.log').write_text(result.stderr)
    (ROOT / 'sdk-results.jsonl').write_text(result.stdout)
    assert result.returncode == 0, result.stderr[-5000:]
    rows = [json.loads(line) for line in result.stdout.splitlines()]
    assert len(rows) == 3 and all(row['passed'] for row in rows), rows
    print(json.dumps(rows, indent=2), flush=True)
finally:
    process.terminate()
    process.wait(timeout=15)
