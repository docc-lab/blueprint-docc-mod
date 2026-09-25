#!/usr/bin/env python3
"""Tomislav-RetCtx: all eight configurations, mean and p99, both x axes.

Two views of the same completed rounds:
  offered   -- response time against OFFERED rate (plot_dsb_sn_e2e_drops.py)
  achieved  -- response time against ACHIEVED throughput (plot_dsb_sn_latency_throughput.py)

The offered view shows where each variant's knee sits on the load the generator
applied; the achieved view ends each curve at that variant's capacity, so the
ceiling is read straight off the x axis. Neither subsumes the other.

Series: no tracing, vanilla, and each bridge with the response path on and off.
Rounds live in separate campaign roots, so every round is passed as its own
--extra under the same label and the scripts accumulate them into one curve.

Usage: plot_all_series.py [--figures DIR]
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
ACHIEVED = REPO / 'utils/plot_dsb_sn_latency_throughput.py'
OFFERED = REPO / 'utils/plot_dsb_sn_e2e_drops.py'
ANALYZE = REPO / 'utils/analyze_dsb_sn_nw.py'
BRIDGES = ('pb', 'cgpb', 'sb')
# One colour per bridge, matching every other figure in the set; the response path
# on/off is carried by line style so the reader tracks a bridge across both modes.
BRIDGE_COLOR = {'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
LABEL = {('pb', 'on'): 'PB on', ('pb', 'off'): 'PB off',
         ('cgpb', 'on'): 'CGPB on', ('cgpb', 'off'): 'CGPB off',
         ('sb', 'on'): 'SB on', ('sb', 'off'): 'SB off'}


def complete_rounds():
    found = {}
    listing = SCRATCH / 'matrix_roots.txt'
    for line in listing.read_text().split():
        root = Path(line)
        m = re.search(r'-m5-(on|off)-r(\d+)-', root.name)
        if not m or not (root / 'run-complete.json').exists():
            continue
        if json.loads((root / 'run-complete.json').read_text()).get('passed'):
            found.setdefault(int(m.group(2)), {})[m.group(1)] = root
    return {r: a for r, a in sorted(found.items()) if {'on', 'off'} <= set(a)}


def run(argv, what):
    out = subprocess.run([str(x) for x in argv], cwd=REPO, capture_output=True, text=True)
    if out.returncode:
        print(out.stdout + out.stderr, file=sys.stderr)
        raise SystemExit(f'{what} failed')
    return out.stdout


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
    rounds = complete_rounds()
    if not rounds:
        raise SystemExit('no complete rounds yet')
    n = len(rounds)
    print(f'complete rounds {sorted(rounds)} -> n={n}')

    first_on = rounds[sorted(rounds)[0]]['on']
    for arms in rounds.values():
        for root in arms.values():
            if not (root / 'analysis' / 'points.json').exists():
                run([PY, '-B', ANALYZE, '--out', root], f'analyse {root.name}')

    # ---- achieved throughput -------------------------------------------------
    extras = []
    for i, r in enumerate(sorted(rounds)):
        on, off = rounds[r]['on'], rounds[r]['off']
        if i:
            extras += [f'nt={on}:nt', f'v={on}:v']
        for kind in BRIDGES:
            extras.append(f'{LABEL[(kind, "on")]}={on}:{kind}')
            extras.append(f'{LABEL[(kind, "off")]}={off}:{kind}')
    style = []
    for kind in BRIDGES:
        for mode in ('on', 'off'):
            style += ['--color', f'{LABEL[(kind, mode)]}={BRIDGE_COLOR[kind]}']
        style += ['--dashed', LABEL[(kind, 'off')]]
    argv = [PY, '-B', ACHIEVED, '--out', first_on, '--figures', args.figures,
            '--name', f'all-achieved-n{n}', '--ylim', '150,300', '--legend-cols', 4,
            '--height', 1.9, '--exclude', 'pb', '--exclude', 'cgpb', '--exclude', 'sb'] + style
    for e in extras:
        argv += ['--extra', e]
    print(run(argv, 'achieved'))

    # ---- offered rate --------------------------------------------------------
    # Same script and the same points.json, just the other x axis. The drops script
    # would also do this, but it needs the collectors' priority logs for every point
    # and one point's before-snapshot has no priority line yet -- an irrelevant
    # dependency when all that is wanted here is mean and p99.
    argv = [PY, '-B', ACHIEVED, '--out', first_on, '--figures', args.figures,
            '--name', f'all-offered-n{n}', '--ylim', '150,300', '--legend-cols', 4,
            '--height', 1.9, '--x-axis', 'offered', '--xtick', 2,
            '--exclude', 'pb', '--exclude', 'cgpb', '--exclude', 'sb'] + style
    for e in extras:
        argv += ['--extra', e]
    print(run(argv, 'offered'))


if __name__ == '__main__':
    main()
