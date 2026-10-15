#!/usr/bin/env python3
"""Summarise a latency_ab.sh run: wrappers vs interceptors, per load level.

Usage:
    latency_summary.py <out dir> [--csv FILE]

<out dir> is the output of latency_ab.sh: run directories named <n>_<build>_<unloaded|ramp>, each
holding wrk2 outputs (wrk.out for a single rate, wrk-<rate>.out for a ramp step).

For every load level (unloaded, and each ramp rate) and both builds it prints the achieved
throughput and the mean / p50 / p99 latency of each repetition, then the average of the two
repetitions and the interceptor-minus-wrapper difference.  Next to the difference it prints the
largest gap between two repetitions of the SAME build at that level ("noise"), so a difference
can be judged against how much a build differs from itself.  Failed requests (non-2xx, socket
errors, timeouts) are listed if there are any.

--csv writes one row per (run, level) with all numbers.
"""
import argparse
import csv
import glob
import os
import re
import statistics
import sys

UNITS = {'us': 1e-3, 'ms': 1.0, 's': 1e3, 'm': 6e4}


def ms(value, unit):
    return float(value) * UNITS[unit]


def parse(path):
    """Numbers from one wrk2 -L output file."""
    text = open(path).read()
    r = {'rps': float('nan'), 'mean': float('nan'), 'p50': float('nan'), 'p99': float('nan'),
         'errors': 0, 'requests': 0}
    m = re.search(r'Requests/sec:\s+([\d.]+)', text)
    if m:
        r['rps'] = float(m.group(1))
    m = re.search(r'^\s+Latency\s+([\d.]+)(us|ms|s|m)\s', text, re.M)
    if m:
        r['mean'] = ms(m.group(1), m.group(2))
    for pct, key in (('50.000', 'p50'), ('99.000', 'p99')):
        m = re.search(rf'^\s*{pct}%\s+([\d.]+)(us|ms|s|m)\s*$', text, re.M)
        if m:
            r[key] = ms(m.group(1), m.group(2))
    m = re.search(r'(\d+) requests in', text)
    if m:
        r['requests'] = int(m.group(1))
    m = re.search(r'Non-2xx or 3xx responses:\s+(\d+)', text)
    if m:
        r['errors'] += int(m.group(1))
    m = re.search(r'Socket errors: connect (\d+), read (\d+), write (\d+), timeout (\d+)', text)
    if m:
        r['errors'] += sum(int(x) for x in m.groups())
    return r


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('out')
    ap.add_argument('--csv')
    args = ap.parse_args()

    # results[level][build] -> list of per-repetition dicts, in run order
    results = {}
    rows = []
    for run in sorted(glob.glob(os.path.join(args.out, '*_*_*'))):
        name = os.path.basename(run)
        m = re.match(r'(\d+)_(wrapper|interceptor)_(unloaded|ramp)$', name)
        if not m or not os.path.isdir(run):
            continue
        n, build, mode = m.groups()
        for f in sorted(glob.glob(os.path.join(run, 'wrk*.out'))):
            rate = re.search(r'wrk-(\d+)\.out$', f)
            level = 'unloaded' if mode == 'unloaded' else f'{int(rate.group(1)):5d} req/s'
            r = parse(f)
            results.setdefault(level, {}).setdefault(build, []).append(r)
            rows.append({'run': name, 'build': build, 'level': level.strip(), **r})

    if not results:
        sys.exit(f'no runs found in {args.out}')

    def key(level):
        return (0, 0) if level == 'unloaded' else (1, int(level.split()[0]))

    print('All latencies in ms.  "noise" = largest gap between two repetitions of the same build.')
    for metric in ('mean', 'p50', 'p99'):
        print(f'\n== {metric} latency')
        print(f'{"load":>12s} | {"wrapper reps":>17s} {"interceptor reps":>17s} | '
              f'{"wrapper":>8s} {"intercept":>9s} {"diff":>8s} {"noise":>7s}')
        for level in sorted(results, key=key):
            w = [r[metric] for r in results[level].get('wrapper', [])]
            i = [r[metric] for r in results[level].get('interceptor', [])]
            if not w or not i:
                continue
            noise = max(max(w) - min(w), max(i) - min(i))
            wm, im = statistics.mean(w), statistics.mean(i)
            print(f'{level:>12s} | {" ".join(f"{x:8.2f}" for x in w):>17s} {" ".join(f"{x:8.2f}" for x in i):>17s} | '
                  f'{wm:8.2f} {im:9.2f} {im - wm:+8.2f} {noise:7.2f}')

    print('\n== achieved throughput (req/s), mean of repetitions')
    for level in sorted(results, key=key):
        w = statistics.mean(r['rps'] for r in results[level].get('wrapper', []))
        i = statistics.mean(r['rps'] for r in results[level].get('interceptor', []))
        print(f'{level:>12s} | wrapper {w:8.1f}   interceptor {i:8.1f}')

    errs = [r for r in rows if r['errors']]
    if errs:
        print('\n== failed requests')
        for r in errs:
            print(f'  {r["run"]} {r["level"]}: {r["errors"]} of {r["requests"]}')

    if args.csv:
        with open(args.csv, 'w', newline='') as f:
            w = csv.DictWriter(f, fieldnames=list(rows[0]))
            w.writeheader()
            w.writerows(rows)


if __name__ == '__main__':
    main()
