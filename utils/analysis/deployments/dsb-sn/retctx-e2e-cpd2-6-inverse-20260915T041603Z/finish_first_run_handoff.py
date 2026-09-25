"""Tomislav-RetCtx: finish the first ramp's post-load capture with delayed rechecks.

Only pauses the owning Python runner after its final timed point and snapshots
are saved. Application pods and the completed request measurements are preserved.
"""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
sys.path.insert(0, str(REPO / 'utils'))
from run_dsb_sn_e2e import now, kube, capture_traces, settle_trace_samples, snapshot
from prepare_dsb_sn_e2e import write_json


def main():
    pid = int((ROOT / 'run.pid').read_text())
    run = ROOT / 'run/01-v'
    target = run / 'rate-05000/result.json'
    write_json(ROOT / 'handoff-status.json', {'state': 'waiting-for-final-point', 'pid': pid})
    while True:
        command = Path(f'/proc/{pid}/cmdline')
        assert command.exists() and b'run_dsb_sn_e2e.py' in command.read_bytes()
        if target.exists() and 'kind' in json.loads(target.read_text()):
            break
        time.sleep(.05)
    os.kill(pid, signal.SIGSTOP)
    write_json(ROOT / 'handoff-status.json', {'state': 'capturing-after-load', 'paused_pid': pid, 'time': now()})
    case = json.loads((ROOT / 'cases.json').read_text())[0]
    pods = json.loads(kube(case['namespace'], 'get', 'pods', '-l', f'retctx-e2e={case["variant"]}', '-o', 'json'))['items']
    assert len(pods) == 33 and not any(p['metadata'].get('deletionTimestamp') for p in pods)
    results = [json.loads(p.read_text()) for p in run.glob('rate-*/result.json')]
    assert len(results) == 16 and all('kind' in r for r in results)
    write_json(ROOT / 'run-status.json', {'state': 'running', 'mode': 'run', 'repetition': 1,
                                        'kind': 'v', 'stage': 'settle-trace-samples', 'updated': now()})
    settle_trace_samples(case, run)
    if (run / 'final').exists():
        (run / 'final').rename(run / ('final-before-handoff-' + str(time.time_ns())))
    snapshot(case['namespace'], case['variant'], run / 'final')
    capture_traces(case, run)
    write_json(run / 'complete.json', {'finished': now(), 'points': 16, 'seed': 1001,
                                      'capture_handoff': str(Path(__file__).resolve())})
    # The old runner is stopped with no timed workload remaining. Replace only
    # that process; resume skips this complete ramp and begins fresh PB state.
    os.kill(pid, signal.SIGKILL)
    with (ROOT / 'logs/run.log').open('a') as log:
        process = subprocess.Popen([sys.executable, '-B', '-u', str(REPO / 'utils/run_dsb_sn_e2e.py'),
                                    'run', '--out', str(ROOT)], cwd=REPO, stdout=log,
                                   stderr=subprocess.STDOUT, start_new_session=True)
    (ROOT / 'run.pid').write_text(str(process.pid) + '\n')
    write_json(ROOT / 'handoff-status.json', {'state': 'complete', 'previous_pid': pid,
                                            'runner_pid': process.pid, 'finished': now(),
                                            'runner_sha256': hashlib.sha256((REPO / 'utils/run_dsb_sn_e2e.py').read_bytes()).hexdigest()})


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        write_json(ROOT / 'handoff-status.json', {'state': 'failed', 'time': now(), 'error': str(error)})
        raise
