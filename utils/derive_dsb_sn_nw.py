#!/usr/bin/env python3
"""Tomislav-RetCtx: derive a no-work root from a completed one, reusing its pinned images.

Copies the selected case directories (pinned manifests, image digests), applies the
current BACKEND_TUNING to the Jaeger/Elasticsearch Deployments, re-hashes the
manifests, writes a reduced plan (cases, repetitions) and links raw data under
/storage. Smoke and run still execute normally on the new root.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

import yaml

from prepare_dsb_sn_e2e import write_json, REPO
from prepare_dsb_sn_nw import (BACKEND_TUNING, COLLECTOR_PROFILES, collector_config, set_reverse,
                               tune_backend)
from prepare_dsb_sn_e2e import environment
from run_dsb_sn_e2e import now

STORAGE = Path('/storage/tomislav-retctx-e2e')
COLLECTOR_PIPELINE_NOTE = {
    'passthrough': 'passthrough: OTLP/gRPC -> batch(8192, 200ms) -> OTLP -> Jaeger -> Elasticsearch; '
                   'no memory_limiter, no priority processor (collectors must not shed)',
    'admission': 'admission control as in the real-work e2e run: OTLP/gRPC -> priority (bridges, soft 50 / '
                 'hard 70, cp_safety_factor 1, force_gc) or memory_limiter (vanilla, no-tracing; 70/20) '
                 '-> batch(8192, 200ms) -> OTLP -> Jaeger -> Elasticsearch',
}
COLLECTOR_PIPELINE_NOTE['admission2g'] = (COLLECTOR_PIPELINE_NOTE['admission'] +
                                          '; collectors 1 CPU / 2 GiB so shedding starts near the knee')
COLLECTOR_PIPELINE_NOTE['sink'] = ('sink: OTLP/gRPC -> batch(8192, 200ms) -> nop. Every span accepted and '
                                   'discarded; no store, no refusals, no backpressure. Isolates instrumentation cost.')


def storage_dirs(root):
    raw = STORAGE / root.name
    for name in ('run', 'smoke'):
        target = raw / name
        if not target.exists():
            try:
                target.mkdir(parents=True)
            except PermissionError:
                subprocess.run(['sudo', '-n', 'mkdir', '-p', str(target)], check=True)
                subprocess.run(['sudo', '-n', 'chown', '-R', f'{os.getuid()}:{os.getgid()}', str(raw)], check=True)
        link = root / name
        if not link.exists():
            link.symlink_to(target)
    return raw


def retune_collectors(documents, kind, variant, profile):
    """Replace the collector ConfigMap and DaemonSet resources for a different profile."""
    spec = COLLECTOR_PROFILES[profile]
    config = collector_config(kind, variant, profile)
    touched = []
    for doc in documents:
        if not doc:
            continue
        name = doc['metadata']['name']
        if doc.get('kind') == 'ConfigMap' and name == f'otelcol-{variant}-config':
            doc['data']['config.yaml'] = yaml.safe_dump(config, sort_keys=False)
            touched.append(name)
        elif doc.get('kind') == 'DaemonSet' and name.startswith('otelcol-'):
            for container in doc['spec']['template']['spec']['containers']:
                container['resources'] = {key: dict(spec['resources']) for key in ('requests', 'limits')}
                environment(container, spec['env'])
            touched.append(name)
    assert len(touched) == 2, touched
    return config


def derive(source, root, kinds, repetitions, note, profile='passthrough', reverse='on',
           provenance=None):
    # Tomislav-RetCtx: the usual source is a COMPLETED campaign, whose pinned digests are
    # reused without rebuilding. A freshly prepared+built root has never run, so there is no
    # run-complete.json to check; its own build status is the equivalent guarantee, and the
    # application-source hashes and monitor come from the campaign being compared against
    # (`provenance`). Carrying those hashes over is the point rather than a convenience: they
    # are re-verified below, so the derive fails unless the application service sources are
    # byte-identical to that campaign's.
    if provenance is None:
        assert json.loads((source / 'run-complete.json').read_text())['passed']
        provenance = source
    else:
        assert json.loads((source / 'prepare-status.json').read_text())['state'] == 'complete'
        assert json.loads((source / 'image-build-status.json').read_text())['state'] == 'complete'
    root.mkdir(parents=True)
    (root / 'logs').mkdir()
    raw = storage_dirs(root)
    shutil.copy2(provenance / 'monitor_nw.py', root)
    previous = json.loads((provenance / 'application-source-hashes.json').read_text())
    # The tracked set spans BOTH the application services (examples/dsb_sn) and the
    # instrumentation (runtime/, plugins/). Only the first must be identical -- that is the
    # standing rule that the application is never modified to suit a campaign. Instrumentation
    # is expected to change between builds, so its new hashes are recorded and the files that
    # moved are named in the plan, which is what makes two campaigns comparable on the record
    # rather than on trust. Files added since the source campaign are picked up too, so a new
    # instrumentation file cannot slip in unrecorded.
    tracked = set(previous)
    for name in list(previous):
        for sibling in sorted((REPO / name).parent.glob('*.go')):
            relative = str(sibling.relative_to(REPO))
            if not relative.endswith('_test.go'):
                tracked.add(relative)
    hashes, changed, added = {}, [], []
    for name in sorted(tracked):
        hashes[name] = hashlib.sha256((REPO / name).read_bytes()).hexdigest()
        if name not in previous:
            added.append(name)
        elif hashes[name] != previous[name]:
            assert not name.startswith('examples/'), f'application source changed since {provenance}: {name}'
            changed.append(name)
    write_json(root / 'application-source-hashes.json', hashes)
    write_json(root / 'source-provenance.json', {
        'compared_with': str(provenance), 'application_files_identical': True,
        'instrumentation_changed': changed, 'instrumentation_added': added})
    cases = []
    for entry in json.loads((source / 'cases.json').read_text()):
        if entry['kind'] not in kinds or entry['sample_ratio'] != 1:
            continue
        case = root / 'builds' / entry['name']
        shutil.copytree(entry['case'], case)
        manifest = case / 'manifest.yaml'
        documents = list(yaml.safe_load_all(manifest.read_text()))
        tuned = [d['metadata']['name'] for d in documents if d and tune_backend(d)]
        assert len(tuned) == 2, tuned
        config = retune_collectors(documents, entry['kind'], entry['variant'], profile)
        services = sum(1 for doc in documents if doc and set_reverse(doc, reverse))
        assert entry['kind'] == 'nt' or services == 13, (entry['kind'], services)
        (case / 'collector.yaml').write_text(yaml.safe_dump(config, sort_keys=False))
        manifest.write_text(yaml.safe_dump_all(documents, sort_keys=False))
        complete = json.loads((case / 'build-complete.json').read_text())
        complete['manifest_sha256'] = hashlib.sha256(manifest.read_bytes()).hexdigest()
        complete['derived_from'] = entry['case']
        write_json(case / 'build-complete.json', complete)
        entry = dict(entry, case=str(case), image_case=str(case), backend_tuning=copy.deepcopy(BACKEND_TUNING),
                     collector_profile=profile, reverse_truss=reverse, derived_from=entry['case'])
        write_json(case / 'case.json', entry)
        cases.append(entry)
    assert cases, kinds
    write_json(root / 'cases.json', cases)
    # A freshly built source has no plan of its own; the ramp grid, seeds and workload
    # settings come from the campaign this run is being compared against, which is what
    # keeps the two directly comparable.
    plan = json.loads(((source if provenance is source else provenance) / 'plan.json').read_text())
    plan.pop('repetition_1_reuse', None)
    plan.update(cases=[c['name'] for c in cases], repetitions=repetitions, seeds=plan['seeds'][:repetitions],
                collector_profile=profile, collector_pipeline=COLLECTOR_PIPELINE_NOTE[profile],
                reverse_truss=reverse,
                reverse_note=('response path on: reverse truss propagates, unscheduled leaves may be rejected'
                              if reverse == 'on' else
                              'response path OFF: no reverse propagation, no leaf rejection; every childless '
                              'server span is a checkpoint, plus the CPD schedule (unchanged)'),
                collector_cpu=COLLECTOR_PROFILES[profile]['resources']['cpu'],
                collector_memory=COLLECTOR_PROFILES[profile]['resources']['memory'],
                collector_gomemlimit=COLLECTOR_PROFILES[profile]['env']['GOMEMLIMIT'],
                created=now(), derived_from=str(source), backend_tuning=copy.deepcopy(BACKEND_TUNING),
                raw_data_storage=str(raw), note=note,
                jaeger_cpus=BACKEND_TUNING['jaeger_cpus'], elasticsearch_cpus=BACKEND_TUNING['elasticsearch_cpus'],
                case_order_note='single kind; no rotation')
    write_json(root / 'plan.json', plan)
    write_json(root / 'prepare-status.json', {'state': 'complete', 'cases': len(cases), 'derived_from': str(source)})
    write_json(root / 'image-build-status.json', {'state': 'complete', 'reused': True,
                                                  'app_images': 13 * len(cases), 'note': 'pinned digests from ' + str(source)})
    return cases


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--kinds', nargs='+', required=True)
    parser.add_argument('--repetitions', type=int, default=1)
    parser.add_argument('--note', default='')
    parser.add_argument('--collector', choices=sorted(COLLECTOR_PROFILES), default='passthrough')
    parser.add_argument('--provenance-from', type=Path, default=None, metavar='ROOT',
                        help='derive from a freshly prepared+built source that has never run, taking '
                             'the application-source hashes and monitor from this completed campaign. '
                             'The hashes are re-verified against the working tree, so the application '
                             'services must be byte-identical to that campaign.')
    parser.add_argument('--reverse', choices=('on', 'off'), default='on',
                        help="'off' disables response-path propagation: leaves are always checkpointed")
    args = parser.parse_args()
    for case in derive(args.source.resolve(), args.out.resolve(), set(args.kinds), args.repetitions,
                       args.note, args.collector, args.reverse,
                       args.provenance_from.resolve() if args.provenance_from else None):
        print(case['name'], case['case'])
