#!/usr/bin/env python3
"""Tomislav-RetCtx: regenerate the matrix figures at whatever n is complete.

A round is complete when both its arms have run: the ON arm (nt, v, pb, cgpb, sb)
and the OFF arm (pb, cgpb, sb). Only complete rounds are plotted, so the figure is
always a balanced set rather than whichever arm happened to finish last.

Rounds live in separate campaign roots, so each round is added as a labelled extra
under the SAME series name; the plot script accumulates them into one curve whose
band is the min-max across rounds.

Two figures per n:
  optsdk-latency-vs-throughput-n<N>   None, Vanilla, PB off, PB on  (the headline)
  optsdk-bridges-n<N>                 the three bridges, on and off

Usage: regen_matrix_plots.py [--figures DIR]
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

SCRATCH = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
PY = REPO / '.venv/bin/python'
PLOT = REPO / 'utils/plot_dsb_sn_latency_throughput.py'
ANALYZE = REPO / 'utils/analyze_dsb_sn_nw.py'

OFF_LABELS = {'pb': 'PB path off', 'cgpb': 'CGPB path off', 'sb': 'SB path off'}
ON_LABELS = {'pb': 'PB path on', 'cgpb': 'CGPB path on', 'sb': 'SB path on'}


def rounds():
    """-> {round: {'on': root, 'off': root}} for rounds whose runs have completed."""
    found = {}
    listing = SCRATCH / 'matrix_roots.txt'
    if not listing.exists():
        return found
    for line in listing.read_text().split():
        root = Path(line)
        m = re.search(r'-m5-(on|off)-r(\d+)-', root.name)
        if not m or not (root / 'run-complete.json').exists():
            continue
        try:
            if not json.loads((root / 'run-complete.json').read_text())['passed']:
                continue
        except Exception:
            continue
        found.setdefault(int(m.group(2)), {})[m.group(1)] = root
    return {r: arms for r, arms in sorted(found.items()) if 'on' in arms and 'off' in arms}


def analyse(root):
    if (root / 'analysis' / 'points.json').exists():
        return
    subprocess.run([str(PY), '-B', str(ANALYZE), '--out', str(root)],
                   cwd=REPO, check=True, capture_output=True)


def kinds_in(root):
    return sorted(p.name[3:] for p in (root / 'run').iterdir() if p.is_dir())


def plot(name, figures, primary, extras, ylim):
    argv = [str(PY), '-B', str(PLOT), '--out', str(primary), '--figures', str(figures),
            '--name', name, '--ylim', ylim,
            # The primary also supplies these under a label; without this they draw twice.
            '--exclude', 'pb', '--exclude', 'cgpb', '--exclude', 'sb']
    for spec in extras:
        argv += ['--extra', spec]
    out = subprocess.run(argv, cwd=REPO, capture_output=True, text=True)
    if out.returncode:
        print(out.stdout + out.stderr, file=sys.stderr)
        raise SystemExit(f'{name} failed')
    print(f'  {name}:')
    for line in out.stdout.strip().splitlines():
        print(f'    {line}')


# Tomislav-RetCtx: 150/300 rather than a shared ceiling. p99/mean has a median
# ratio of 2.13 across all pre-wall points, so a 2:1 pair of ceilings makes both
# panels clip at the same place in the ramp (~offered 7000-7500, where the walls
# start). One shared ceiling would either waste half the mean panel or clip p99
# far earlier than mean.


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--figures', type=Path,
                    default=Path('/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-20'))
    args = ap.parse_args()

    complete = rounds()
    if not complete:
        raise SystemExit('no complete rounds yet')
    n = len(complete)
    print(f'complete rounds: {sorted(complete)} -> n={n}')
    for arms in complete.values():
        for root in arms.values():
            analyse(root)

    first = complete[sorted(complete)[0]]['on']
    # Primary supplies nt/v under their own names; every bridge series is labelled so
    # that "on" and "off" -- two roots of the SAME kind -- can share the figure.
    head_extras, bridge_extras = [], []
    for r in sorted(complete):
        on, off = complete[r]['on'], complete[r]['off']
        if r != sorted(complete)[0]:
            head_extras += [f'nt={on}:nt', f'v={on}:v']
        for kind in ('pb', 'cgpb', 'sb'):
            if kind in kinds_in(on):
                spec_on = f'{ON_LABELS[kind]}={on}:{kind}'
                bridge_extras.append(spec_on)
                if kind == 'pb':
                    head_extras.append(spec_on)
            if kind in kinds_in(off):
                spec_off = f'{OFF_LABELS[kind]}={off}:{kind}'
                bridge_extras.append(spec_off)
                if kind == 'pb':
                    head_extras.append(spec_off)

    plot(f'optsdk-latency-vs-throughput-n{n}', args.figures, first, head_extras, '150,300')
    plot(f'optsdk-bridges-n{n}', args.figures, first, bridge_extras, '150,300')


if __name__ == '__main__':
    main()
