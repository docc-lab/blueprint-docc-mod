#!/usr/bin/env python3
"""Tomislav-RetCtx: convert simulator histograms to exact spanload size weights."""

import argparse
import hashlib
import json
import math
from pathlib import Path
import re
import sys


SCHEMA = "bridges.payload_size_percentages.v1"
BRIDGES = {"PB0": "pb", "CGP0": "cgpb", "SB3": "sb"}
BYTE_ACCOUNTING = (
    "Includes the 3-byte _br key and 1-byte type tag; excludes the separate _d "
    "attribute and the rest of the span."
)
KEY_TYPE_BYTES = 4
MAX_PAYLOAD_BYTES = 1 << 20
MAX_INPUT_BYTES = 4 << 20
# JSON integer weights must survive spanload's float64 decoding exactly.
MAX_EXACT_COUNT = 1 << 53


def require(condition, message):
    if not condition:
        raise ValueError(message)


def integer(value, label, minimum=0, maximum=MAX_EXACT_COUNT):
    require(type(value) is int and minimum <= value <= maximum,
            f"{label} must be an integer in {minimum}..{maximum}")
    return value


def number(value, label):
    require(type(value) in (int, float) and math.isfinite(value),
            f"{label} must be a finite number")
    return value


def matches(value, expected, label):
    number(value, label)
    require(math.isclose(value, expected, rel_tol=1e-12, abs_tol=1e-9),
            f"{label} disagrees with exact bin counts: {value} vs {expected}")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def reject_constant(value):
    raise ValueError(f"non-finite JSON number: {value}")


def json_bytes(value):
    return (json.dumps(value, indent=2, allow_nan=False) + "\n").encode()


