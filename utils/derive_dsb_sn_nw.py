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
from prepare_dsb_sn_nw import (BACKEND_TUNING, BACKENDS, BRIDGES, CLICKHOUSE, COLLECTOR_PROFILES, GATEWAY, STORE_TUNINGS,
                               collector_config, cpu_count,
                               install_clickhouse_backend,
                               set_reverse, tune_backend)
from prepare_dsb_sn_e2e import environment
from dsb_apps import APPS, app_name, set_cache_args  # Tomislav-RetCtx: per-application constants (default app 'sn')
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
COLLECTOR_PIPELINE_NOTE['admission500m'] = (COLLECTOR_PIPELINE_NOTE['admission'] +
                                            '; collectors 500m CPU / 256 Mi (half the CPU of admission)')
COLLECTOR_PIPELINE_NOTE['admission6040'] = (COLLECTOR_PIPELINE_NOTE['admission'].replace(
    'soft 50 / hard 70', 'soft 40 / hard 60').replace('70/20', '60/20') +
    '; thresholds lowered so every configuration sheds before its knee')
# Tomislav-RetCtx: fast exporter retry; see prepare_dsb_sn_nw.COLLECTOR_PROFILES['admission6040fr'].
COLLECTOR_PIPELINE_NOTE['admission6040fr'] = (COLLECTOR_PIPELINE_NOTE['admission6040'] +
                                              '; exporter retry_on_failure 200 ms initial / 1 s max (default 5 s / 30 s)')
# Tomislav-RetCtx: OpenTelemetry Helm-chart default memory percentages; see prepare_dsb_sn_nw.COLLECTOR_PROFILES.
COLLECTOR_PIPELINE_NOTE['admissionotel'] = (COLLECTOR_PIPELINE_NOTE['admission'].replace(
    'soft 50 / hard 70', 'soft 55 / hard 80 (OTel Helm defaults: limit 80, spike 25)').replace('70/20', '80/25'))
COLLECTOR_PIPELINE_NOTE['admissionotel500m'] = (COLLECTOR_PIPELINE_NOTE['admissionotel'] +
                                                '; collectors 500m CPU / 256 Mi (SN real-work agent budget)')
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


def retune_collectors(documents, kind, variant, profile, backend='jaeger', app='sn', priority_extra=None, gomaxprocs=None,
                      collector_image=None, priority_receiver=False, priority_queue=False, pprof=False,
                      compression=None):
    """Replace the collector ConfigMap and DaemonSet resources for a different profile.
    Tomislav-RetCtx: priority_extra / gomaxprocs ('auto' = the agent's CPU limit) / collector_image
    (a digest reference) are optional collector-behaviour switches; None leaves everything as before."""
    spec = COLLECTOR_PROFILES[profile]
    config = collector_config(kind, variant, profile, backend, app=app, priority_extra=priority_extra,
                              priority_receiver=priority_receiver, priority_queue=priority_queue, pprof=pprof,
                              compression=compression)
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
                if gomaxprocs == 'auto':
                    environment(container, {'GOMAXPROCS': cpu_count(spec['resources']['cpu'])})
                if collector_image:
                    assert '@sha256:' in collector_image, collector_image
                    container['image'] = collector_image
            touched.append(name)
    assert len(touched) == 2, touched
    return config


