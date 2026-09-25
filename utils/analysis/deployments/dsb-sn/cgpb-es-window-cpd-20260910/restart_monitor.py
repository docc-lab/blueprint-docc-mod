#!/usr/bin/env python3
"""Resume the existing read-only observer with the final deployment baseline."""
import json
import os
from pathlib import Path
import subprocess
import sys

import run_phase as run

root = run.ROOT
phase = run.SPEC['phases'][-1]
assert (root / phase['name'] / 'result.json').exists()
expected = dict(run.APPS, **{run.DS: run.COLLECTORS['phases'][phase['name']]})
digests = json.loads((root / 'image-digests.json').read_text())
snapshot = run.health(root, 'final', expected, digests)
run.save(root / 'verification-pods.json', snapshot)
run.save(root / 'verification.json', {'frontend_url': run.SPEC['api_url'],
    'checkpoint_config': phase['config_map'], 'bridge': 'cgpb', 'namespace': run.NS})
source = (run.OLD / 'monitor.py').read_text()
# The previous experiment already uses a script-relative artifact root.
(root / 'monitor.py').write_text(source)
directory = root / 'monitoring'
directory.mkdir(exist_ok=True)
pid_file = directory / 'monitor.pid'
if pid_file.exists():
    old_pid = int(pid_file.read_text())
    if Path(f'/proc/{old_pid}').exists():
        raise RuntimeError(f'Monitor PID {old_pid} already exists')
with (directory / 'monitor.log').open('a') as log:
    process = subprocess.Popen([sys.executable, '-u', str(root / 'monitor.py')],
        stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
        start_new_session=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
run.save(root / 'monitor-start.json', {'started_at': run.stamp(), 'pid': process.pid})
print('Monitoring resumed; PID', process.pid)
