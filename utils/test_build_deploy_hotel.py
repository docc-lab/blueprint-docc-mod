import argparse
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import build_deploy_hotel as hotel


class HotelDeployTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.args = hotel.parser().parse_args([])

    def test_passthrough_removes_shedding_from_both_variants(self):
        self.args.collector = "passthrough"
        self.args.cpd = 6
        for kind in ("bridges", "vanilla"):
            with self.subTest(kind=kind):
                path = self.directory / "config.yaml"
                path.write_text((hotel.APP / f"wiring/specs/collector-{kind}.yaml").read_text())
                hotel.configure_collector(path, self.args)
                config = hotel.load(path)
                self.assertEqual(config["service"]["pipelines"]["traces"]["processors"], ["batch"])
                self.assertFalse({"priority", "memory_limiter"} & config["processors"].keys())
                self.assertEqual(config["receivers"]["configdiscovery"]["config_map"]["cpd"], 6)
                self.assertEqual(config["service"]["pipelines"]["traces"]["exporters"], ["otlp"])

    def test_runtime_env_reaches_every_app_with_only_frontend_as_root(self):
        suffix = "hotel_sb_esrev2"
        path = self.directory / "compose.yaml"
        services = {
            f"{name}_service_{suffix}_ctr": {"environment": ["BRIDGE_KIND=sb"]}
            for name in hotel.SERVICES
        }
        services[f"otelcol_{suffix}_ctr"] = {"environment": ["GC_INTERVAL_SEC=0.1"]}
        services[f"jaeger_{suffix}_ctr"] = {}
        services[f"rate_cache_{suffix}_ctr"] = {"image": "memcached"}
        hotel.save(path, {"services": services})
        self.args.reverse_truss = True
        self.args.gc = "natural"
        self.args.sample_ratio = 0.25
        compose = hotel.configure_compose(path, suffix, self.args)
        for name in hotel.SERVICES:
            env = compose["services"][f"{name}_service_{suffix}_ctr"]["environment"]
            self.assertEqual(env["BRIDGE_KIND"], "sb")
            self.assertEqual(env["REVERSE_TRUSS"], "on")
            self.assertEqual(env["RT_ROOT"], "on" if name == "frontend" else "off")
            self.assertEqual(env["OTLP_RETRY"], "off")
            self.assertEqual(env["GOGC"], "100")
            self.assertEqual(env["GC_INTERVAL_SEC"], "0")
            self.assertEqual(env["OTEL_SAMPLE_RATIO"], "0.25")
        self.assertEqual(compose["services"][f"otelcol_{suffix}_ctr"]["environment"]["GC_INTERVAL_SEC"], "0")
        self.assertEqual(compose["services"][f"rate_cache_{suffix}_ctr"]["image"], "memcached:latest")

    def test_reverse_probability_options_reach_collector_discovery(self):
        self.args = hotel.parser().parse_args(["--reverse-policy", "probability", "--reverse-probability", "0.25"])
        path = self.directory / "config.yaml"
        path.write_text((hotel.APP / "wiring/specs/collector-bridges.yaml").read_text())
        hotel.configure_collector(path, self.args)
        config = hotel.load(path)
        self.assertEqual(config["receivers"]["configdiscovery"]["config_map"]["reverse_policy"], "probability")
        self.assertEqual(config["receivers"]["configdiscovery"]["config_map"]["reverse_probability"], 0.25)
        self.args = hotel.parser().parse_args(["--reverse-policy", "depth_linear"])
        hotel.configure_collector(path, self.args)
        config = hotel.load(path)
        self.assertEqual(config["receivers"]["configdiscovery"]["config_map"]["reverse_policy"], "depth_linear")
        self.assertNotIn("reverse_probability", config["receivers"]["configdiscovery"]["config_map"])

    def test_manifests_are_namespaced_and_probed_without_changing_local_routing(self):
        suffix = "hotel_pb_es"
        for kind, name, port in (
            ("DaemonSet", "otelcol-hotel-pb-es-ctr", 4317),
            ("Deployment", "frontend-service-hotel-pb-es-ctr", 2000),
        ):
            hotel.save(self.directory / f"{name}-{kind}.yaml", {
                "kind": kind, "metadata": {"name": name},
                "spec": {"template": {"metadata": {}, "spec": {"containers": [{
                    "name": name, "ports": [{"containerPort": port}], "image": "test:latest",
                }]}}},
            })
            hotel.save(self.directory / f"{name}-service.yaml", {
                "kind": "Service", "metadata": {"name": name},
                "spec": {"ports": [{"port": port}],
                         **({"internalTrafficPolicy": "Local"} if port == 4317 else {})},
            })
        self.args.nodeport = 31080
        hotel.prepare_manifests(self.directory, suffix, self.args)
        for path in self.directory.glob("*.yaml"):
            doc = hotel.load(path)
            self.assertEqual(doc["metadata"]["namespace"], "dsb-hotel")
            if doc["kind"] in ("DaemonSet", "Deployment"):
                c = doc["spec"]["template"]["spec"]["containers"][0]
                self.assertIn("startupProbe", c)
                self.assertIn("readinessProbe", c)
            elif doc["metadata"]["name"].startswith("otelcol-"):
                self.assertEqual(doc["spec"]["internalTrafficPolicy"], "Local")
            else:
                self.assertNotIn("internalTrafficPolicy", doc["spec"])
                self.assertEqual(doc["spec"]["ports"][0]["nodePort"], 31080)

    def test_failed_image_build_never_pushes_or_continues(self):
        name = "frontend_service_hotel_pb_ctr"
        hotel.save(self.directory / "frontend.yaml", {
            "kind": "Deployment", "metadata": {"name": name.replace("_", "-")},
            "spec": {"template": {"spec": {"containers": [{"image": "registry/frontend:rev2"}]}}},
        })
        compose = {"services": {name: {"build": {"context": "frontend"}}}}
        with patch.object(hotel, "run", side_effect=subprocess.CalledProcessError(1, ["docker", "build"])) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                hotel.build_images(compose, self.directory, self.directory)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.args[0][:2], ["docker", "build"])


if __name__ == "__main__":
    unittest.main()
