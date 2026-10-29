"""Tomislav-RetCtx (user 2026-10-29): the larger hotel dataset and request mixes -- what the derive
puts in a manifest, what the runner then expects, and what wrk2 is handed."""
import copy
import json
import unittest
from unittest.mock import patch

import dsb_hotel_seed as seed
import run_dsb_sn_e2e
from dsb_apps import HOTEL_SEED

VARIANT = 'hotel-pb-es-x1'


def hotel_manifest(variant=VARIANT):
    """The shape of a prepared hotel manifest: the six databases, caches, services, collectors."""
    docs = []
    for base in ('geo-db', 'rate-db', 'profile-db', 'recomd-db', 'reserv-db', 'user-db'):
        docs.append({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': f'{base}-{variant}-ctr'},
                     'spec': {'template': {'spec': {'containers': [{'name': base, 'image': '10.10.1.1:30000/mongo@sha256:abc',
                                                                     'ports': [{'containerPort': 27017}]}]}}}})
        docs.append({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': f'{base}-{variant}-ctr'}})
    for service in ('frontend', 'search', 'geo', 'rate', 'profile', 'recomd', 'user', 'reserv'):
        docs.append({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': f'{service}-service-{variant}-ctr'},
                     'spec': {'template': {'spec': {'containers': [{'name': service, 'image': '10.10.1.1:30000/x@sha256:def'}]}}}})
    docs.append({'apiVersion': 'apps/v1', 'kind': 'DaemonSet', 'metadata': {'name': f'otelcol-{variant}', 'namespace': 'dsb-hotel'}})
    return docs


class SeedSpec(unittest.TestCase):
    def test_parse_defaults_and_bounds(self):
        self.assertEqual(seed.parse_spec(''), seed.SPEC_DEFAULTS)
        spec = seed.parse_spec('hotels=2000,users=5000,reservations=20,seed=7,lat_min=37.9')
        self.assertEqual((spec['hotels'], spec['users'], spec['reservations'], spec['seed'], spec['lat_min']), (2000, 5000, 20, 7, 37.9))
        self.assertEqual(seed.parse_spec(seed.spec_text(spec)), spec)
        for bad in ('hotels=10', 'users=1', 'reservations=-1', 'indexes=2', 'rooms=3'):
            with self.assertRaises((AssertionError, ValueError)):
                seed.parse_spec(bad)

    def test_eval_prologue_names_every_parameter_the_script_reads(self):
        text = seed.eval_params(seed.parse_spec('hotels=300,users=800,reservations=3,lon_max=-121.9'), 'reservation')
        self.assertEqual(text, "var TARGET='reservation', HOTELS=300, USERS=800, RES_ROWS_PER_NIGHT=3, SEED=1, INDEXES=1, "
                               "DESCRIPTION_BYTES=1000, LON_MAX=-121.9;")
        script = seed.SEED_SCRIPT.read_text()
        for name in ('HOTELS', 'USERS', 'RES_ROWS_PER_NIGHT', 'SEED', 'INDEXES', 'DESCRIPTION_BYTES', 'LON_MAX'):
            self.assertIn(f"param('{name}'", script)


class ManifestInstall(unittest.TestCase):
    def test_job_configmap_and_six_init_containers(self):
        docs = hotel_manifest()
        before = len(docs)
        spec = seed.parse_spec('hotels=2000,users=5000,reservations=20')
        summary = seed.install(docs, VARIANT, 'dsb-hotel', spec, script_text='// script')
        self.assertEqual(len(docs), before + 2)
        job = next(d for d in docs if d['kind'] == 'Job')
        cm = next(d for d in docs if d['kind'] == 'ConfigMap')
        self.assertEqual((job['metadata']['name'], cm['metadata']['name']), (f'hotel-seed-{VARIANT}',) * 2)
        self.assertEqual(job['metadata']['labels'], {'retctx-e2e': VARIANT})  # owned_resources / teardown
        self.assertEqual(job['spec']['backoffLimit'], 0)
        pod = job['spec']['template']['spec']
        self.assertEqual(pod['restartPolicy'], 'Never')
        self.assertEqual(pod['containers'][0]['image'], '10.10.1.1:30000/mongo@sha256:abc')  # the deployment's own mongo
        hosts = dict(p.split('=') for p in pod['containers'][0]['env'][0]['value'].split())
        self.assertEqual(hosts, {'geo': f'geo-db-{VARIANT}-ctr', 'rate': f'rate-db-{VARIANT}-ctr', 'profile': f'profile-db-{VARIANT}-ctr',
                                 'recommendation': f'recomd-db-{VARIANT}-ctr', 'reservation': f'reserv-db-{VARIANT}-ctr',
                                 'user': f'user-db-{VARIANT}-ctr'})
        self.assertEqual(cm['data']['seed_hotels.js'], '// script')
        self.assertIn("reservation) params=\"var TARGET='reservation', HOTELS=2000, USERS=5000, RES_ROWS_PER_NIGHT=20", cm['data']['seed.sh'])
        waited = {d['metadata']['name'] for d in docs if d['kind'] == 'Deployment'
                  and any(c['name'] == 'wait-seed' for c in d['spec']['template']['spec'].get('initContainers', []))}
        self.assertEqual(waited, {f'{s}-service-{VARIANT}-ctr' for s in ('geo', 'rate', 'profile', 'recomd', 'user', 'reserv')})
        self.assertEqual(sorted(summary['init_containers']), sorted(waited))
        reserv = next(d for d in docs if d['metadata']['name'] == f'reserv-service-{VARIANT}-ctr')
        command = reserv['spec']['template']['spec']['initContainers'][0]['command'][2]
        self.assertIn(f'--host reserv-db-{VARIANT}-ctr', command)
        self.assertIn('getSiblingDB("reservation-db").seed_marker.countDocuments({_id:"seed"})', command)
        # idempotent: a second install replaces, never stacks, the init container
        seed.install(docs, VARIANT, 'dsb-hotel', spec, script_text='// script')
        self.assertEqual(len(reserv['spec']['template']['spec']['initContainers']), 1)

    def test_install_refuses_a_manifest_without_the_databases(self):
        docs = [d for d in hotel_manifest() if not d['metadata']['name'].startswith('user-db-')]
        with self.assertRaises(AssertionError):
            seed.install(docs, VARIANT, 'dsb-hotel', seed.parse_spec(''), script_text='')


class ExpectedCounts(unittest.TestCase):
    def test_stock_plus_marker(self):
        self.assertEqual(seed.expected_counts(HOTEL_SEED, None)['rate-db.inventory'], 27)
        markers = {'geo-db': {'inserted': {'geo': 1920}}, 'rate-db': {'inserted': {'inventory': 1973}},
                   'profile-db': {'inserted': {'hotels': 1920}}, 'recomd-db': {'inserted': {'recommendation': 1920}},
                   'reserv-db': {'inserted': {'number': 1920, 'reservation': 600123}}, 'user-db': {'inserted': {'user': 4500}}}
        expected = seed.expected_counts(HOTEL_SEED, markers)
        self.assertEqual(expected, {'geo-db.geo': 2000, 'rate-db.inventory': 2000, 'profile-db.hotels': 2000,
                                    'recommendation-db.recommendation': 2000, 'reservation-db.reservation': 600124,
                                    'reservation-db.number': 2000, 'user-db.user': 5001})


class Workloads(unittest.TestCase):
    def test_scripts_exist_and_hotel_mix_takes_the_seeded_ranges(self):
        for app, name in (('hotel', 'search'), ('hotel', 'mixed'), ('hotelnw', 'mixed'), ('sn', 'compose'), ('sn', 'mixed'), ('snrw', 'mixed')):
            block = seed.workload_for(app, name)
            self.assertTrue(block['lua'].endswith('.lua'), block)
        mixed = seed.workload_for('hotel', 'mixed', seed.parse_spec('hotels=2000,users=5000'))
        self.assertEqual(mixed['lua_env'], {'max_user_index': '5000', 'max_hotel_id': '2000'})
        self.assertEqual(seed.workload_for('hotel', 'mixed')['lua_env'], {'max_user_index': '500', 'max_hotel_id': '80'})
        with self.assertRaises(AssertionError):
            seed.workload_for('sn', 'search')


class Readiness(unittest.TestCase):
    def test_ready_ignores_a_finished_job_pod(self):
        def pod(name, phase='Running', ready='True'):
            return {'metadata': {'name': name}, 'status': {'phase': phase, 'conditions': [{'type': 'Ready', 'status': ready}]}}
        items = [pod('a'), pod('b'), pod('hotel-seed-x-abcde', 'Succeeded', 'False')]
        with patch.object(run_dsb_sn_e2e, 'kube', return_value=json.dumps({'items': items})):
            self.assertEqual([p['metadata']['name'] for p in run_dsb_sn_e2e.ready('ns', 'x', expected=2)], ['a', 'b'])


if __name__ == '__main__':
    unittest.main()