def convert(data, input_name):
    """Validate before writing; retain every positive-count, one-byte bin."""
    require(len(data) <= MAX_INPUT_BYTES, "input JSON exceeds 4 MiB")
    source = json.loads(data, object_pairs_hook=unique_object, parse_constant=reject_constant)
    require(isinstance(source, dict), "input must be an object")
    require(source.get("schema") == SCHEMA, f"expected schema {SCHEMA}")
    require(source.get("metric") == "emitted_bridge_payload_bytes", "unsupported histogram metric")
    require(type(source.get("bin_width_bytes")) is int and source["bin_width_bytes"] == 1,
            "exact one-byte bins are required; grouped ranges cannot recover exact sizes")
    require(source.get("zero_count_bins_omitted") is True, "expected positive-count bins only")
    require(source.get("percentage_unit") == "percent (0 to 100)", "expected percentages in 0..100")
    require(source.get("normalization") == "All emitted bridge payloads within each bridge type, including the entire tail.",
            "histogram must include all emitted payloads and the entire tail")
    require(source.get("byte_accounting") == BYTE_ACCOUNTING,
            "unsupported byte accounting; expected _br key + type (4 bytes) in each size")
    corpus, config, bridges = source.get("corpus"), source.get("configuration"), source.get("bridges")
    require(isinstance(corpus, dict) and isinstance(corpus.get("name"), str) and corpus["name"].strip(),
            "corpus must include a name")
    integer(corpus.get("traces"), "corpus.traces", 1)
    integer(corpus.get("spans"), "corpus.spans", 1)
    require(isinstance(config, dict), "configuration must be an object")
    lo = integer(config.get("checkpoint_distance_min"), "checkpoint_distance_min", 1, 256)
    hi = integer(config.get("checkpoint_distance_max"), "checkpoint_distance_max", lo, 256)
    integer(config.get("checkpoint_seed"), "checkpoint_seed", 0, (1 << 64) - 1)
    require(isinstance(bridges, dict) and set(bridges) == set(BRIDGES),
            "expected exactly the PB0, CGP0, and SB3 histograms")
    input_hash = hashlib.sha256(data).hexdigest()
    outputs = {}
    summary = {
        "schema": "spanload.simulator_distributions.v1",
        "annotation": "Tomislav-RetCtx: simulator count histograms converted to _br byte-value distributions.",
        "input_file": input_name,
        "input_sha256": input_hash,
        "input_schema": SCHEMA,
        "input_metric": source["metric"],
        "input_byte_accounting": BYTE_ACCOUNTING,
        "subtracted_key_and_type_bytes": KEY_TYPE_BYTES,
        "output_metric": "_br OTLP bytes_value length; excludes key, type, protobuf framing, and separate depth metadata",
        "corpus": corpus,
        "configuration": config,
        "sampling": "Exact bin counts are relative weights; all tail bins are retained. Checkpoint cadence is configured separately in spanload.",
        "bridges": {},
    }
    for original, name in BRIDGES.items():
        histogram = bridges[original]
        require(isinstance(histogram, dict), f"{original} histogram must be an object")
        total = integer(histogram.get("payload_count"), f"{original}.payload_count", 1)
        bins = histogram.get("bins")
        require(isinstance(bins, list) and bins, f"{original}.bins must be a nonempty array")
        require(integer(histogram.get("observed_byte_sizes"), f"{original}.observed_byte_sizes", 1) == len(bins),
                f"{original} observed size count does not match bins")
        histogram_hash = histogram.get("source_histogram_sha256")
        require(isinstance(histogram_hash, str) and re.fullmatch(r"[0-9a-f]{64}", histogram_hash),
                f"{original} needs a source histogram SHA-256")
        cumulative, previous, byte_sum = 0, -1, 0
        values = []
        for i, row in enumerate(bins):
            label = f"{original}.bins[{i}]"
            require(isinstance(row, dict), f"{label} must be an object")
            size = integer(row.get("bytes"), label + ".bytes", KEY_TYPE_BYTES, MAX_PAYLOAD_BYTES + KEY_TYPE_BYTES)
            count = integer(row.get("count"), label + ".count", 1)
            require(size > previous, f"{label}: byte sizes must be unique and strictly increasing")
            cumulative += count
            require(cumulative <= total, f"{original} bin counts exceed payload_count")
            matches(row.get("percentage"), 100 * count / total, label + ".percentage")
            matches(row.get("cumulative_percentage"), 100 * cumulative / total, label + ".cumulative_percentage")
            byte_sum += size * count
            # Tomislav-RetCtx: simulator key/type overhead is separate from the
            # OTLP value. Integer counts avoid rounded-percentage tail loss.
            values.append({"bytes": size - KEY_TYPE_BYTES, "weight": count})
            previous = size
        require(cumulative == total, f"{original} bin counts do not sum to payload_count")
        require(integer(histogram.get("min_bytes"), f"{original}.min_bytes") == bins[0]["bytes"], f"{original} min_bytes mismatch")
        require(integer(histogram.get("max_bytes"), f"{original}.max_bytes") == bins[-1]["bytes"], f"{original} max_bytes mismatch")
        matches(histogram.get("mean_bytes"), byte_sum / total, f"{original}.mean_bytes")
        description = (f"Tomislav-RetCtx: {input_name} sha256={input_hash}; "
                       f"{corpus['name']}, {original}, CPD {lo}..{hi}, checkpoint seed {config['checkpoint_seed']}; "
                       f"source histogram sha256={histogram_hash}; exact counts, full tail; "
                       "4-byte _br key/type overhead subtracted. See sibling manifest.json for simulation settings.")
        filename = name + ".json"
        outputs[filename] = json_bytes({"source": description, "type": "discrete", "values": values})
        require(len(outputs[filename]) <= MAX_INPUT_BYTES, f"{filename} exceeds spanload's 4 MiB input limit")
        mean = (byte_sum - KEY_TYPE_BYTES * total) / total
        summary["bridges"][name] = {
            "simulator_bridge": original,
            "source_histogram_sha256": histogram_hash,
            "file": filename,
            "file_sha256": hashlib.sha256(outputs[filename]).hexdigest(),
            "payload_count": total,
            "distinct_sizes": len(values),
            "simulator_mean_bytes": byte_sum / total,
            "min_value_bytes": values[0]["bytes"],
            "max_value_bytes": values[-1]["bytes"],
            "mean_value_bytes": mean,
            "variance_value_bytes": sum((v["bytes"] - mean) ** 2 * v["weight"] for v in values) / total,
        }
    outputs["manifest.json"] = json_bytes(summary)
    return outputs, summary


def generate(input_path, output_path):
    with input_path.open("rb") as stream:
        data = stream.read(MAX_INPUT_BYTES + 1)
    outputs, summary = convert(data, input_path.name)
    # A new directory preserves previous experiment artifacts. Input validation
    # and serialization finish before any output path is created.
    output_path.mkdir(parents=True)
    for filename, contents in outputs.items():
        (output_path / filename).write_bytes(contents)
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True, help="Simulator payload_percentages.json (schema v1)")
    parser.add_argument("--out", type=Path, required=True, help="NEW directory for pb.json, cgpb.json, sb.json, and manifest.json")
    args = parser.parse_args(argv)
    try:
        summary = generate(args.input, args.out)
    except (OSError, ValueError) as error:
        parser.exit(2, f"generate_spanload_distributions: {error}\n")
    print(json.dumps(summary, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
