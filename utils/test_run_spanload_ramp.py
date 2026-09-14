"""Tomislav-RetCtx: steady-window accounting excludes startup and drain."""

from types import SimpleNamespace
from pathlib import Path
import json
import tempfile
import unittest
from unittest.mock import patch

from run_spanload_ramp import METRICS, REPO, combine_phases, epoch, run_variant, summarize_step


class RampAccountingTest(unittest.TestCase):
    def test_complete_grid_continues_after_saturation(self):
        # Tomislav-RetCtx: all variants must reach the same endpoint in complete-grid mode.
        for complete, expected in ((False, 2), (True, 4)):
            with self.subTest(complete_grid=complete), tempfile.TemporaryDirectory() as temporary:
                args = SimpleNamespace(out=Path(temporary), collector_config=REPO / 'utils/spanload/collector.yaml',
                    export_format='proto', collector_cpu=2, collector_gomemlimit='2400MiB',
                    collector_image='test-image', rates=[100, 200, 300, 400], complete_grid=complete)
                details = {'HostConfig': {'NanoCpus': 1000000000, 'Memory': 4 * (1 << 30)},
                           'State': {'Pid': 123, 'ExitCode': 0, 'OOMKilled': False}, 'RestartCount': 0}
                def fake_command(argv):
                    return json.dumps([details]) if argv[1] == 'inspect' else ''
                def fake_step(variant, rate, *unused):
                    return {'variant': variant, 'target_spans_per_second': rate,
                            'capacity_limited': True, 'exported_spans_per_second': 80,
                            'generator_cpu_cores_per_process': [.5] * 4, 'collector_peak_rss_bytes': 1000000}
                with patch('run_spanload_ramp.command', side_effect=fake_command) as commands, \
                     patch('run_spanload_ramp.scrape'), patch('run_spanload_ramp.socket.create_connection'), \
                     patch('run_spanload_ramp.run_step', side_effect=fake_step):
                    rows, saturated = run_variant('pb', args, .5)
                self.assertTrue(saturated)
                self.assertEqual(len(rows), expected)
                self.assertEqual([row['target_spans_per_second'] for row in rows], args.rates[:expected])
                self.assertIn('GOMEMLIMIT=2400MiB', commands.call_args_list[0].args[0])
                state = json.loads((args.out / 'pb/completion.json').read_text())
                self.assertEqual(state['complete_grid'], complete)

    def test_multiple_senders_use_overlap_and_weight_payload_means(self):
        first = {key: 0 for key in ("offered_spans", "attempted_spans", "acknowledged_spans",
            "attempted_checkpoint_spans", "attempted_protobuf_bytes", "queue_dropped_spans",
            "failed_spans", "rejected_spans", "unsent_spans", "scheduler_unissued_spans")}
        first.update(started="1970-01-01T00:00:00Z", generation_seconds=25,
            offered_spans=1000, attempted_spans=1000, acknowledged_spans=1000, attempted_checkpoint_spans=100,
            attempted_checkpoint_payload={"count": 100, "total_bytes": 1000, "mean_bytes": 10})
        second = {**first, "started": "1970-01-01T00:00:02Z", "generation_seconds": 24,
            "offered_spans": 2400, "attempted_spans": 2400, "acknowledged_spans": 2400, "attempted_checkpoint_spans": 300,
            "attempted_checkpoint_payload": {"count": 300, "total_bytes": 9000, "mean_bytes": 30}}
        combined = combine_phases([first, second])
        self.assertEqual(combined["generation_seconds"], 23)
        self.assertEqual(combined["started"], second["started"])
        self.assertEqual(combined["offered_spans"], 3400)
        self.assertEqual(combined["offered_spans_per_second"], 140)
        self.assertEqual(combined["attempted_checkpoint_payload"]["mean_bytes"], 25)
        second["started"] = "1970-01-01T00:01:00Z"
        with self.assertRaises(ValueError):
            combine_phases([first, second])

    def test_go_nanosecond_timestamps(self):
        self.assertAlmostEqual(epoch("2026-09-14T14:40:14.150844812Z"),
                               epoch("2026-09-14T14:40:14.150844+00:00"))
        self.assertAlmostEqual(epoch("1970-01-01T00:00:00.1Z"), .1)

    def test_steady_rates_exclude_startup_and_drain(self):
        phase = {"started": "1970-01-01T00:00:00Z", "generation_seconds": 25,
                 "offered_spans": 30000, "attempted_spans": 25000,
                 "attempted_checkpoint_spans": 12500, "attempted_protobuf_bytes": 2000000,
                 "attempted_checkpoint_payload": {"mean_bytes": 20},
                 "queue_dropped_spans": 5000, "failed_spans": 0, "rejected_spans": 0,
                 "unsent_spans": 0, "scheduler_unissued_spans": 0}
        samples = []
        for second in range(30):
            count = max(second - 5, 0) * 1000
            if second > 24:
                count += 1000000  # drain traffic must not inflate steady throughput
            samples.append({"time": second, "counters": {METRICS[0]: count, METRICS[1]: 0,
                           METRICS[2]: count, METRICS[3]: 0},
                           "collector": {"cpu_seconds": second * .98, "rss_bytes": 100},
                           "generator": {"cpu_seconds": second * .2, "rss_bytes": 200}})
        result = summarize_step("pb", 1200, phase, samples, SimpleNamespace(discard_first=5))
        self.assertEqual(result["steady_seconds"], 19)
        self.assertEqual(result["exported_spans_per_second"], 1000)
        self.assertEqual(result["accepted_spans_per_second"], 1000)
        self.assertAlmostEqual(result["collector_cpu_cores"], .98)
        self.assertAlmostEqual(result["generator_cpu_cores"], .2)
        self.assertEqual(result["attempted_checkpoint_fraction"], .5)
        self.assertTrue(result["capacity_limited"])
        with self.assertRaises(RuntimeError):
            summarize_step("pb", 1200, phase, samples[:7], SimpleNamespace(discard_first=5))
        with self.assertRaisesRegex(RuntimeError, "steady measurement window"):
            summarize_step("pb", 1200, phase, samples, SimpleNamespace(discard_first=5, min_steady_seconds=30))


if __name__ == "__main__":
    unittest.main()
