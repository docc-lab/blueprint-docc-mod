"""Tomislav-RetCtx: focused checks for the zero-work ramp helpers."""
import unittest

from prepare_dsb_sn_nw import (BACKEND_TUNING, CASES, COLLECTOR_PROFILES, case_name, collector_config,
                               service_environment, tune_backend)
from run_dsb_sn_nw import connections_for, millicores, CONNECTION_CAP


class NoWorkHelpers(unittest.TestCase):
    def test_connection_schedule_matches_history_below_cap(self):
        self.assertEqual(connections_for(2000), 200)
        self.assertEqual(connections_for(5000), 1250)
        self.assertEqual(connections_for(7000), 2450)
        self.assertEqual(connections_for(7500), CONNECTION_CAP)
        self.assertEqual(connections_for(14000), CONNECTION_CAP)
        self.assertEqual(connections_for(1), 1)

    def test_case_names_and_order(self):
        self.assertEqual([case_name(k, r) for k, r in CASES],
                         ['nt', 'v', 'pb', 'cgpb', 'sb', 'v-s10', 'pb-s10', 'cgpb-s10', 'sb-s10'])

    def test_passthrough_collector_pipeline(self):
        for kind in ('nt', 'v', 'pb', 'cgpb', 'sb'):
            cfg = collector_config(kind, 'x')
            self.assertEqual(cfg['service']['pipelines']['traces']['processors'], ['batch'])
            self.assertNotIn('memory_limiter', cfg['processors'])
            self.assertNotIn('priority', cfg['processors'])
            self.assertEqual('configdiscovery' in cfg['receivers'], kind in ('pb', 'cgpb', 'sb'))

    def test_service_environment(self):
        self.assertNotIn('BRIDGE_KIND', service_environment('nt', 1.0))
        env = service_environment('pb', 0.1)
        self.assertEqual((env['BRIDGE_KIND'], env['OTEL_SAMPLE_RATIO'], env['REVERSE_TRUSS'], env['RT_LEAF_REJECT']),
                         ('pb', 0.1, 'on', 1))
        env = service_environment('v', 1.0)
        self.assertEqual((env['REVERSE_TRUSS'], env['RT_LEAF_REJECT'], env['OTLP_RETRY']), ('off', 0, 'off'))

    def test_millicores_accepts_normalised_quantities(self):
        self.assertEqual([millicores(q) for q in ('26', '26000m', '6', '500m', '1')], [26000, 26000, 6000, 500, 1000])

    def test_tune_backend_moves_cpu_to_elasticsearch(self):
        def deployment(name, env):
            return {'kind': 'Deployment', 'metadata': {'name': name}, 'spec': {'template': {'spec': {'containers': [
                {'name': name, 'env': [{'name': k, 'value': v} for k, v in env.items()],
                 'resources': {'requests': {'cpu': '24000m'}, 'limits': {'cpu': '24000m'}}}]}}}}
        jaeger = deployment('jaeger-x-ctr', {'ES_BULK_WORKERS': '10', 'GOMAXPROCS': '24'})
        elastic = deployment('elasticsearch-x-ctr', {'ES_JAVA_OPTS': '-Xms4g -Xmx4g', 'GOMAXPROCS': '8'})
        other = deployment('user-service-x-ctr', {})
        self.assertTrue(tune_backend(jaeger)); self.assertTrue(tune_backend(elastic)); self.assertFalse(tune_backend(other))
        env = {e['name']: e['value'] for e in jaeger['spec']['template']['spec']['containers'][0]['env']}
        self.assertEqual((env['ES_TAGS_AS_FIELDS_ALL'], env['ES_NUM_REPLICAS'], env['ES_NUM_SHARDS'], env['GOMAXPROCS'],
                          env['ES_BULK_WORKERS']), ('true', '0', '12', '12', '24'))
        self.assertEqual(jaeger['spec']['template']['spec']['containers'][0]['resources']['limits']['cpu'], '12000m')
        es = elastic['spec']['template']['spec']['containers'][0]
        self.assertEqual(es['resources'], {'requests': {'cpu': '26000m'}, 'limits': {'cpu': '26000m'}})
        self.assertEqual({e['name']: e['value'] for e in es['env']}['ES_JAVA_OPTS'], '-Xms16g -Xmx16g')
        self.assertEqual(other['spec']['template']['spec']['containers'][0]['resources']['limits']['cpu'], '24000m')
        self.assertEqual(BACKEND_TUNING['index_template']['order'], 10)
        self.assertLessEqual(BACKEND_TUNING['jaeger_cpus'] + BACKEND_TUNING['elasticsearch_cpus'], 39)

    def test_admission_profile_matches_the_real_work_pipeline(self):
        for kind in ('pb', 'cgpb', 'sb'):
            cfg = collector_config(kind, 'x', 'admission')
            self.assertEqual(cfg['service']['pipelines']['traces']['processors'], ['priority', 'batch'])
            self.assertEqual(cfg['processors']['priority']['soft_percentage'], 50)
            self.assertEqual(cfg['processors']['priority']['hard_percentage'], 70)
            self.assertEqual(cfg['processors']['priority']['cp_safety_factor'], 1)
            self.assertTrue(cfg['processors']['priority']['force_gc'])
            self.assertNotIn('memory_limiter', cfg['processors'])
            self.assertIn('configdiscovery', cfg['receivers'])
        for kind in ('v', 'nt'):
            cfg = collector_config(kind, 'x', 'admission')
            self.assertEqual(cfg['service']['pipelines']['traces']['processors'], ['memory_limiter', 'batch'])
            self.assertEqual(cfg['processors']['memory_limiter']['limit_percentage'], 70)
            self.assertNotIn('priority', cfg['processors'])
            self.assertNotIn('configdiscovery', cfg['receivers'])

    def test_admission2g_has_the_same_policy_with_a_larger_budget(self):
        for kind in ('pb', 'cgpb', 'sb'):
            self.assertEqual(collector_config(kind, 'x', 'admission2g'),
                             collector_config(kind, 'x', 'admission'))
        self.assertEqual(COLLECTOR_PROFILES['admission2g']['resources'], {'cpu': '1', 'memory': '2Gi'})
        self.assertEqual(COLLECTOR_PROFILES['admission2g']['env']['GOMEMLIMIT'], '1843MiB')

    def test_admission_profile_keeps_e2e_memory_and_no_work_cpu(self):
        self.assertEqual(COLLECTOR_PROFILES['admission']['resources'], {'cpu': '1', 'memory': '256Mi'})
        self.assertEqual(COLLECTOR_PROFILES['admission']['env']['GOMEMLIMIT'], '230MiB')
        self.assertEqual(COLLECTOR_PROFILES['passthrough']['resources'], {'cpu': '1', 'memory': '4Gi'})

    def test_passthrough_profile_is_the_default_and_unchanged(self):
        for kind in ('nt', 'v', 'pb', 'cgpb', 'sb'):
            self.assertEqual(collector_config(kind, 'x'), collector_config(kind, 'x', 'passthrough'))
            self.assertEqual(collector_config(kind, 'x')['service']['pipelines']['traces']['processors'], ['batch'])


if __name__ == '__main__':
    unittest.main()
