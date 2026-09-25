#!/usr/bin/env python3
"""Tomislav-RetCtx: durable file-based progress for the zero-work ramp; no workload traffic."""
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
sys.path.insert(0, str(REPO / 'utils'))
from prepare_dsb_sn_e2e import write_json


def now():
    return datetime.now(timezone.utc).isoformat()


def main():
    plan = json.loads((ROOT / 'plan.json').read_text())
    total_runs = len(plan['cases']) * int(plan.get('repetitions', 1))
    total_points = total_runs * len(plan['ramp_rates'])
    seen = set()
    missing_process_ticks = 0
    while True:
        # Tomislav-RetCtx: the runner writes run-status.json only after its first deploy
        # starts, so a monitor launched alongside it must wait rather than die.
        try:
            status = json.loads((ROOT / 'run-status.json').read_text())
        except (FileNotFoundError, json.JSONDecodeError):
            write_json(ROOT / 'monitor-status.json', {'state': 'waiting-for-runner', 'updated': now()})
            time.sleep(5)
            continue
        rows = []
        for path in sorted((ROOT / 'run').glob('*/rate-*/result.json')):
            if '-interrupted-' in path.parent.parent.name:
                continue
            row = json.loads(path.read_text())
            if 'kind' not in row:
                continue
            rows.append(row)
            if str(path) not in seen:
                event = {key: row[key] for key in ('case', 'offered_rps', 'completed_rps', 'mean_ms', 'p99_ms',
                                                  'non_2xx_3xx', 'socket_errors', 'finished')}
                event['path'] = str(path)
                with (ROOT / 'progress-events.jsonl').open('a') as stream:
                    stream.write(json.dumps(event) + '\n')
                seen.add(str(path))
        complete_runs = [p for p in (ROOT / 'run').glob('*/complete.json') if '-interrupted-' not in p.parent.name]
        latest = max(rows, key=lambda r: r['finished']) if rows else None
        progress = {'updated': now(), 'runner': status, 'completed_points': len(rows), 'total_points': total_points,
                    'completed_runs': len(complete_runs), 'total_runs': total_runs,
                    'completed_requests': sum(r['completed_requests'] for r in rows),
                    'non_2xx_3xx': sum(r['non_2xx_3xx'] for r in rows), 'latest_point': latest}
        write_json(ROOT / 'progress.json', progress)
        lines = ['# Tomislav-RetCtx: zero-work (no-work) ramp', '', f"Updated: {progress['updated']}", '',
                 f"State: {status['state']}; {len(rows)}/{total_points} points, {len(complete_runs)}/{total_runs} ramps complete.",
                 f"Current case/stage: {status.get('case', '-')}/{status.get('stage', '-')} offered {status.get('offered_rps', '-')}", '',
                 'snnw zero-work services, passthrough collectors 1 CPU/4GiB, CPD 2-6 inverse_depth, leaf rejection; 500-14000 step 500, 30 s/point, N=1.', '']
        if latest:
            lines += [f"Latest: {latest['case']} offered {latest['offered_rps']}/s, completed {latest['completed_rps']:.1f}/s, "
                      f"mean {latest['mean_ms']:.2f} ms, p99 {latest['p99_ms']:.2f} ms, HTTP errors {latest['non_2xx_3xx']}.", '']
        (ROOT / 'STATUS.md').write_text('\n'.join(lines))
        if status['state'] == 'complete':
            with (ROOT / 'logs/final-analysis.log').open('w') as log:
                subprocess.run([sys.executable, '-B', str(REPO / 'utils/analyze_dsb_sn_nw.py'), '--out', str(ROOT)],
                               check=True, stdout=log, stderr=subprocess.STDOUT, timeout=900)
            write_json(ROOT / 'monitor-status.json', {'state': 'complete', 'updated': now()})
            return
        if status['state'] == 'failed':
            write_json(ROOT / 'monitor-status.json', {'state': 'runner-failed', 'updated': now(), 'runner': status})
            return
        pid = int((ROOT / 'run.pid').read_text())
        command = Path(f'/proc/{pid}/cmdline')
        alive = command.exists() and b'run_dsb_sn_nw.py' in command.read_bytes()
        missing_process_ticks = 0 if alive else missing_process_ticks + 1
        if missing_process_ticks >= 2:
            raise RuntimeError('runner exited without a terminal status; inspect preserved logs')
        write_json(ROOT / 'monitor-status.json', {'state': 'watching', 'updated': now(), 'runner_pid': pid})
        time.sleep(30)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        write_json(ROOT / 'monitor-status.json', {'state': 'failed', 'updated': now(), 'error': str(error)})
        raise
