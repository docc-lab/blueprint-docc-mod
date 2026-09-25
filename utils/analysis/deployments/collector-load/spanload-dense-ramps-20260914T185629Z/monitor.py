#!/usr/bin/env python3
"""Tomislav-RetCtx: concise progress from atomic status and completed point records."""
from datetime import datetime, timezone
import json
from pathlib import Path

root = Path(__file__).resolve().parent
status = json.loads((root / 'status.json').read_text())
stage = status.get('stage')
result = {'time': datetime.now(timezone.utc).isoformat(), 'state': status['state'], 'points': status['finished_points'], 'total': status['planned_points']}
completed = root / 'results.json'
all_rows = {(row['stage'], row['target_spans_per_second']): row for row in json.loads(completed.read_text())} if completed.exists() else {}
if stage and status['state'] != 'complete':
    directory = Path(stage['out'])
    records = sorted((directory / stage['variant']).glob('*-summary.json'))
    result['stage'] = stage['name']
    result['points'] += len(records)
    for path in records:
        row = json.loads(path.read_text())
        all_rows[(stage['name'], row['target_spans_per_second'])] = row
    progress = directory / 'status.json'
    if progress.exists():
        result['collecting_rate'] = json.loads(progress.read_text()).get('target_spans_per_second')
    if records:
        row = json.loads(records[-1].read_text())
        result.update(last_rate=row['target_spans_per_second'], throughput=round(row['exported_spans_per_second']),
                      collector_cpu_percent=round(100*row['collector_cpu_cores'], 2),
                      max_sender_cores=round(max(row['generator_cpu_cores_per_process']), 3),
                      failed_spans=row['failed_spans'], refused_spans_per_second=row['refused_spans_per_second'],
                      accounting_passed=row['counts_reconciled'])
if (root / 'analysis-status.json').exists():
    result['analysis'] = json.loads((root / 'analysis-status.json').read_text())['state']
result['rpc_failed_spans_total'] = sum(row['failed_spans'] for row in all_rows.values())
result['collector_refused_spans_total'] = sum(row['full_phase_collector_deltas']['otelcol_receiver_refused_spans'] for row in all_rows.values())
result['peak_steady_rss_mib'] = round(max((row['collector_peak_rss_bytes'] for row in all_rows.values()), default=0) / (1 << 20), 1)
print(json.dumps(result))
if status['state'] in ('failed', 'interrupted'):
    print(json.dumps(status))
    print((root / 'logs' / (stage['name'] + '.log')).read_text()[-3000:])
