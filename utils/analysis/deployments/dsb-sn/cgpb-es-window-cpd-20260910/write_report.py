#!/usr/bin/env python3
"""Write the corrected comparison from verified, archived measurements."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
summaries = json.loads((ROOT / 'comparison.json').read_text())
equivalence = json.loads((ROOT / 'input-equivalence.json').read_text())
assert equivalence['matched']
verification = json.loads((ROOT / 'final-verification.json').read_text())
assert not verification['monitor']['alerts']
random = summaries[-1]
n = random['requests']
labels = ['Fixed 2 (legacy format)', 'Fixed 4 (window descriptor)', 'Random 2–6 (window descriptor)']
rows = []
for label, summary in zip(labels, summaries):
    rows.append(f'| {label} | {summary["checkpoints_per_request"]:.2f} | {summary["bridge_payload_bytes_per_request"]:.2f} | {summary["bloom_bytes_per_request"]:.2f} | {summary["root_latency_median_ms"]:.2f} / {summary["root_latency_p95_ms"]:.2f} ms |')
table = '\n'.join(rows)
hypothetical = random['hypothetical_original_max_sized_payload_bytes_per_request']
savings = (1 - random['bridge_payload_bytes_per_request'] / hypothetical) * 100
counts = random['root_draw_counts']
draw_mean = sum(int(d) * count for d, count in counts.items()) / n
draw_row = ' | '.join(str(counts.get(str(d), 0)) for d in range(2, 7))
distribution = random['checkpoint_count_distribution']
total_measured = verification['measured_posts']
total_warmup = sum(verification['warmup_posts'].values())
phase_counts = ', '.join(f'{s["phase"]}: {s["all_measured_requests"]}' for s in summaries)
report = f'''# Checkpoint windows sized for their selected distance

The corrected SDK sizes every PB/CGPB Bloom filter for its window's **selected
checkpoint distance**, using capacity `max(1, selected_distance - 1)` and the
existing false-positive target 0.0001. It no longer sizes ranged windows for
`cpd_max`. The corrected images are deployed in `dsb-sn`, with range **2–6** live.
Service implementations are unchanged.

| Selected CPD | 2 | 3 | 4 | 5 | 6 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Bloom bytes, verified in exported trusses | 3 | 5 | 8 | 10 | 12 |

![Observed filter sizes](filter-sizes.png)

The mutable TTL remains the first forward-baggage byte. An additional immutable
byte beside the filter stores `window_distance - 1`. Decoders use it to derive
exact Bloom m/k and byte length, including the boundary before CGPB's trailing
hash array. An ordinary span retains that descriptor while decrementing TTL.
A checkpoint exports the incoming window and builds a new outgoing filter for
its new draw. Early leaves and returned trusses retain their original window
geometry. A root uses its first draw for both its empty emitted filter and its
outgoing window.

```text
ranged PB/CGPB truss = uvarint(depth) || checkpoint_span_id(8)
                      || byte(window_CPD - 1) || Bloom || optional_CGPB_HA
forward baggage     = base64url(TTL_byte || truss)
```

The decoder checks each filter's actual width and exact bits against the
intended distance and intervening native ancestor span IDs. It also checks
checkpoint anchors, encoded depths, countdown gaps, agreement between sibling
window descriptors, and complete native parent trees. All those checks passed.
The old fixed `cpd` format is preserved. SB has no path Bloom filter and retains
its existing truss format.

The rerun used the unchanged repository `compose-post.lua`, seed 42, one thread,
one connection, target 5 requests/sec, and 20 seconds per phase. Each phase had
five warmup requests at 1 request/sec with seed 999. The timed cutoff produced
these actual server counts: **{phase_counts}**. The comparison uses the first
**{n} matching requests per phase**, in request order. Every additional trace
is retained and validated. No replacement requests were sent.

| Setting | Checkpoints/request | Exported bridge bytes/request | Bloom bytes/request | Root latency median / p95 |
| --- | ---: | ---: | ---: | ---: |
{table}

All compared traces contain **23 spans**, with **7 spans from root to deepest
leaf**, including client and server spans. Their call graphs match in request
order. MongoDB verification also confirms matching creators, original text,
mentions, media IDs/types, expanded URLs, and post types after normalizing
generated IDs and shortened URLs. There are **{total_measured} distinct measured
posts**, plus **{total_warmup} warmup posts**. Raw client acknowledgment counts
and any completion after the timed client cutoff are recorded per phase in
`measured-run.json`; wrk reported no HTTP or socket errors.

![Corrected comparison](comparison.png)

Randomized checkpoint counts ranged from **{min(map(int, distribution))} to
{max(map(int, distribution))}** per trace. Every trace includes one root and
eight server-leaf checkpoints, so nine checkpoints are unavoidable under the
existing leaf policy. Fanout can create several checkpoints at a level where
the countdown expires. Leaves may checkpoint earlier than the selected
distance, but their filters remain sized for the intended window.

| Root-selected distance | 2 | 3 | 4 | 5 | 6 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Requests in the matched prefix | {draw_row} |

The window descriptors expose these draws directly. The observed mean root
draw was **{draw_mean:.3f}**, compared with the configured distribution's mean of
4. Fixed 4 matches the nominal mean; finite sampling and graph structure still
affect the comparison. These small, sequential runs do not establish a latency
or throughput improvement.

To separate sizing from different random draws, the analyzer also calculates
what the **same observed randomized checkpoint placements** would have cost
under the original maximum-sized format: **{hypothetical:.2f} bytes/request**,
versus **{random['bridge_payload_bytes_per_request']:.2f} measured** now. This is
a **{savings:.1f}% reduction** for that calculation, including the new descriptor's
cost. The calculated reference uses a 12-byte Bloom at every checkpoint and
omits the new descriptor, exactly as the previous ranged format did. It is an
arithmetic comparison on the current traces, not a separately measured run.

Bridge bytes are the decoded `_br` and `_d` payloads, including the new immutable
descriptor. They exclude other span fields, attribute names, protobuf/resource
envelopes, compression, and forward baggage. They are not total network traffic.
The fixed-4 TTL control also carries the one-byte descriptor, adding one byte
per checkpoint compared with the earlier fixed-4 measurement.

![Actual checkpoint paths](checkpoint-paths.png)

All fourteen SDK images were rebuilt and pushed, and their running image IDs
match registry digests. The collector binary and three collector configuration
images were reused from the previous build: they already serve the bounds and
pass the payload through. Traffic paused during each coordinated restart, and
all nine collector endpoints plus all fourteen SDK startup configurations were
verified before requests began. The twelve MongoDB/Redis/Jaeger/Elasticsearch
pods retained their UIDs. Final health is **35/35 Ready, zero restarts**, with
zero SDK span drops and resumed monitoring without alerts.

The separate pre-existing CGPB branch-record anomaly remains unchanged; see
[the prior observation](../cgpb-es-random-cpd-20260910/HA-OBSERVATION.md). Byte
counts include those records. No claim is made here about HA-based branch
reconstruction correctness.

The prior maximum-sized results are preserved in
`../cgpb-es-random-cpd-20260910/` and marked superseded for this sizing policy.
Raw evidence is in each phase directory; decoded rows are in `span-details.csv`.
See `comparison.json`, `input-equivalence.json`, `final-verification.json`,
`sdk-tests.log`, and [DEPLOYMENT.md](DEPLOYMENT.md) for the rest of the record.
'''
(ROOT / 'RESULTS.md').write_text(report)
print(f'Wrote corrected results for {n} matched requests per phase; {savings:.1f}% sizing reduction on the same placements.')
