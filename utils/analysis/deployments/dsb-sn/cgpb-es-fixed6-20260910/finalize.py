#!/usr/bin/env python3
"""Verify restoration and write the fixed-6 comparison report."""
import hashlib
import json
from pathlib import Path
import statistics

import run_phase as run


def main():
    restored = json.loads((run.ROOT / 'restored-random2_6/result.json').read_text())
    comparison = json.loads((run.ROOT / 'comparison.json').read_text())
    actual = json.loads((run.ROOT / 'fixed6/result.json').read_text())
    summaries = comparison['summaries']
    logs = json.loads((run.ROOT / 'fixed6/log-check.json').read_text())
    counters = {key: sum(value['sdk'].get(key, 0) for value in logs.values())
                for key in ['spans_received', 'spans_sent', 'spans_dropped', 'cp_received', 'cp_sent']}
    expected_spans = (actual['requests'] + run.SPEC['warmup_requests']) * 23
    assert counters['spans_received'] == counters['spans_sent'] == expected_spans
    assert counters['spans_dropped'] == 0
    assert counters['cp_received'] == counters['cp_sent'] == (actual['requests'] + 5) * 9
    for resource in ['deployments', 'daemonsets']:
        current = json.loads(run.kube('get', resource, '-o', 'json'))
        run.save(run.ROOT / ('after-' + resource + '.json'), current)
        before = json.loads((run.ROOT / ('before-' + resource + '.json')).read_text())
        normalize = lambda items: {item['metadata']['name']: item['spec'] for item in items}
        assert normalize(current['items']) == normalize(before['items']), resource
    for path, expected in json.loads((run.ROOT / 'workflow-source-hashes.json').read_text()).items():
        assert hashlib.sha256((run.REPO / path).read_bytes()).hexdigest() == expected, path
    phase = run.SPEC['restore_config']
    expected_images = dict(run.APPS, **{run.DS: run.COLLECTORS['phases'][phase['name']]})
    digests = json.loads((run.ROOT / 'image-digests.json').read_text())
    run.health(run.ROOT, 'final', expected_images, digests)
    start = json.loads((run.OLD / 'monitor-start.json').read_text())
    cmdline = Path(f'/proc/{start["pid"]}/cmdline').read_bytes()
    assert str(run.OLD / 'monitor.py').encode() in cmdline
    monitor = json.loads((run.OLD / 'monitoring/latest.json').read_text())
    assert monitor['timestamp'] >= start['started_at']
    assert not monitor['alerts'] and monitor['ready_pods'] == 35 and monitor['total_restarts'] == 0
    assert all(probe['ok'] for probe in monitor['api'].values())
    verification = {
        'verified_at': run.stamp(), 'restored_config': phase['config_map'],
        'all_workload_specs_restored': True, 'service_source_hashes_unchanged': True,
        'backend_pods_unchanged': True, 'ready_pods': 35, 'restarts': 0,
        'fixed6_sdk_counters_including_warmup': counters,
        'monitor_pid': start['pid'], 'monitor_timestamp': monitor['timestamp'],
        'monitor_alerts': monitor['alerts'], 'timeline_probes': monitor['api'],
    }
    run.save(run.ROOT / 'final-verification.json', verification)
    names = [('fixed2', 'Fixed 2 (legacy)'), ('fixed4', 'Fixed 4'),
             ('fixed6', 'Fixed 6'), ('random2_6', 'Random 2-6')]
    rows = '\n'.join(
        f'| {label} | {summaries[name]["checkpoints_per_request"]:.2f} | '
        f'{summaries[name]["bridge_payload_bytes_per_request"]:.2f} | '
        f'{summaries[name]["bloom_bytes_per_request"]:.2f} |'
        for name, label in names)
    component_rows = '\n'.join(
        f'| {label} | {values["bloom"]:.2f} | '
        f'{values["checkpoint_anchor_and_depth"] + values["window_descriptor"] + values["ordinary_depth"]:.2f} | '
        f'{values["branch_records"]:.2f} |'
        for name, label in names
        for values in [comparison['byte_components'][name]])
    root_rows = '\n'.join(
        f'| {distance} | {values["requests"]} | {values["checkpoints_per_request"]:.2f} | '
        f'{values["bridge_bytes_per_request"]:.2f} |'
        for distance, values in comparison['random_grouped_by_root_draw'].items())
    report = f'''# Fixed checkpoint distance 6 and the randomized byte increase

Fixed 6 measured **230 decoded bridge bytes and 9 checkpoints per request**.
This follows the earlier fixed-4 measurement of 228 bytes and 11 checkpoints.
Both use the corrected TTL format with an immutable window-distance descriptor.
The random 2-6 measurement averages 243.62 bytes and 11.56 checkpoints.

| Configuration | Checkpoints/request | Bridge bytes/request | Bloom bytes/request |
| --- | ---: | ---: | ---: |
{rows}

The comparison uses the first **99 matching requests** from each phase. Fixed 6
completed **{actual['requests']} distinct stored posts and complete traces**, with
99 client acknowledgments and one completion after wrk's timed cutoff. The extra
trace was retained and validated. Each trace has 23 spans and a deepest path of
7 spans, counting both client and server spans. MongoDB input hashes and native
call-graph signatures match the earlier runs in request order.

## Why fixed 6 is 230 bytes

Fixed 6 has one root checkpoint and eight server-leaf checkpoints. Five leaves
are at depth 4 and three at depth 6. There are no interior checkpoints. Every
Bloom filter is sized for the intended distance of 6, including early leaves:
12 bytes per filter, or 108 bytes per request.

The exact accounting is:

```text
9 checkpoints x (1 depth + 8 anchor + 1 window descriptor + 12 Bloom)
  + 14 ordinary spans x 1 depth byte
  + 2 branch records x 9 bytes
= 230 bytes/request
```

Moving from fixed 4 to fixed 6 increases Bloom bytes by 20, while combined
checkpoint/depth metadata falls by 18 because there are two fewer checkpoints.
Branch records remain 18 bytes. The net increase is therefore only 2 bytes.
Linear extrapolation from fixed 2 to fixed 4 does not account for this change
in checkpoint count. The fixed-2 reference also uses the legacy format without
the per-checkpoint window descriptor.

## Why randomized 2-6 costs more on this graph

The countdown advances through both client and server spans. Scheduled
checkpoints can therefore land on clients. The existing OnEnd policy also
checkpoints every server leaf, even when its incoming TTL has not expired.
With reverse rejection disabled in these experiments, all those checkpoints
are retained.

For a root draw of 3, seven fanout client spans checkpoint at depth 3. Five of
their immediate server children are leaves, which checkpoint again at depth 4.
The longer branches can add scheduled checkpoints at depth 5 immediately before
their leaf checkpoints at depth 6. Those traces contain 16 or 18 checkpoints.
A root draw of 5 similarly places three client checkpoints at depth 5 before
their leaf children at depth 6. Fixed 6 needs only the root and eight leaves.

The following table groups the **existing random run by its root draw**.
Descendant windows continue making independent random draws; these rows are
not separate fixed-CPD experiments.

| Root draw | Requests | Checkpoints/request | Bridge bytes/request |
| --- | ---: | ---: | ---: |
{root_rows}

The mean selected distance alone does not determine cost on a finite fanout
graph. Checkpoint placement, mandatory leaves, and the number of copies of
each window's filter all affect the total. Per-window sizing is verified here;
the larger total persists because those filters and headers occur at different
and sometimes additional checkpoints.

| Configuration | Bloom bytes | Checkpoint/depth metadata bytes | Branch-record bytes |
| --- | ---: | ---: | ---: |
{component_rows}

Relative to fixed 4, random 2-6 adds 7.07 Bloom bytes, 5.00 net checkpoint/depth
metadata bytes, and 3.55 branch-record bytes per request: 15.62 total, or 6.85%.
The increase also remains if all branch-record bytes are excluded: 222.07
versus 210.00 bytes/request. The pre-existing CGPB branch-record anomaly is
unchanged; see [the recorded observation](../cgpb-es-random-cpd-20260910/HA-OBSERVATION.md).
This report does not establish HA reconstruction correctness.

## Run and deployment verification

The follow-up used the unchanged repository `examples/dsb_sn/scripts/compose-post.lua`,
seed 42, one thread and connection, 5 requests/sec for 20 seconds, and five
warmup requests at 1 request/sec with seed 999. The fourteen application image
digests were reused. A collector configuration image was built and pushed from
the same immutable collector binary, changing only `cpd_min: 6, cpd_max: 6`.
All nine collector endpoints and fourteen SDK startup configurations were
verified before requests began. No service implementation changed.

All raw payloads were decoded and checked against native span ancestry, depth,
checkpoint anchors, filter width, exact Bloom bits, and the fixed-6 schedule.
SDK counters including warmup report {expected_spans} spans received and sent,
zero dropped spans, and {(actual['requests'] + 5) * 9} checkpoints received and sent.
There were no HTTP/socket failures or SDK/collector measurement log errors.
Root latency was {summaries['fixed6']['root_latency_median_ms']:.2f} ms median
and {summaries['fixed6']['root_latency_p95_ms']:.2f} ms p95. These small sequential
runs do not establish latency or throughput effects.

**Random 2-6 was restored at {restored['restored_at']}.** All deployment and
DaemonSet specifications match the pre-follow-up state. The twelve backend
and trace-storage pods retained their UIDs and data. Final health is 35/35
Ready with zero restarts. The original observer directory is active again;
monitor PID {start['pid']} has successful timeline probes and no alerts.

Bytes mean the decoded `_br` and `_d` payloads only. Forward baggage, other span
attributes, OTLP envelopes, and compression are outside this measurement.

Evidence: `fixed6/traces.json`, `fixed6/analysis.json`, `fixed6/span-details.csv`,
`comparison.json`, `input-equivalence.json`, `final-verification.json`,
`fixed6-run.log`, `restore-random.log`, and `collector-build-push.log`.
The earlier three-phase report is [here](../cgpb-es-window-cpd-20260910/RESULTS.md).
'''
    (run.ROOT / 'RESULTS.md').write_text(report)
    previous_report = run.OLD / 'RESULTS.md'
    text = previous_report.read_text()
    link = '../cgpb-es-fixed6-20260910/RESULTS.md'
    if link not in text:
        note = ('\nA subsequent **fixed-6 control measured 230 bytes/request and 9 checkpoints/request**. '
                'The [follow-up report](' + link + ') explains why the randomized run is larger, '
                'including scheduled client checkpoints immediately before mandatory server-leaf checkpoints.\n')
        marker = '\nAll compared traces contain **23 spans**'
        assert marker in text
        previous_report.write_text(text.replace(marker, note + marker, 1))
    run.report('Restoration verified, monitor healthy, report written')


if __name__ == '__main__':
    main()
