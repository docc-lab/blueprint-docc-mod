#!/usr/bin/env python3
"""Tomislav-RetCtx: set up a single-rate PB run for CPU profiling.

One offered rate held long enough (120 s) to take a 30 s profile inside the
measurement window, with BRIDGES_PPROF enabled on the application pods. Reverse
on and reverse off are otherwise identical, so a profile diff attributes the
response path's application CPU.

Usage: prof_setup.py <on|off>   -> prints the derived root
"""
import json
import subprocess
import sys
from pathlib import Path

import yaml

REPO = Path('/users/tomislav/blueprint-docc-mod')
SCRATCH = Path(__file__).resolve().parent
PROVENANCE = Path('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-n3-cpd2-6-inverse-20260916T160407Z')
RATE = 4000
SECONDS = 120
PPROF_PORT = ':6060'


def main():
    reverse = sys.argv[1]
    assert reverse in ('on', 'off'), reverse
    tag = f'-{sys.argv[2]}' if len(sys.argv) > 2 else ''
    source = Path((SCRATCH / 'prof_src_root').read_text().strip())
    stamp = source.name.rsplit('-', 1)[1]
    root = Path(f'/users/tomislav/deployments/dsb-sn/retctx-prof-{reverse}{tag}-{stamp}')
    if root.exists():
        sys.exit(f'{root} already exists')

    subprocess.run(['sg', 'docker', '-c', ' '.join([
        f'cd {REPO} &&', '.venv/bin/python', '-B', '-u', 'utils/derive_dsb_sn_nw.py',
        '--source', str(source), '--out', str(root), '--provenance-from', str(PROVENANCE),
        '--kinds', 'pb', '--repetitions', '1', '--collector', 'admission', '--reverse', reverse,
        '--note', f'"CPU-profiling run: single rate {RATE}, {SECONDS}s, BRIDGES_PPROF on the app pods; '
                  f'reverse {reverse}. Not an evaluation campaign."'])], check=True)

    # One rate, held long enough to profile inside the measurement window.
    plan = json.loads((root / 'plan.json').read_text())
    plan['ramp_rates'] = [RATE]
    plan['seconds_per_rate'] = SECONDS
    plan['profiling_note'] = (f'single-rate profiling run; ramp_rates and seconds_per_rate overridden '
                              f'to [{RATE}] and {SECONDS}s, BRIDGES_PPROF={PPROF_PORT} on application pods')
    (root / 'plan.json').write_text(json.dumps(plan, indent=1) + '\n')

    # Enable the debug listener on the application services only. The collector and
    # backends are untouched, and the variable is absent from any evaluation run.
    manifest = root / 'builds' / 'pb' / 'manifest.yaml'
    documents = list(yaml.safe_load_all(manifest.read_text()))
    patched = 0
    for doc in documents:
        if not doc or doc.get('kind') != 'Deployment':
            continue
        name = doc['metadata']['name']
        if '-service-' not in name or name.startswith('otelcol-'):
            continue
        for container in doc['spec']['template']['spec']['containers']:
            env = container.setdefault('env', [])
            if any(e['name'] == 'BRIDGES_PPROF' for e in env):
                continue
            env.append({'name': 'BRIDGES_PPROF', 'value': PPROF_PORT})
            patched += 1
    assert patched, 'no application containers patched'
    manifest.write_text(yaml.safe_dump_all(documents, sort_keys=False))
    # The runner verifies the manifest against the hash recorded at derive time, so
    # re-record it. Rewriting the hash is only safe because this script is the thing
    # that changed the manifest; the check still catches anything else.
    import hashlib
    complete = root / 'builds' / 'pb' / 'build-complete.json'
    payload = json.loads(complete.read_text())
    payload['manifest_sha256'] = hashlib.sha256(manifest.read_bytes()).hexdigest()
    payload['manifest_note'] = 'BRIDGES_PPROF added to application containers by prof_setup.py'
    complete.write_text(json.dumps(payload, indent=1) + '\n')
    print(f'patched BRIDGES_PPROF into {patched} application containers', file=sys.stderr)
    print(root)


if __name__ == '__main__':
    main()
