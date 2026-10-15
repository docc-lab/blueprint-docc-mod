#!/usr/bin/env python3
"""Where do goroutine stacks grow?  Reads CPU profiles and sums the CPU spent growing stacks
(runtime.newstack: the runtime copies a too-small stack to one twice the size and adjusts every
frame on it) by the function that crossed the limit (the frame right below runtime.newstack) and by
the goroutine it happened on (the bottom frame of the stack).

Usage:
    stack_growth.py <profile> [<profile> ...] [--top N] [--chains]

    profile   pprof CPU profiles (e.g. <run>/pprof/*_proc.pb.gz from run_local.sh with
              PROFILE_SECONDS; the allocs profiles are skipped); several are merged
    --top N   rows per table (default 12)
    --chains  also print, for the top functions, the call chain below them (who called them)

Needs `go` on PATH (go tool pprof).

Example:
    utils/grpc_interceptors/stack_growth.py $W/prof/pb_slim/interceptor/pprof/*_proc.pb.gz
"""
import argparse
import collections
import os
import re
import subprocess
import sys

SEPARATOR = '-----------+-------------------------------------------------------'


def traces(profiles):
    """Yield (seconds, frames) per sample stack, frames from the leaf down to the goroutine root."""
    out = subprocess.run(['go', 'tool', 'pprof', '-traces', '-focus=runtime\\.newstack', *profiles],
                         capture_output=True, text=True, check=True).stdout
    for block in out.split(SEPARATOR):
        lines = [l.strip() for l in block.strip('\n').split('\n') if l.strip()]
        if not lines:
            continue
        m = re.match(r'([\d.]+)(us|ms|s)\s+(.*)', lines[0])
        if not m:
            continue
        seconds = float(m.group(1)) * {'us': 1e-6, 'ms': 1e-3, 's': 1.0}[m.group(2)]
        yield seconds, [m.group(3)] + lines[1:]


def short(frame):
    return re.sub(r'^.*/', '', frame)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('profiles', nargs='+')
    ap.add_argument('--top', type=int, default=12)
    ap.add_argument('--chains', action='store_true')
    args = ap.parse_args()
    profiles = [p for p in args.profiles if not p.endswith('.allocs.pb.gz')]

    total_out = subprocess.run(['go', 'tool', 'pprof', '-top', '-nodecount=1', *profiles],
                               capture_output=True, text=True, check=True).stdout
    m = re.search(r'Total samples = ([\d.]+)(ms|s)', total_out)
    total = float(m.group(1)) * (1e-3 if m.group(2) == 'ms' else 1.0) if m else float('nan')

    by_trigger, by_root, chains = collections.Counter(), collections.Counter(), collections.Counter()
    growth = 0.0
    for seconds, frames in traces(profiles):
        if 'runtime.newstack' not in frames:
            continue
        i = frames.index('runtime.newstack')
        trigger = frames[i + 1] if i + 1 < len(frames) else '?'
        growth += seconds
        by_trigger[trigger] += seconds
        by_root[frames[-1]] += seconds
        chains[(trigger, '  <-  '.join(short(f) for f in frames[i + 2:i + 8]))] += seconds

    print(f'stack growth: {growth:.2f} s of {total:.2f} s sampled ({growth / total:.1%})')
    print(f'\nby the function that crossed the limit:')
    for f, t in by_trigger.most_common(args.top):
        print(f'  {t:7.2f} s  {f}')
    print(f'\nby goroutine (bottom frame):')
    for f, t in by_root.most_common(args.top):
        print(f'  {t:7.2f} s  {f}')
    if args.chains:
        print(f'\ncall chains below the top functions:')
        top = {f for f, _ in by_trigger.most_common(5)}
        for (f, chain), t in chains.most_common():
            if f in top and t >= 0.05:
                print(f'  {t:7.2f} s  {short(f)}  <-  {chain}')


if __name__ == '__main__':
    sys.exit(main())
