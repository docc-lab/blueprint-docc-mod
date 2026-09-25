#!/usr/bin/env python3
"""Restore the existing random 2-6 deployment and its read-only observer."""
import json
import subprocess
import sys

import run_phase as run


def main():
    assert (run.ROOT / 'fixed6/result.json').exists(), 'Finish the fixed-6 measurement first'
    directory = run.ROOT / 'restored-random2_6'
    directory.mkdir()
    phase = run.SPEC['restore_config']
    digests = run.preflight()
    expected = run.configure(phase, directory, digests)
    snapshot = run.health(directory, 'after', expected, digests)
    run.save(directory / 'result.json', {
        'restored_at': run.stamp(), 'config': phase['config_map'],
        'ready_pods': len(snapshot['items']), 'restarts': 0,
        'backend_pods_unchanged': True,
    })
    subprocess.run([sys.executable, str(run.OLD / 'restart_monitor.py')], check=True)
    run.save(directory / 'monitor-start.json',
             json.loads((run.OLD / 'monitor-start.json').read_text()))
    run.report('Random 2-6 restored and background monitoring resumed')


if __name__ == '__main__':
    main()