def derive(source, root, kinds, repetitions, note, profile='passthrough', reverse='on',
           provenance=None, rates=None, seconds_per_rate=None, generator=None, backend='jaeger',
           gateway_cpu=None, store='nowork', collector_image=None, gomaxprocs=None, us_margin_mode=None,
           gateway_lp_refusal=None, sdk_retry=None, sdk_retry_delay_ms=None, priority_receiver=False,
           cpu_shed_threshold=None, gateway_cpu_shed_threshold=None, cpu_target=None, gateway_cpu_target=None,
           priority_queue=False, gateway_priority_queue=False, pprof=False, agent_compression=None, app_pprof=False):
    assert backend in BACKENDS, backend
    # Tomislav-RetCtx: which Jaeger+ES sizing (prepare_dsb_sn_nw.STORE_TUNINGS): the no-work
    # matrix's tuned store, or the SN real-work e2e campaign's (user 2026-09-23 for hotel).
    # (clickhouse backend: no Jaeger/ES at all, so no store tuning is recorded)
    tuning = STORE_TUNINGS[store] if backend == 'jaeger' else None
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
        gateway = None
        if backend == 'jaeger':
            tuned = [d['metadata']['name'] for d in documents if d and tune_backend(d, tuning)]
            assert len(tuned) == 2, tuned
        app = app_name(entry)
        # Tomislav-RetCtx: memcached server arguments (dsb_apps cache_args; hotel only)
        caches = set_cache_args(documents, APPS[app])
        assert caches == (3 if app == 'hotel' else 0), (app, caches)
        # Tomislav-RetCtx: collector-behaviour switches (all None = unchanged). The agents' priority
        # processor gets us_margin_mode; the gateway's gets us_margin_mode and lp_refusal_code.
        agent_extra = dict({'us_margin_mode': us_margin_mode} if us_margin_mode else {},
                           **({'cpu_shed_threshold': cpu_shed_threshold} if cpu_shed_threshold else {}),
                           **({'cpu_target': cpu_target} if cpu_target else {})) or None
        gateway_extra = dict({'us_margin_mode': us_margin_mode} if us_margin_mode else {},
                             **({'lp_refusal_code': gateway_lp_refusal} if gateway_lp_refusal else {}),
                             **({'cpu_shed_threshold': gateway_cpu_shed_threshold} if gateway_cpu_shed_threshold else {}),
                             **({'cpu_target': gateway_cpu_target} if gateway_cpu_target else {}))
        config = retune_collectors(documents, entry['kind'], entry['variant'], profile, backend, app,
                                   priority_extra=agent_extra, gomaxprocs=gomaxprocs, collector_image=collector_image,
                                   priority_receiver=priority_receiver, priority_queue=priority_queue, pprof=pprof,
                                   compression=agent_compression)
        if backend == 'clickhouse':
            # Tomislav-RetCtx: Jaeger+Elasticsearch out, gateway collector + ClickHouse + shim in.
            namespace = next(d['metadata']['namespace'] for d in documents if d and d.get('kind') == 'DaemonSet')
            gateway = install_clickhouse_backend(documents, entry['kind'], entry['variant'], profile, namespace,
                                                 gateway_cpu=gateway_cpu, priority_extra=gateway_extra or None,
                                                 gomaxprocs=gomaxprocs, priority_receiver=priority_receiver,
                                                 priority_queue=gateway_priority_queue, pprof=pprof)
            (case / 'gateway.yaml').write_text(yaml.safe_dump(gateway, sort_keys=False))
        # Tomislav-RetCtx: response-path propagation only exists for the bridges.
        # run_dsb_sn_nw.verify_deployment expects REVERSE_TRUSS=off on every non-bridge
        # kind regardless of the campaign's setting, so applying the campaign value to
        # vanilla or no-tracing produces a deployment the runner refuses. Honour the
        # same rule here rather than emitting manifests that cannot be deployed.
        kind_reverse = reverse if entry['kind'] in BRIDGES else 'off'
        # Tomislav-RetCtx: SDK one-shot retry (runtime/plugins/otelcol/sdk_retry.go). 'priority' =
        # bridges retry refused checkpoint batches once and suppress LP meanwhile (BRIDGES_RETRY=hp),
        # vanilla retries every refused batch once (=all); no-tracing has no SDK.
        retry_mode = {'priority': 'hp' if entry['kind'] in BRIDGES else 'all'}.get(sdk_retry) if entry['kind'] != 'nt' else None
        if retry_mode:
            for doc in documents:
                if doc and doc.get('kind') == 'Deployment' and '-service-' in doc['metadata']['name']:
                    for container in doc['spec']['template']['spec']['containers']:
                        environment(container, dict({'BRIDGES_RETRY': retry_mode},
                                                    **({'BRIDGES_RETRY_DELAY_MS': sdk_retry_delay_ms} if sdk_retry_delay_ms else {})))
        # Tomislav-RetCtx: opt-in in-app CPU profiling (runtime/plugins/otelcol/pprof.go, BRIDGES_PPROF).
        if app_pprof:
            for doc in documents:
                if doc and doc.get('kind') == 'Deployment' and '-service-' in doc['metadata']['name']:
                    for container in doc['spec']['template']['spec']['containers']:
                        environment(container, {'BRIDGES_PPROF': ':6060'})
        services = sum(1 for doc in documents if doc and set_reverse(doc, kind_reverse))
        assert entry['kind'] == 'nt' or services == APPS[app]['app_services'], (entry['kind'], services)
        (case / 'collector.yaml').write_text(yaml.safe_dump(config, sort_keys=False))
        manifest.write_text(yaml.safe_dump_all(documents, sort_keys=False))
        complete = json.loads((case / 'build-complete.json').read_text())
        complete['manifest_sha256'] = hashlib.sha256(manifest.read_bytes()).hexdigest()
        complete['derived_from'] = entry['case']
        write_json(case / 'build-complete.json', complete)
        deployments = sum(1 for d in documents if d and d.get('kind') == 'Deployment')
        entry = dict(entry, case=str(case), image_case=str(case),
                     backend_tuning=copy.deepcopy(tuning) if backend == 'jaeger' else None, store=store if tuning else None,
                     cache_args=APPS[app].get('cache_args'),
                     backend=backend, expected_pods=deployments + 8, gateway_cpu=gateway_cpu, app=app,
                     collector_image_override=collector_image, collector_gomaxprocs=gomaxprocs,
                     us_margin_mode=us_margin_mode, gateway_lp_refusal=gateway_lp_refusal,
                     sdk_retry_mode=retry_mode, sdk_retry_delay_ms=sdk_retry_delay_ms,
                     priority_receiver=priority_receiver, cpu_shed_threshold=cpu_shed_threshold,
                     gateway_cpu_shed_threshold=gateway_cpu_shed_threshold,
                     cpu_target=cpu_target, gateway_cpu_target=gateway_cpu_target, priority_queue=priority_queue,
                     gateway_priority_queue=gateway_priority_queue, pprof=pprof, agent_compression=agent_compression,
                     app_pprof=app_pprof, collector_profile=profile, reverse_truss=reverse, derived_from=entry['case'])
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
                collector_profile=profile, collector_pipeline=(COLLECTOR_PIPELINE_NOTE[profile] if backend == 'jaeger' else
                    COLLECTOR_PIPELINE_NOTE[profile].replace('-> OTLP -> Jaeger -> Elasticsearch',
                    '-> OTLP -> node-9 gateway collector (same admission processor) -> ClickHouse')),
                reverse_truss=reverse,
                reverse_note=('response path on: reverse truss propagates, unscheduled leaves may be rejected'
                              if reverse == 'on' else
                              'response path OFF: no reverse propagation, no leaf rejection; every childless '
                              'server span is a checkpoint, plus the CPD schedule (unchanged)'),
                collector_cpu=COLLECTOR_PROFILES[profile]['resources']['cpu'],
                collector_memory=COLLECTOR_PROFILES[profile]['resources']['memory'],
                collector_gomemlimit=COLLECTOR_PROFILES[profile]['env']['GOMEMLIMIT'],
                created=now(), derived_from=str(source), backend_tuning=copy.deepcopy(tuning), store=store if tuning else None,
                cache_args=APPS[app_name(cases[0])].get('cache_args'),
                raw_data_storage=str(raw), note=note,
                jaeger_cpus=tuning['jaeger_cpus'] if tuning else None,
                elasticsearch_cpus=tuning['elasticsearch_cpus'] if tuning else None,
                clickhouse=({'server_cpus': CLICKHOUSE['cpus'], 'server_memory': CLICKHOUSE['memory'],
                             'gateway_resources': dict(GATEWAY['resources'], **({'cpu': str(gateway_cpu)} if gateway_cpu else {})),
                             'gateway_env': GATEWAY['env']} if backend == 'clickhouse' else None),
                backend=backend, gateway_cpu=gateway_cpu,
                collector_image_override=collector_image, collector_gomaxprocs=gomaxprocs,
                us_margin_mode=us_margin_mode, gateway_lp_refusal=gateway_lp_refusal,
                sdk_retry=sdk_retry, sdk_retry_delay_ms=sdk_retry_delay_ms,
                priority_receiver=priority_receiver, cpu_shed_threshold=cpu_shed_threshold,
                gateway_cpu_shed_threshold=gateway_cpu_shed_threshold,
                cpu_target=cpu_target, gateway_cpu_target=gateway_cpu_target, priority_queue=priority_queue,
                gateway_priority_queue=gateway_priority_queue, agent_compression=agent_compression,
                case_order_note='single kind; no rotation')
    # Tomislav-RetCtx: stationary / bursty runs override the inherited ramp grid. The
    # generator block is what run_wrk reads; peak_multiplier sizes the connection pool
    # for the largest epoch (cap / E[x] for the truncated Pareto), and the realised
    # offered rate of a heavy-tailed run is sent_requests / duration, not -R.
    if rates is not None:
        plan['ramp_rates'] = list(rates)
    if seconds_per_rate is not None:
        plan['seconds_per_rate'] = seconds_per_rate
    if generator is not None:
        a, cap = generator['burst_alpha'], generator['burst_cap']
        norm = (a / (a - 1)) * (1 - cap ** (1 - a)) / (1 - cap ** (-a))
        plan['generator'] = dict(generator, peak_multiplier=cap / norm, min_multiplier=1 / norm,
                                 expected_x=norm, note='rate multiplier per epoch = truncated-Pareto(alpha) on '
                                 '[1,cap] / E[x]; E[g]=1 in expectation, realised mean varies per window')
    write_json(root / 'plan.json', plan)
    write_json(root / 'prepare-status.json', {'state': 'complete', 'cases': len(cases), 'derived_from': str(source)})
    write_json(root / 'image-build-status.json', {'state': 'complete', 'reused': True,
                                                  'app_images': APPS[app_name(cases[0])]['app_services'] * len(cases),
                                                  'note': 'pinned digests from ' + str(source)})
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
    parser.add_argument('--rates', type=int, nargs='+', default=None,
                        help='replace the inherited ramp grid (e.g. one rate for a stationary run)')
    parser.add_argument('--seconds-per-rate', type=int, default=None)
    parser.add_argument('--backend', choices=BACKENDS, default='jaeger',
                        help='trace store: jaeger (Jaeger v1 + Elasticsearch) or clickhouse (gateway collector + ClickHouse + Jaeger-API shim)')
    parser.add_argument('--gateway-cpu', default=None,
                        help='clickhouse backend only: CPU limit of the node-9 gateway collector (e.g. 2 or 1500m); default 4')
    parser.add_argument('--store', choices=sorted(STORE_TUNINGS), default='nowork',
                        help='Jaeger+Elasticsearch sizing: nowork (tuned, 12/26 cores) or realwork '
                             '(SN real-work e2e: 24/8 cores, stock writer)')
    parser.add_argument('--collector-image', default=None,
                        help='override the agents\' (and hence the gateway\'s) collector image; a @sha256 digest reference')
    parser.add_argument('--gomaxprocs', choices=('auto',), default=None,
                        help="'auto': GOMAXPROCS = each collector's CPU limit (Go 1.24 otherwise uses every node core)")
    parser.add_argument('--us-margin-mode', choices=('arrival', 'backlog'), default=None,
                        help='priority processor us_margin_mode on agents and gateway (default: processor default, arrival)')
    parser.add_argument('--gateway-lp-refusal', choices=('unavailable', 'resource_exhausted'), default=None,
                        help='gateway priority processor lp_refusal_code (resource_exhausted = agents drop shed LP, no retry)')
    parser.add_argument('--priority-receiver', action='store_true',
                        help="bridges' agents and gateway use the 'priorityotlp' receiver (pre-decode LP refusal)")
    parser.add_argument('--cpu-shed-threshold', type=float, default=None,
                        help="agents' priority processor cpu_shed_threshold (fraction of the CPU quota; off if unset)")
    parser.add_argument('--gateway-cpu-shed-threshold', type=float, default=None,
                        help="gateway priority processor cpu_shed_threshold (off if unset)")
    parser.add_argument('--cpu-target', type=float, default=None,
                        help="agents' priority processor cpu_target: graded LP admission up to this CPU fraction")
    parser.add_argument('--gateway-cpu-target', type=float, default=None,
                        help="gateway priority processor cpu_target (graded LP admission)")
    parser.add_argument('--priority-queue', action='store_true',
                        help="bridges' agents: strict-priority queue stage (priority -> batch -> priority/queue, "
                             "exporter sending_queue off; HP sent first, queued LP evicted to admit checkpoints)")
    parser.add_argument('--gateway-priority-queue', action='store_true',
                        help="bridges' gateway: the same strict-priority queue stage in front of the ClickHouse exporter "
                             "(batch per priority, exporter sending_queue off, dispatch_workers = its num_consumers)")
    parser.add_argument('--agent-compression', choices=('none', 'gzip', 'zstd', 'snappy'), default=None,
                        help="agents' OTLP exporter compression (collector default gzip); applies to every kind")
    parser.add_argument('--app-pprof', action='store_true', help='BRIDGES_PPROF=:6060 on every service (in-app CPU profiling)')
    parser.add_argument('--pprof', action='store_true', help='pprof extension (:1777) on agents and gateway (profiling)')
    parser.add_argument('--sdk-retry', choices=('priority',), default=None,
                        help="'priority': bridges retry refused checkpoint batches once and drop LP locally while one "
                             "waits (BRIDGES_RETRY=hp); vanilla retries every refused batch once (=all). Needs service "
                             "images built with runtime/plugins/otelcol/sdk_retry.go")
    parser.add_argument('--sdk-retry-delay-ms', type=int, default=None, help='BRIDGES_RETRY_DELAY_MS (SDK default 300)')
    parser.add_argument('--generator', choices=('pareto',), default=None,
                        help='bursty arrival process via the patched wrk2 fork (see run_wrk)')
    parser.add_argument('--wrk-binary', default='/users/tomislav/DeathStarBench/wrk2/wrk')
    parser.add_argument('--burst-alpha', type=float, default=1.5)
    parser.add_argument('--burst-cap', type=float, default=4.0)
    parser.add_argument('--burst-epoch', default='1s')
    args = parser.parse_args()
    generator = None
    if args.generator:
        generator = {'binary': args.wrk_binary, 'dist': args.generator, 'burst_alpha': args.burst_alpha,
                     'burst_cap': args.burst_cap, 'burst_epoch': args.burst_epoch}
    for case in derive(args.source.resolve(), args.out.resolve(), set(args.kinds), args.repetitions,
                       args.note, args.collector, args.reverse,
                       args.provenance_from.resolve() if args.provenance_from else None, rates=args.rates, seconds_per_rate=args.seconds_per_rate, generator=generator, backend=args.backend, gateway_cpu=args.gateway_cpu, store=args.store,
                       collector_image=args.collector_image, gomaxprocs=args.gomaxprocs, us_margin_mode=args.us_margin_mode,
                       gateway_lp_refusal=args.gateway_lp_refusal, sdk_retry=args.sdk_retry,
                       sdk_retry_delay_ms=args.sdk_retry_delay_ms, priority_receiver=args.priority_receiver,
                       cpu_shed_threshold=args.cpu_shed_threshold,
                       gateway_cpu_shed_threshold=args.gateway_cpu_shed_threshold,
                       cpu_target=args.cpu_target, gateway_cpu_target=args.gateway_cpu_target,
                       priority_queue=args.priority_queue, gateway_priority_queue=args.gateway_priority_queue,
                       pprof=args.pprof, agent_compression=args.agent_compression, app_pprof=args.app_pprof):
        print(case['name'], case['case'])
