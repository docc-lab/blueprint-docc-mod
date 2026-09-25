#!/usr/bin/env python3
"""Tomislav-RetCtx: durable file-based progress and final audit; no workload traffic."""
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
    seen = set()
    missing_process_ticks = 0
    while True:
        status = json.loads((ROOT / 'run-status.json').read_text())
        rows = []
        for path in sorted((ROOT / 'run').glob('*/rate-*/result.json')):
            if '-interrupted-' in path.parent.parent.name:
                continue
            row = json.loads(path.read_text())
            if 'kind' not in row:
                continue
            rows.append(row)
            if str(path) not in seen:
                event = {key: row[key] for key in ('kind', 'repetition', 'offered_rps', 'successful_rps',
                                                  'p99_ms', 'non_2xx_3xx', 'socket_errors', 'finished')}
                event['path'] = str(path)
                with (ROOT / 'progress-events.jsonl').open('a') as stream:
                    stream.write(json.dumps(event) + '\n')
                seen.add(str(path))
        complete_runs = [p for p in (ROOT / 'run').glob('*/complete.json')
                         if '-interrupted-' not in p.parent.name]
        latest = max(rows, key=lambda r: r['finished']) if rows else None
        plan = json.loads((ROOT / 'plan.json').read_text())
        total_runs = len(plan['variants']) * plan['repetitions']
        total_points = total_runs * len(plan['ramp_rates'])
        progress = {'updated': now(), 'runner': status, 'completed_points': len(rows), 'total_points': total_points,
                    'completed_runs': len(complete_runs), 'total_runs': total_runs,
                    'completed_requests': sum(r['completed_requests'] for r in rows),
                    'non_2xx_3xx': sum(r['non_2xx_3xx'] for r in rows), 'latest_point': latest}
        write_json(ROOT / 'progress.json', progress)
        lines = ['# Tomislav-RetCtx: active end-to-end ramp', '',
                 f"Updated: {progress['updated']}", '',
                 f"State: {status['state']}; {len(rows)}/{total_points} points, {len(complete_runs)}/{total_runs} ramps complete.",
                 f"Current variant/round/stage: {status.get('kind', '-')}/{status.get('repetition', '-')}/{status.get('stage', '-')}", '',
                 'Vanilla memory_limiter versus PB/CGPB/SB priority+batch; 500m CPU/256MiB per collector.',
                 'Bridge CPD uniform 2–6; inverse_depth reverse policy; unscheduled-leaf rejection.',
                 'Fresh-state 2k–5k ramps, step200, 30s/point; five paired rounds.', '']
        if latest:
            lines += [f"Latest: {latest['kind']} round {latest['repetition']}, offered {latest['offered_rps']}/s, "
                      f"successful {latest['successful_rps']:.1f}/s, p99 {latest['p99_ms']:.2f}ms, "
                      f"HTTP errors {latest['non_2xx_3xx']}.", '']
        (ROOT / 'STATUS.md').write_text('\n'.join(lines))
        if status['state'] == 'complete':
            with (ROOT / 'logs/final-analysis.log').open('w') as log:
                subprocess.run([sys.executable, '-B', str(REPO / 'utils/analyze_dsb_sn_e2e.py'),
                                '--out', str(ROOT)], check=True, stdout=log, stderr=subprocess.STDOUT,
                               timeout=600)
            curves = json.loads((ROOT / 'analysis/curves.json').read_text())
            audit = json.loads((ROOT / 'analysis/audit.json').read_text())
            report = ['# Tomislav-RetCtx: Social Network end-to-end ramp', '',
                      'All 320 points across 20 fresh-state ramps completed. Five paired repetitions use '
                      '2,000–5,000 requests/s in steps of 200, with 30 seconds per point.', '',
                      'Vanilla uses the stock collector memory_limiter; PB/CGPB/SB use the priority processor. '
                      'Both use batch and OTLP to Jaeger/Elasticsearch. Each node-local collector has '
                      '500m CPU/256MiB, 50% soft/70% hard thresholds. Bridges use uniform CPD2–6, '
                      'inverse-depth reverse returns, and rejection at unscheduled server leaves.', '',
                      '| Variant | Best mean successful requests/s | Offered rate there | p99 at 5k (ms) |',
                      '| --- | ---: | ---: | ---: |']
            for kind in plan['variants']:
                data = [p for p in curves if p['kind'] == kind]
                best = max(data, key=lambda p: p['successful_rps']['mean'])
                end = next(p for p in data if p['offered_rps'] == 5000)
                report += [f"| {kind} | {best['successful_rps']['mean']:.1f} | {best['offered_rps']} | "
                           f"{end['p99_ms']['mean']:.2f} |"]
            report += ['', 'The figure shows means with sample standard deviation across repetitions. '
                       'Use the full curves to assess saturation; the best mean is not a fitted capacity estimate.', '',
                       f"Raw-count audit: {audit['raw_verified_points']} points verified; "
                       f"{len(audit['issues'])} recorded telemetry/restart/sample issues. Inspect analysis/audit.json.", '',
                       'Stored trace samples describe retained data and return-payload validity; they do not '
                       'establish reconstruction accuracy. Request success does not imply complete tracing.', '',
                       'Artifacts: analysis/end-to-end.pdf, analysis/points.csv, analysis/curves.json, '
                       'analysis/points.json; complete logs, snapshots, trace samples and manifests remain under this root.', '',
                       'Paper gaps, recovered settings, exact pipeline configuration and known differences are '
                       'recorded in plan.json and the repository guide docs/dev/dsb_sn_retctx_evaluation.md. '
                       'Application implementations were unchanged. The final deployment remains available.', '']
            (ROOT / 'RESULTS.md').write_text('\n'.join(report))
            write_json(ROOT / 'monitor-status.json', {'state': 'complete', 'updated': now(),
                                                     'raw_verified_points': audit['raw_verified_points']})
            return
        if status['state'] == 'failed':
            write_json(ROOT / 'monitor-status.json', {'state': 'runner-failed', 'updated': now(), 'runner': status})
            return
        pid = int((ROOT / 'run.pid').read_text())
        command = Path(f'/proc/{pid}/cmdline')
        alive = command.exists() and b'run_dsb_sn_e2e.py' in command.read_bytes()
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
