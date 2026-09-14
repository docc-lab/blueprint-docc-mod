"""Tomislav-RetCtx: histogram integrity, byte accounting, and tail preservation."""

import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from generate_spanload_distributions import (
    BRIDGES, BYTE_ACCOUNTING, MAX_INPUT_BYTES, SCHEMA, convert, generate,
)


def fixture():
    # The rare last bin must survive conversion without sample expansion.
    sizes, counts = [4, 20, 7526], [500_000_000, 499_999_999, 1]
    cumulative = 0
    bins = []
    for size, count in zip(sizes, counts):
        cumulative += count
        bins.append({"bytes": size, "count": count, "percentage": count / 10_000_000,
                     "cumulative_percentage": cumulative / 10_000_000})
    histogram = {"payload_count": cumulative, "min_bytes": 4, "max_bytes": 7526,
                 "mean_bytes": sum(s * n for s, n in zip(sizes, counts)) / cumulative,
                 "observed_byte_sizes": 3, "source_histogram_sha256": "a" * 64, "bins": bins}
    return {"schema": SCHEMA, "metric": "emitted_bridge_payload_bytes", "bin_width_bytes": 1,
            "zero_count_bins_omitted": True, "percentage_unit": "percent (0 to 100)",
            "normalization": "All emitted bridge payloads within each bridge type, including the entire tail.",
            "byte_accounting": BYTE_ACCOUNTING, "corpus": {"name": "test", "traces": 1, "spans": 1_000_000_000},
            "configuration": {"checkpoint_distance_min": 2, "checkpoint_distance_max": 8, "checkpoint_seed": 42,
                              "prime_mode": "no-prime", "sb3": {"lehmer": True, "single_pop": True}},
            "bridges": {k: copy.deepcopy(histogram) for k in BRIDGES}}


class DistributionConversionTest(unittest.TestCase):
    def test_conserves_exact_counts_and_subtracts_key_type_bytes(self):
        data = json.dumps(fixture()).encode()
        files, manifest = convert(data, "payload_percentages.json")
        self.assertEqual(set(files), {"pb.json", "cgpb.json", "sb.json", "manifest.json"})
        for original, name in BRIDGES.items():
            profile = json.loads(files[name + ".json"])
            self.assertEqual(profile["type"], "discrete")
            self.assertEqual(profile["values"], [{"bytes": 0, "weight": 500_000_000},
                                                 {"bytes": 16, "weight": 499_999_999},
                                                 {"bytes": 7522, "weight": 1}])
            record = manifest["bridges"][name]
            self.assertEqual(record["simulator_bridge"], original)
            self.assertEqual(record["payload_count"], 1_000_000_000)
            self.assertEqual(record["file_sha256"], hashlib.sha256(files[name + ".json"]).hexdigest())
            self.assertAlmostEqual(record["mean_value_bytes"], record["simulator_mean_bytes"] - 4)
            self.assertLess(len(files[name + ".json"]), 2000)
        self.assertEqual(manifest["configuration"], fixture()["configuration"])
        self.assertEqual(manifest["input_sha256"], hashlib.sha256(data).hexdigest())
        self.assertEqual(convert(data, "payload_percentages.json")[0], files)

    def test_rejects_inconsistent_or_ambiguous_histograms(self):
        changes = [
            (("schema",), "unknown"), (("metric",), "baggage_bytes"),
            (("bin_width_bytes",), 2), (("bin_width_bytes",), True),
            (("byte_accounting",), "Value only"), (("normalization",), "99% of payloads"),
            (("percentage_unit",), "fraction"), (("zero_count_bins_omitted",), False),
            (("corpus", "spans"), 0), (("configuration", "checkpoint_distance_max"), 1),
            (("bridges", "PB0", "payload_count"), 1_000_000_001),
            (("bridges", "PB0", "payload_count"), (1 << 53) + 1),
            (("bridges", "PB0", "min_bytes"), 3), (("bridges", "PB0", "mean_bytes"), 99),
            (("bridges", "PB0", "observed_byte_sizes"), 2),
            (("bridges", "PB0", "source_histogram_sha256"), "invalid"),
            (("bridges", "PB0", "bins", 0, "bytes"), 3),
            (("bridges", "PB0", "bins", 1, "bytes"), 4),
            (("bridges", "PB0", "bins", 2, "bytes"), (1 << 20) + 5),
            (("bridges", "PB0", "bins", 0, "count"), 500_000_000.5),
            (("bridges", "PB0", "bins", 0, "count"), True),
            (("bridges", "PB0", "bins", 2, "count"), 0),
            (("bridges", "PB0", "bins", 0, "percentage"), 100),
            (("bridges", "PB0", "bins", 0, "cumulative_percentage"), 100),
        ]
        for path, value in changes:
            with self.subTest(path=path, value=value):
                source = fixture()
                target = source
                for part in path[:-1]:
                    target = target[part]
                target[path[-1]] = value
                with self.assertRaises(ValueError):
                    convert(json.dumps(source).encode(), "test.json")

    def test_rejects_invalid_json_or_missing_bridges(self):
        for data in [b'null', b'[]', b'{"schema":1,"schema":2}', b'{"x":NaN}',
                     b'{"x":Infinity}', b'{} {}', b' ' * (MAX_INPUT_BYTES + 1)]:
            with self.subTest(data=data[:40]), self.assertRaises(ValueError):
                convert(data, "test.json")
        source = fixture()
        del source["bridges"]["SB3"]
        with self.assertRaises(ValueError):
            convert(json.dumps(source).encode(), "test.json")

    def test_new_output_directory_and_invalid_input_are_safe(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source, output = root / "source.json", root / "profiles"
            source.write_text(json.dumps(fixture()))
            generate(source, output)
            saved = {p.name: p.read_bytes() for p in output.iterdir()}
            with self.assertRaises(FileExistsError):
                generate(source, output)
            self.assertEqual({p.name: p.read_bytes() for p in output.iterdir()}, saved)
            source.write_text('{}')
            with self.assertRaises(ValueError):
                generate(source, root / "invalid")
            self.assertFalse((root / "invalid").exists())


if __name__ == "__main__":
    unittest.main()
