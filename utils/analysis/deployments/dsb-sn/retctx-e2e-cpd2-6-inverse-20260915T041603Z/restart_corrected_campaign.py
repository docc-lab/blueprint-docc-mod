"""Tomislav-RetCtx: preserve the FD-limited attempt, then restart with fresh state."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
sys.path.insert(0, str(REPO / 'utils'))
from prepare_dsb_sn_e2e import write_json


def main():
    while True:
        capture = json.loads((ROOT / 'interrupted-capture-status.json').read_text())
        if capture['state'] == 'complete':
            break
        assert capture['state'] != 'failed', capture
        time.sleep(5)
    for name in ('run.pid', 'monitor.pid', 'handoff.pid'):
        pid = int((ROOT / name).read_text())
        command = Path(f'/proc/{pid}/cmdline')
        assert not command.exists() or not command.read_bytes(), (name, pid)
    run = ROOT / 'run/01-v'
    if run.exists():
        archive = run.with_name('01-v-interrupted-fdlimit-' + str(time.time_ns()))
        run.rename(archive)
    else:
        previous = list((ROOT / 'run').glob('01-v-interrupted-fdlimit-*'))
        assert len(previous) == 1, previous
        archive = previous[0]
    for name in ('analysis', 'progress-events.jsonl'):
        if (ROOT / name).exists():
            assert not (archive / name).exists()
            shutil.move(str(ROOT / name), str(archive / name))
    for name in ('run-status.json', 'monitor-status.json', 'progress.json', 'STATUS.md'):
        if (ROOT / name).exists():
            (archive / name).write_bytes((ROOT / name).read_bytes())
    plan = json.loads((ROOT / 'plan.json').read_text())
    plan.update(generator_min_file_descriptors=16384,
                post_ramp_trace_capture={'minimum_quiet_seconds': 30, 'maximum_drain_seconds': 120,
                                         'recheck_id_batch': 50, 'repeat_empty_search_after_drain': True})
    write_json(ROOT / 'plan.json', plan)
    files = [*REPO.glob('utils/*dsb_sn_e2e.py'), REPO / 'docs/dev/dsb_sn_retctx_evaluation.md', ROOT / 'plan.json']
    record = {'started': datetime.now(timezone.utc).isoformat(), 'interrupted_attempt': str(archive),
              'reason': 'wrk initialization exceeded the inherited 1024-FD limit; primary ramps restart fresh',
              'files': {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}}
    write_json(ROOT / 'run-provenance-corrected.json', record)
    with (ROOT / 'logs/run-corrected.log').open('w') as log:
        runner = subprocess.Popen([sys.executable, '-B', '-u', str(REPO / 'utils/run_dsb_sn_e2e.py'),
                                   'run', '--out', str(ROOT)], cwd=REPO, stdout=log,
                                  stderr=subprocess.STDOUT, start_new_session=True)
    (ROOT / 'run.pid').write_text(str(runner.pid) + '\n')
    # Wait for the new runner to replace the old terminal status.
    for _ in range(100):
        if json.loads((ROOT / 'run-status.json').read_text())['state'] == 'running':
            break
        time.sleep(.1)
    assert json.loads((ROOT / 'run-status.json').read_text())['state'] == 'running'
    with (ROOT / 'logs/monitor-corrected.log').open('w') as log:
        monitor = subprocess.Popen([sys.executable, '-B', '-u', str(ROOT / 'monitor_e2e.py')],
                                   stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    (ROOT / 'monitor.pid').write_text(str(monitor.pid) + '\n')
    write_json(ROOT / 'restart-status.json', {'state': 'complete', 'runner_pid': runner.pid,
                                             'monitor_pid': monitor.pid, **record})


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        write_json(ROOT / 'restart-status.json', {'state': 'failed', 'error': str(error)})
        raise
