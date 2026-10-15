#!/usr/bin/env python3
"""Compare the distribution of attribute values, per span name, between runs.

Usage:
    attr_stats.py <run A> <run B> [<run C> ...] [--keys _br,_rc,_d,_o] [--rows N]

Some bridge attributes cannot be compared trace by trace, because they depend on things that change
from run to run even for the same build: random checkpoint draws (with the real SN config), or the
order in which concurrent calls start and finish (SB's start ordinals `_o`, the size of CGPB / SB
`_br` records).  What must match between runs is their distribution.

For every (span name, attribute) the script builds the distribution of a "signature" of the value:
  * the value itself if it is short (<= 16 characters, e.g. `_o=bytes:02`, `_d=bytes:05`),
  * otherwise its length (e.g. `len=48`), because long values embed random span IDs,
  * `<absent>` when the span does not carry the attribute.
It then computes the total variation distance (TVD) between each run and the first run: half the
sum of absolute differences of the probabilities.  0 = identical distributions, 1 = disjoint.

Typical use: A = wrapper run, B = another wrapper run (shows the run-to-run noise), C = interceptor
run.  The interceptor's largest TVD should be about the same as B's.  The script prints the rows
with the largest TVD and, at the end, the largest TVD of every run against A.
"""
import argparse
import collections
import json
import os


def signature(value):
    kind, _, raw = value.partition(':')
    return value if len(raw) <= 16 else f'{kind}:len={len(raw)}'


def distributions(run, keys):
    dist = collections.defaultdict(collections.Counter)   # (name, key) -> Counter(signature)
    for line in open(os.path.join(run, 'spans.jsonl')):
        s = json.loads(line)
        for k in keys:
            v = s['attributes'].get(k)
            dist[s['name'], k][signature(v) if v is not None else '<absent>'] += 1
    return dist


def tvd(p, q):
    n_p, n_q = sum(p.values()), sum(q.values())
    return 0.5 * sum(abs(p[x] / n_p - q[x] / n_q) for x in set(p) | set(q))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('runs', nargs='+')
    ap.add_argument('--keys', default='_br,_rc,_d,_o')
    ap.add_argument('--rows', type=int, default=8, help='how many of the most different rows to print')
    args = ap.parse_args()
    keys = args.keys.split(',')
    labels = [os.path.basename(os.path.normpath(r)) for r in args.runs]
    dists = [distributions(r, keys) for r in args.runs]
    ref = dists[0]

    rows = []
    for row in sorted(ref):
        if set(ref[row]) == {'<absent>'} and all(set(d[row]) <= {'<absent>'} for d in dists[1:]):
            continue  # attribute never present on this span name
        rows.append((row, [tvd(ref[row], d[row]) for d in dists[1:]]))

    print(f'{len(rows)} (span name, attribute) rows compared; most different first:')
    print(f'{"span / attribute":60s}' + ''.join(f'{"TVD " + l[:18]:>26s}' for l in labels[1:]))
    for (name, k), ds in sorted(rows, key=lambda r: -max(r[1]))[:args.rows]:
        print(f'{(name + " " + k)[:60]:60s}' + ''.join(f'{d:26.3f}' for d in ds))
        for i, d in enumerate([ref] + dists[1:]):
            top = ', '.join(f'{sig}: {c / sum(d[name, k].values()):.0%}' for sig, c in d[name, k].most_common(4))
            print(f'      {labels[i][:24]:24s} {top}')
    print()
    for i, label in enumerate(labels[1:]):
        worst = max((ds[i] for _, ds in rows), default=0.0)
        print(f'largest TVD vs {labels[0]}: {label}: {worst:.3f}')

    # Every pair, so the reference run is not special: with three runs (wrapper, wrapper,
    # interceptor) the interceptor's pairs should look like the wrapper-wrapper pair.
    if len(dists) > 2:
        print('largest TVD for every pair of runs:')
        for i in range(len(dists)):
            for j in range(i + 1, len(dists)):
                keys_ij = set(dists[i]) | set(dists[j])
                worst = max((tvd(dists[i][r], dists[j][r]) for r in keys_ij
                             if sum(dists[i][r].values()) and sum(dists[j][r].values())), default=0.0)
                print(f'  {labels[i]} vs {labels[j]}: {worst:.3f}')


if __name__ == '__main__':
    main()
