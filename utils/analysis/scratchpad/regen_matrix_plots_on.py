#!/usr/bin/env python3
"""Tomislav-RetCtx: n=5 no-work ramp figures from the RESPONSE-PATH-ON rounds only, with the
bridges labelled plainly (PB / CGPB / SB): the paper's default configuration. The on-vs-off
comparison is left for the ablation figure (regen_matrix_plots.py).

  nw-ramp-n<N>   None, Vanilla, PB, CGPB, SB
"""
import argparse, re, subprocess, sys
from pathlib import Path
SCRATCH = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod'); PY = REPO / '.venv/bin/python'
PLOT = REPO / 'utils/plot_dsb_sn_latency_throughput.py'; ANALYZE = REPO / 'utils/analyze_dsb_sn_nw.py'
LABEL = {'pb': 'PB', 'cgpb': 'CGPB', 'sb': 'SB'}; COLOR = {'PB': '#2878B5', 'CGPB': '#D36B23', 'SB': '#33945B'}

def on_rounds():
    found = {}
    for line in (SCRATCH / 'matrix_roots.txt').read_text().split():
        root = Path(line); m = re.search(r'-m5-(on|off)-r(\d+)-', root.name)
        if m and m.group(1) == 'on' and (root / 'run-complete.json').exists():
            found[int(m.group(2))] = root
    return dict(sorted(found.items()))

def analyse(root):
    if not (root / 'analysis' / 'points.json').exists():
        subprocess.run([str(PY), '-B', str(ANALYZE), '--out', str(root)], cwd=REPO, check=True, capture_output=True)

def kinds_in(root): return sorted(p.name[3:] for p in (root / 'run').iterdir() if p.is_dir())

def plot(name, figures, primary, extras, ylim, height=None, legend_cols=0, spread='band'):
    argv = [str(PY), '-B', str(PLOT), '--out', str(primary), '--figures', str(figures), '--name', name, '--ylim', ylim,
            '--spread', spread, '--exclude', 'pb', '--exclude', 'cgpb', '--exclude', 'sb']
    for label, color in COLOR.items(): argv += ['--color', f'{label}={color}']
    if height: argv += ['--height', str(height)]
    if legend_cols: argv += ['--legend-cols', str(legend_cols)]
    for spec in extras: argv += ['--extra', spec]
    out = subprocess.run(argv, cwd=REPO, capture_output=True, text=True)
    if out.returncode: print(out.stdout + out.stderr, file=sys.stderr); raise SystemExit(f'{name} failed')
    print(f'  {name}:'); [print(f'    {l}') for l in out.stdout.strip().splitlines()]

def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--figures', type=Path, default=Path('/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-23'))
    args = ap.parse_args()
    rounds = on_rounds(); n = len(rounds); print(f'ON rounds: {sorted(rounds)} -> n={n}')
    for root in rounds.values(): analyse(root)
    first = rounds[sorted(rounds)[0]]
    head, bridges = [], []
    for r in sorted(rounds):
        root = rounds[r]
        if r != sorted(rounds)[0]: head += [f'nt={root}:nt', f'v={root}:v']; bridges += [f'nt={root}:nt', f'v={root}:v']
        for kind in ('pb', 'cgpb', 'sb'):
            if kind in kinds_in(root):
                spec = f'{LABEL[kind]}={root}:{kind}'; bridges.append(spec)
                if kind == 'pb': head.append(spec)
    plot(f'nw-ramp-n{n}', args.figures, first, bridges, '150,300', height=1.4, legend_cols=5)
    plot(f'nw-ramp-n{n}-errorbars', args.figures, first, bridges, '150,300', height=1.4, legend_cols=5, spread='errorbars')

if __name__ == '__main__': main()
