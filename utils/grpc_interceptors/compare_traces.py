#!/usr/bin/env python3
"""Compare the traces of two local runs (spans.jsonl files written by fakecollector).

Usage:
    compare_traces.py <run A> <run B> [--ignore-attr KEY ...] [--mask-attr KEY ...] [--show N]

<run A> and <run B> are results directories of run_local.sh (or spans.jsonl files).

How traces are matched: run_local.sh sends the same request sequence over one connection, so the
i-th trace of run A (ordered by the start time of its root span) should be the same request as the
i-th trace of run B.  The first min(count A, count B) traces are compared pairwise.

What "the same" means: each trace is turned into a canonical tree.  For every span the tree keeps
    service, tracer (scope) name, span name, kind, status, attributes, event names
and the span's children, sorted, so the order in which concurrent children happened to start does
not matter.  Trace IDs, span IDs, timestamps and durations are ignored.  Where an attribute value
contains a span ID of the same trace (hex), the ID is replaced by that span's position in the
canonical tree, so attributes that point at other spans still compare.

--ignore-attr KEY drops attribute KEY everywhere (repeatable), for attributes known to differ.
--mask-attr KEY keeps attribute KEY but compares only its type and length (repeatable), for values
that embed random span IDs in binary form (e.g. the bridges' _br / _rc), where only presence and
size can match between runs.

Output: how many traces match, then up to --show (default 3) mismatching pairs with the first
spans that differ.  Exit code 0 when every compared trace matches and both runs have the same
number of traces, 1 otherwise.
"""
import argparse
import collections
import json
import os
import re
import sys

HEX = re.compile(r'[0-9a-f]{16}')


def load(path):
    if os.path.isdir(path):
        path = os.path.join(path, 'spans.jsonl')
    traces = collections.defaultdict(list)
    with open(path) as f:
        for line in f:
            s = json.loads(line)
            traces[s['trace_id']].append(s)
    return traces


def canonical(spans, ignore, mask=()):
    """Canonical, hashable form of one trace (see module docstring), plus a readable listing."""
    by_id = {s['span_id']: s for s in spans}
    children = collections.defaultdict(list)
    roots = []
    for s in spans:
        if s['parent_span_id'] and s['parent_span_id'] in by_id:
            children[s['parent_span_id']].append(s)
        else:
            roots.append(s)

    # First pass: the shape of every subtree without attribute values, used to order siblings
    # deterministically and to give every span a position label like "0.2.1".
    def shape(s):
        kids = sorted(shape(c) for c in children[s['span_id']])
        return (s['name'], s['kind'], tuple(kids))

    position = {}

    def label(s, pos):
        position[s['span_id']] = pos
        kids = sorted(children[s['span_id']], key=lambda c: (shape(c), c['start_ns']))
        for i, c in enumerate(kids):
            label(c, f'{pos}.{i}')

    roots.sort(key=lambda s: (shape(s), s['start_ns']))
    for i, r in enumerate(roots):
        label(r, str(i))

    def norm_value(v):
        return HEX.sub(lambda m: f'<span {position[m.group(0)]}>' if m.group(0) in position else m.group(0), v)

    def attr_value(k, v):
        if k in mask:
            kind, _, raw = v.partition(':')
            return f'{kind}:<{len(raw)} chars>'
        return norm_value(v)

    def node(s):
        attrs = tuple(sorted((k, attr_value(k, v)) for k, v in s['attributes'].items() if k not in ignore))
        events = tuple(e['name'] for e in s.get('events') or [])
        kids = tuple(sorted(node(c) for c in children[s['span_id']]))
        return (s['service'], s['scope'], s['name'], s['kind'], s['status'], attrs, events, kids)

    tree = tuple(sorted(node(r) for r in roots))
    return tree, roots


def ordered(traces):
    """Traces sorted by the start time of their earliest span."""
    return sorted(traces.values(), key=lambda spans: min(s['start_ns'] for s in spans))


def flatten(tree, depth=0, out=None):
    """Readable lines for a canonical tree."""
    out = [] if out is None else out
    for service, scope, name, kind, status, attrs, events, kids in tree:
        a = ', '.join(f'{k}={v}' for k, v in attrs)
        out.append(f'{"  " * depth}{name} [{kind}] scope={scope} svc={service} status={status}'
                   + (f' attrs: {a}' if a else '') + (f' events: {list(events)}' if events else ''))
        flatten(kids, depth + 1, out)
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('a')
    ap.add_argument('b')
    ap.add_argument('--ignore-attr', action='append', default=[])
    ap.add_argument('--mask-attr', action='append', default=[])
    ap.add_argument('--show', type=int, default=3)
    args = ap.parse_args()
    ignore = set(args.ignore_attr)
    mask = set(args.mask_attr)

    ta, tb = ordered(load(args.a)), ordered(load(args.b))
    n = min(len(ta), len(tb))
    print(f'traces: A={len(ta)} B={len(tb)}; comparing the first {n} pairwise')
    print(f'spans:  A={sum(map(len, ta))} B={sum(map(len, tb))}')

    mismatches = []
    for i in range(n):
        ca, _ = canonical(ta[i], ignore, mask)
        cb, _ = canonical(tb[i], ignore, mask)
        if ca != cb:
            mismatches.append((i, ca, cb))
    print(f'identical traces: {n - len(mismatches)} / {n}')

    for i, ca, cb in mismatches[:args.show]:
        la, lb = flatten(ca), flatten(cb)
        print(f'\n--- trace #{i}: A has {len(la)} spans, B has {len(lb)} spans; first differences:')
        shown = 0
        for x, y in zip(la, lb):
            if x != y:
                print(f'  A: {x}\n  B: {y}')
                shown += 1
                if shown == 5:
                    break
        if len(la) != len(lb):
            longer, tag = (la, 'A') if len(la) > len(lb) else (lb, 'B')
            for extra in longer[min(len(la), len(lb)):][:5]:
                print(f'  only in {tag}: {extra}')

    # Also a whole-run view that does not depend on pairing: how often each span name appears.
    na = collections.Counter(s['name'] for t in ta[:n] for s in t)
    nb = collections.Counter(s['name'] for t in tb[:n] for s in t)
    if na != nb:
        print('\nspan name counts differ (A vs B):')
        for name in sorted(set(na) | set(nb)):
            if na[name] != nb[name]:
                print(f'  {name}: {na[name]} vs {nb[name]}')

    ok = not mismatches and len(ta) == len(tb)
    print('\nRESULT:', 'SAME' if ok else 'DIFFERENT')
    sys.exit(0 if ok else 1)


if __name__ == '__main__':
    main()
