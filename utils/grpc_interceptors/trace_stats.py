#!/usr/bin/env python3
"""Summarise one local run: traces, spans per trace, and spans per name.

Usage:
    trace_stats.py <run dir> [--expect-spans N]

<run dir> is a results directory of run_local.sh.  Prints the number of traces, a histogram of
spans per trace and the count of every span name.  With --expect-spans N, exits 1 unless every
trace has exactly N spans (e.g. 23 for a no-work SN ComposePost); useful to check that sampling
never leaves partial traces behind.
"""
import argparse
import collections
import json
import os
import sys

ap = argparse.ArgumentParser()
ap.add_argument('run')
ap.add_argument('--expect-spans', type=int)
args = ap.parse_args()

traces = collections.defaultdict(list)
for line in open(os.path.join(args.run, 'spans.jsonl')):
    s = json.loads(line)
    traces[s['trace_id']].append(s)

sizes = collections.Counter(len(v) for v in traces.values())
names = collections.Counter(s['name'] for v in traces.values() for s in v)
print(f'traces: {len(traces)}   spans: {sum(len(v) for v in traces.values())}')
print('spans per trace: ' + ', '.join(f'{n} spans x{c}' for n, c in sorted(sizes.items())))
for name, c in sorted(names.items()):
    print(f'  {c:7d}  {name}')
if args.expect_spans is not None:
    bad = sum(c for n, c in sizes.items() if n != args.expect_spans)
    print(f'traces without exactly {args.expect_spans} spans: {bad}')
    sys.exit(1 if bad else 0)
