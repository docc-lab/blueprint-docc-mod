#!/usr/bin/env python3
"""Tomislav-RetCtx: partial results from the n=5 matrix, as it accumulates.

Each round completes all eight configurations once, so the table is balanced at
every round boundary. Reports per-configuration peak completed throughput with the
spread across rounds, and the paired ON/OFF gap per bridge -- paired because each
round's ON and OFF arms run ~90 minutes apart rather than on different nights.

Usage: matrix_status.py
"""
import glob
import json
import re
import statistics
from pathlib import Path

SCRATCH = Path(__file__).resolve().parent
ROOTS = SCRATCH / 'matrix_roots.txt'


# A ramp is 28 rates. Tomislav-RetCtx: require all of them before the case counts.
# Reading an in-flight ramp reports its peak-so-far, which early in the ramp is just
# the current offered rate -- that produced a "CGPB off peak 500" and a -1394% gap.
RAMP_POINTS = 28


def peak(root, case):
    """Peak completed rps, or None if this case's ramp has not finished."""
    values = []
    for f in glob.glob(f'{root}/run/{case}/rate-*/result.json'):
        d = json.load(open(f))
        if 'completed_rps' in d:
            values.append(d['completed_rps'])
    return max(values) if len(values) == RAMP_POINTS else None


def main():
    if not ROOTS.exists():
        raise SystemExit('no rounds started yet')
    peaks, rounds_seen = {}, set()
    for line in ROOTS.read_text().split():
        root = Path(line)
        if not (root / 'run').is_dir():
            continue
        m = re.search(r'-m5-(on|off)-r(\d+)-', root.name)
        if not m:
            continue
        arm, rnd = m.group(1), int(m.group(2))
        # Only count an arm as seen once every one of its ramps is finished.
        cases = [p.name for p in (root / 'run').iterdir() if p.is_dir()]
        if cases and all(peak(root, c) is not None for c in cases):
            rounds_seen.add((rnd, arm))
        for case in sorted(p.name for p in (root / 'run').iterdir() if p.is_dir()):
            kind = case[3:]
            # vanilla and no-tracing have one configuration; they only appear in
            # the ON arm because the runner forces reverse off for non-bridges.
            label = kind if kind in ('nt', 'v') else f'{kind} {arm}'
            value = peak(root, case)
            if value is not None:
                peaks.setdefault(label, []).append((rnd, value))

    complete = sum(1 for r in range(1, 6) if (r, 'on') in rounds_seen and (r, 'off') in rounds_seen)
    print(f'complete rounds: {complete}/5   (arms started: {len(rounds_seen)}/10)\n')
    print(f'{"configuration":14} {"n":>2} {"mean":>8} {"min":>8} {"max":>8} {"spread":>7}')
    order = ['nt', 'v', 'pb on', 'pb off', 'cgpb on', 'cgpb off', 'sb on', 'sb off']
    for label in order:
        vals = [v for _, v in sorted(peaks.get(label, []))]
        if not vals:
            continue
        lo, hi = min(vals), max(vals)
        print(f'{label:14} {len(vals):2} {statistics.mean(vals):8.0f} {lo:8.0f} {hi:8.0f} '
              f'{100 * (hi - lo) / statistics.mean(vals):6.1f}%')

    print(f'\n{"paired ON vs OFF (per round)":38}')
    for kind in ('pb', 'cgpb', 'sb'):
        on = dict(peaks.get(f'{kind} on', []))
        off = dict(peaks.get(f'{kind} off', []))
        pairs = [(r, on[r], off[r]) for r in sorted(set(on) & set(off))]
        if not pairs:
            continue
        gaps = [100 * (o - n) / o for _, n, o in pairs]
        detail = '  '.join(f'r{r}:{g:+.1f}%' for (r, _, _), g in zip(pairs, gaps))
        mean = statistics.mean(gaps)
        sd = statistics.stdev(gaps) if len(gaps) > 1 else 0.0
        print(f'  {kind:5} n={len(gaps)}  response-path cost {mean:5.1f}% (sd {sd:.1f})   {detail}')


if __name__ == '__main__':
    main()
