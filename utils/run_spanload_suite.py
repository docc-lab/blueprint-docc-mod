#!/usr/bin/env python3
"""Tomislav-RetCtx: repeated complete rate grids with one memory-protected collector."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time

import yaml

from run_spanload_ramp import REPO, command, cpu_state, sha256, write_json


RATES = {'zero': list(range(100000, 1600001, 100000)),
         'semconv10-example': list(range(10000, 200001, 10000))}
VARIANTS = ['none', 'pb', 'cgpb', 'sb']
GENERATOR_CPUS = ['4-7', '14-17', '8-11', '0,1,18,19']
IMAGE = 'spanload-collector:ramp-20260914'


def now():
    return datetime.now(timezone.utc).isoformat()


def plan(root, repetitions):
    stages = []
    # Same batch size and GOMEMLIMIT in both controls: only the processor differs.
    for profile in RATES:
        for variant in ('none', 'sb'):
            for limiter in (False, True):
                name = f'control-{profile}-{variant}-limiter-{int(limiter)}'
                stages.append(dict(name=name, kind='control', profile=profile, variant=variant,
                    repetition=0, limiter=limiter, rates=[RATES[profile][-1]], out=str(root / 'controls' / name)))
    for repetition in range(1, repetitions + 1):
        profiles = list(RATES) if repetition % 2 else list(reversed(RATES))
        shift = (repetition - 1) % len(VARIANTS)
        order = VARIANTS[shift:] + VARIANTS[:shift]
        for profile in profiles:
            for variant in order:
                name = f'repeat-{repetition:02d}-{profile}-{variant}'
                stages.append(dict(name=name, kind='ramp', profile=profile, variant=variant,
                    repetition=repetition, limiter=True, rates=RATES[profile], out=str(root / 'ramps' / name)))
    return stages


def stage_command(stage, root, seconds):
    argv = [sys.executable, str(REPO / 'utils/run_spanload_ramp.py'), '--out', stage['out'],
            '--collector-image', IMAGE, '--collector-config', str(root / ('collector-realistic.yaml' if stage['limiter'] else 'collector-without-limiter.yaml')),
            '--collector-gomemlimit', '2400MiB', '--profile', stage['profile'], '--variants', stage['variant'],
            '--rates', ','.join(map(str, stage['rates'])), '--seconds', str(seconds),
            '--discard-first', '10', '--min-steady-seconds', '30', '--complete-grid', '--export-format', 'proto']
    for cpus in GENERATOR_CPUS:
        argv += ['--generator-cpus', cpus]
    return argv


def collect_stage(stage):
    root = Path(stage['out'])
    rows = json.loads((root / 'results.json').read_text())
    assert [row['target_spans_per_second'] for row in rows] == stage['rates']
    completion = json.loads((root / stage['variant'] / 'completion.json').read_text())
    assert completion['complete_grid'] and completion['steps'] == len(stage['rates'])
    assert all(row['counts_reconciled'] and row['steady_seconds'] >= 30 for row in rows)
    assert all(row['metric_scrape_errors'] == 0 for row in rows)
    return [{**row, 'repetition': stage['repetition'], 'memory_limiter': stage['limiter'],
             'kind': stage['kind'], 'stage': stage['name']} for row in rows]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--repetitions', type=int, default=3)
    parser.add_argument('--seconds', type=int, default=45)
    parser.add_argument('--resume', action='store_true')
    args = parser.parse_args()
    if args.repetitions < 1 or args.seconds < 44:
        parser.error('use positive repetitions and at least 44 seconds per point')
    root = args.out.resolve()
    if args.resume:
        manifest = json.loads((root / 'manifest.json').read_text())
        assert manifest['seconds'] == args.seconds and manifest['repetitions'] == args.repetitions
        assert sha256(REPO / 'utils/run_spanload_ramp.py') == manifest['ramp_source_sha256']
        assert sha256(REPO / 'utils/spanload/bin/spanload') == manifest['generator_binary_sha256']
        stages = manifest['stages']
    else:
        root.mkdir(parents=True)
        (root / 'logs').mkdir()
        shutil.copy2(REPO / 'utils/spanload/collector-realistic.yaml', root / 'collector-realistic.yaml')
        plain = yaml.safe_load((root / 'collector-realistic.yaml').read_text())
        del plain['processors']['memory_limiter']
        plain['service']['pipelines']['traces']['processors'].remove('memory_limiter')
        (root / 'collector-without-limiter.yaml').write_text(yaml.safe_dump(plain, sort_keys=False))
        shutil.copy2(__file__, root / 'run_spanload_suite.py')
        shutil.copy2(REPO / 'utils/run_spanload_ramp.py', root / 'run_spanload_ramp.py')
        shutil.copytree(REPO / 'utils/spanload', root / 'generator-source',
                        ignore=shutil.ignore_patterns('bin', 'runs', '__pycache__'))
        stages = plan(root, args.repetitions)
        manifest = {'annotation': 'Tomislav-RetCtx: complete rate grids, three repeated ramps, separate memory-limiter controls.',
            'created': now(), 'seconds': args.seconds, 'repetitions': args.repetitions,
            'ramp_source_sha256': sha256(REPO / 'utils/run_spanload_ramp.py'),
            'generator_binary_sha256': sha256(REPO / 'utils/spanload/bin/spanload'),
            'collector_image': json.loads(command(['docker', 'image', 'inspect', IMAGE]))[0],
            'cpu_topology': command(['lscpu', '-e=CPU,CORE,SOCKET,NODE']), 'cpu_frequency': cpu_state(),
            'generator_cpu_sets': GENERATOR_CPUS, 'collector_cpu': 2,
            'collector_gomemlimit': '2400MiB', 'min_steady_seconds': 30, 'discard_first': 10,
            'stages': stages, 'planned_ramp_points': sum(len(s['rates']) for s in stages if s['kind'] == 'ramp'),
            'planned_control_points': sum(len(s['rates']) for s in stages if s['kind'] == 'control')}
        write_json(root / 'manifest.json', manifest)
    rows = []
    child = None
    status = {'pid': os.getpid(), 'started': now(), 'planned_stages': len(stages),
              'planned_points': sum(len(s['rates']) for s in stages), 'finished_stages': 0, 'finished_points': 0}
    try:
        for index, stage in enumerate(stages):
            if (Path(stage['out']) / 'stage-success.json').exists():
                rows.extend(collect_stage(stage))
                continue
            status.update(stage=stage, finished_stages=index, finished_points=len(rows), updated=now(), state='running')
            write_json(root / 'status.json', status)
            argv = stage_command(stage, root, args.seconds)
            write_json(root / 'logs' / (stage['name'] + '-command.json'), argv)
            print(f"{now()} starting {stage['name']} ({len(stage['rates'])} points)", flush=True)
            with (root / 'logs' / (stage['name'] + '.log')).open('w') as stream:
                child = subprocess.Popen(argv, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
                status.update(child_pid=child.pid)
                write_json(root / 'status.json', status)
                while child.poll() is None:
                    time.sleep(1)
                if child.returncode != 0:
                    raise RuntimeError(f"stage {stage['name']} exited {child.returncode}; see its log")
                child = None
            stage_rows = collect_stage(stage)
            write_json(Path(stage['out']) / 'stage-success.json', {'finished': now(), 'points': len(stage_rows)})
            rows.extend(stage_rows)
            write_json(root / 'results.json', rows)
            print(f"{now()} finished {stage['name']}; {len(rows)}/{status['planned_points']} points", flush=True)
        status.update(state='complete', finished_stages=len(stages), finished_points=len(rows), updated=now(), child_pid=None)
        write_json(root / 'results.json', rows)
        write_json(root / 'completion.json', {'finished': now(), 'points': len(rows), 'all_stages_complete': True})
    except BaseException as error:
        status.update(state='interrupted' if isinstance(error, KeyboardInterrupt) else 'failed', error=str(error), updated=now())
        raise
    finally:
        if child and child.poll() is None:
            os.killpg(child.pid, signal.SIGINT)
            try:
                child.wait(timeout=30)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
        write_json(root / 'status.json', status)


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, signal.default_int_handler)
    main()
