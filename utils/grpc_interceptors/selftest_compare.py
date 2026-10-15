#!/usr/bin/env python3
"""Check that compare_traces.py actually notices differences.

Usage:
    selftest_compare.py <run dir>

Takes a recorded run (results dir of run_local.sh), writes corrupted copies of its spans.jsonl to a
temporary directory, and runs compare_traces.py on (original, copy).  The unmodified copy must be
reported as SAME and every corrupted copy as DIFFERENT.

Each corruption changes one leaf span of trace #3 (in start order):
  rename     change the span name
  drop       remove the span
  attr       add an attribute
  scope      change the tracer (scope) name
  kind       change the span kind
  reparent   move the span under a different parent
"""
import copy
import json
import os
import subprocess
import sys
import tempfile

here = os.path.dirname(os.path.abspath(__file__))
run = sys.argv[1]
spans = [json.loads(l) for l in open(os.path.join(run, 'spans.jsonl'))]

traces = {}
for s in spans:
    traces.setdefault(s['trace_id'], []).append(s)
victim = sorted(traces, key=lambda t: min(s['start_ns'] for s in traces[t]))[3]
tspans = traces[victim]
parents = {s['parent_span_id'] for s in tspans}
leaf_id = next(s['span_id'] for s in tspans if s['span_id'] not in parents and s['parent_span_id'])
leaf_parent = next(s['parent_span_id'] for s in tspans if s['span_id'] == leaf_id)
other_id = next(s['span_id'] for s in tspans if s['span_id'] not in (leaf_id, leaf_parent))

# Each corruption gets a copy of the leaf span and returns the replacement (None = drop it).
corruptions = {
    'unchanged': lambda s: s,
    'rename':    lambda s: {**s, 'name': s['name'] + '_x'},
    'drop':      lambda s: None,
    'attr':      lambda s: {**s, 'attributes': {**s['attributes'], 'extra': 'int:1'}},
    'scope':     lambda s: {**s, 'scope': s['scope'] + '_x'},
    'kind':      lambda s: {**s, 'kind': 'INTERNAL'},
    'reparent':  lambda s: {**s, 'parent_span_id': other_id},
}

failures = 0
with tempfile.TemporaryDirectory() as tmp:
    for name, corrupt in corruptions.items():
        out = os.path.join(tmp, name)
        os.makedirs(out)
        with open(os.path.join(out, 'spans.jsonl'), 'w') as f:
            for s in spans:
                if s['span_id'] == leaf_id:
                    s = corrupt(copy.deepcopy(s))
                if s is not None:
                    f.write(json.dumps(s) + '\n')
        r = subprocess.run([sys.executable, os.path.join(here, 'compare_traces.py'), run, out],
                           capture_output=True, text=True)
        verdict = 'SAME' if r.returncode == 0 else 'DIFFERENT'
        expected = 'SAME' if name == 'unchanged' else 'DIFFERENT'
        failures += verdict != expected
        print(f'{name:10s} -> {verdict:9s} {"ok" if verdict == expected else "WRONG, expected " + expected}')

print('selftest', 'FAILED' if failures else 'passed')
sys.exit(1 if failures else 0)
