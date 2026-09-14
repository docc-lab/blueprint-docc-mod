import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

from reverse_policy import configure, validate


class ReversePolicyTest(unittest.TestCase):
    def test_switching_policies_removes_stale_probability(self):
        config = {"cpd_min": 2, "cpd_max": 6}
        configure(config, "probability", 0.25)
        self.assertEqual(config["reverse_probability"], 0.25)
        configure(config, "inverse_depth")
        self.assertEqual(config["reverse_policy"], "inverse_depth")
        self.assertNotIn("reverse_probability", config)
        configure(config, "depth_linear")
        configure(config, "ttl")
        self.assertEqual(config, {"cpd_min": 2, "cpd_max": 6, "reverse_policy": "ttl"})

    def test_no_override_preserves_custom_collector_settings(self):
        for config in ({}, {"reverse_policy": "depth_linear"},
                       {"reverse_policy": "probability", "reverse_probability": "0.25"}):
            with self.subTest(config=config):
                original = dict(config)
                configure(config)
                self.assertEqual(config, original)

    def test_invalid_options_and_custom_yaml(self):
        for policy, probability in [("invalid", None), (None, 0.5), ("ttl", 0.5),
                                    ("inverse_depth", 0.5), ("depth_linear", 0.5),
                                    *[("probability", v) for v in (None, -1, 1.1, True, math.nan, math.inf, 10**1000)]]:
            with self.subTest(policy=policy, probability=probability), self.assertRaises(ValueError):
                validate(policy, probability)
        for config in ({"reverse_policy": None}, {"reverse_probability": None},
                       {"reverse_policy": "probability"},
                       {"reverse_policy": "probability", "reverse_probability": "NaN"}):
            with self.subTest(config=config), self.assertRaises(ValueError):
                configure(config)
        for p in (0, 0.25, 1):
            validate("probability", p)

    def run_dsb_python(self, marker, arguments):
        # Execute the deployer's actual embedded configuration code in isolation;
        # tests must never enter its image-build or deployment stages.
        directory = Path(__file__).resolve().parent
        source = (directory / "build_deploy_dsb.sh").read_text()
        self.assertEqual(source.count(marker), 1)
        code = marker + source.split(marker, 1)[1].split("\nPY", 1)[0]
        env = dict(os.environ, PYTHONPATH=str(directory))
        return subprocess.run([sys.executable, "-c", code, *arguments],
                              env=env, capture_output=True, text=True, timeout=20)

    def test_dsb_option_validation_rejects_bad_policies(self):
        marker = "import sys\nfrom checkpoint_distance import validate\nimport reverse_policy\n"
        for policy, probability in (("unknown", ""), ("probability", ""),
                                    ("probability", "nan"), ("depth_linear", "0.5")):
            result = self.run_dsb_python(marker, ["", "", "", "docker_cgpb_es", policy, probability])
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("reverse-", result.stderr)

    def test_dsb_collector_stage_writes_and_switches_policy(self):
        marker = "import sys, yaml\nfrom checkpoint_distance import configure\nimport reverse_policy\n"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.yaml"
            path.write_text(yaml.safe_dump({
                "receivers": {"configdiscovery": {"config_map": {"cpd": 6}}},
                "processors": {"priority": {}, "memory_limiter": {}},
                "service": {"pipelines": {"traces": {"processors": ["priority"]}}},
            }))
            for policy, probability in (("probability", "0.25"), ("depth_linear", ""), ("ttl", "")):
                result = self.run_dsb_python(marker, [str(path), "", "50", "70", "1", "true", "1s", "0s", "passthrough", "2", "6", policy, probability])
                self.assertEqual(result.returncode, 0, result.stderr)
                config = yaml.safe_load(path.read_text())
                cm = config["receivers"]["configdiscovery"]["config_map"]
                self.assertEqual(cm["reverse_policy"], policy)
                self.assertEqual((cm["cpd_min"], cm["cpd_max"]), (2, 6))
                if probability:
                    self.assertEqual(cm["reverse_probability"], 0.25)
                else:
                    self.assertNotIn("reverse_probability", cm)
                self.assertEqual(config["service"]["pipelines"]["traces"]["processors"], ["batch"])


if __name__ == "__main__":
    unittest.main()
