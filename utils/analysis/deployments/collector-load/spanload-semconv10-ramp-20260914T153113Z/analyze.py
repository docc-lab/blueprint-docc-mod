#!/usr/bin/env python3
"""Tomislav-RetCtx: audit the semconv ramp and reproduce its baseline comparison."""

import csv
import json
from pathlib import Path
import re
import statistics
from types import SimpleNamespace

from run_spanload_ramp import METRICS, plot_results, summarize_step, write_json


ROOT = Path(__file__).resolve().parent
NAMES = {"none": "Vanilla", "pb": "PB", "cgpb": "CGPB", "sb": "SB"}
COLORS = {"none": "#333333", "pb": "#0072B2", "cgpb": "#D55E00", "sb": "#009E73"}


def read(path):
    return json.loads(path.read_text())


def counter(path, name):
    return sum(float(m.group(1)) for m in re.finditer(
        rf"^{name}(?:_total)?(?:\{{[^\n]*\}})?\s+([0-9.eE+-]+)", path.read_text(), re.M))


def audit():
    rows = read(ROOT / "results.json")
    settings = read(ROOT / "manifest.json")
    baseline = read(ROOT / "zero-baseline/manifest.json")
    for key in ("generator_binary_sha256", "checkpoint_fraction", "collector_cpu",
                "generator_cpus", "workers", "seconds", "discard_first", "rates"):
        assert settings[key] == baseline[key], key
    assert settings["collector_image"]["Id"] == baseline["collector_image"]["Id"]
    assert settings["profile"] == "semconv10-example"
    assert all(read(ROOT / "completion.json").values())
    expected_attributes = read(ROOT / "semconv10-example.json")
    variants = {}
    for variant in NAMES:
        directory = ROOT / variant
        series = [row for row in rows if row["variant"] == variant]
        completion = read(directory / "completion.json")
        assert completion["saturated"] and completion["steps"] == len(series)
        final = read(directory / "container-final.json")
        assert final["State"]["ExitCode"] == 0 and not final["State"]["OOMKilled"]
        assert final["RestartCount"] == 0
        assert final["HostConfig"]["NanoCpus"] == 1_000_000_000
        assert final["HostConfig"]["Memory"] == 4 * (1 << 30)
        totals = dict.fromkeys(("target_spans", "offered_spans", "attempted_spans", "acknowledged_spans",
            "queue_dropped_spans", "failed_spans", "rejected_spans", "unsent_spans",
            "scheduler_unissued_spans", "attempted_checkpoint_spans", "attempted_protobuf_bytes"), 0)
        payload_bytes = 0
        for row in series:
            stem = f'{row["target_spans_per_second"]:09d}'
            step = directory / stem
            records = [json.loads(line) for line in (step / "results.jsonl").read_text().splitlines()]
            phase, = [r for r in records if r["type"] == "phase"]
            assert phase["offered_spans"] == phase["attempted_spans"] + phase["queue_dropped_spans"] + phase["unsent_spans"]
            assert phase["attempted_spans"] == phase["acknowledged_spans"] + phase["rejected_spans"] + phase["failed_spans"]
            assert phase["target_spans"] == phase["offered_spans"] + phase["scheduler_unissued_spans"]
            for name in (METRICS[0], METRICS[2]):
                delta = counter(step / "001-after-00.prom", name) - counter(step / "001-before-00.prom", name)
                assert delta == phase["acknowledged_spans"], (variant, stem, name, delta, phase["acknowledged_spans"])
            for name in (METRICS[1], METRICS[3]):
                assert counter(step / "001-after-00.prom", name) == counter(step / "001-before-00.prom", name)
            manifest = read(step / "manifest.json")
            assert manifest["config"]["profile"] == "semconv10-example"
            assert manifest["resolved_profile"]["span_attributes"] == expected_attributes
            assert manifest["resolved_profile"]["resource_attributes"] == []
            if variant != "none":
                assert manifest["config"]["checkpoint_fraction"] == settings["checkpoint_fraction"]
                assert manifest["config"]["payload_seed"] == 42
            samples = read(directory / (stem + "-samples.json"))
            recalculated = summarize_step(variant, row["target_spans_per_second"], phase, samples,
                                          SimpleNamespace(**settings))
            assert recalculated == row, (variant, stem, "steady-window mismatch")
            assert row["metric_scrape_errors"] == 0
            for key in totals:
                totals[key] += phase[key]
            cp = phase.get("attempted_checkpoint_payload")
            if cp:
                payload_bytes += cp["total_bytes"]
        plateau = statistics.mean(row["exported_spans_per_second"] for row in series[-2:])
        assert plateau == completion["plateau_spans_per_second"]
        variants[variant] = {
            "plateau_spans_per_second": plateau,
            "plateau_point_range": sorted(row["exported_spans_per_second"] for row in series[-2:]),
            "plateau_offered_rates": [row["target_spans_per_second"] for row in series[-2:]],
            "collector_cpu_percent": statistics.mean(row["collector_cpu_cores"] for row in series[-2:]) * 100,
            "maximum_generator_cpu_cores": max(row["generator_cpu_cores"] for row in series),
            "maximum_collector_rss_mib": max(row["collector_peak_rss_bytes"] for row in series) / (1 << 20),
            "steps": len(series), "totals": totals,
            "observed_checkpoint_fraction": totals["attempted_checkpoint_spans"] / totals["attempted_spans"],
            "observed_mean_checkpoint_value_bytes": payload_bytes / totals["attempted_checkpoint_spans"] if payload_bytes else None,
            "mean_protobuf_bytes_per_span": totals["attempted_protobuf_bytes"] / totals["attempted_spans"],
        }
    for item in variants.values():
        item["reduction_vs_vanilla_percent"] = 100 * (1 - item["plateau_spans_per_second"] / variants["none"]["plateau_spans_per_second"])
    summary = {"status": "passed", "profile": settings["profile"], "all_counters_reconciled": True,
               "all_containers_exited_cleanly": True, "same_binary_and_collector_as_zero": True, "variants": variants}
    write_json(ROOT / "SUMMARY.json", summary)
    return rows, summary


def comparison(rows, summary):
    baseline = read(ROOT / "zero-baseline/SUMMARY.json")
    table = []
    for variant in NAMES:
        old, new = baseline["variants"][variant], summary["variants"][variant]
        table.append({"variant": variant,
            "zero_plateau_spans_per_second": old["plateau_spans_per_second"],
            "semconv10_plateau_spans_per_second": new["plateau_spans_per_second"],
            "zero_reduction_vs_vanilla_percent": old["reduction_vs_vanilla_percent"],
            "semconv10_reduction_vs_vanilla_percent": new["reduction_vs_vanilla_percent"],
            "semconv_reduction_vs_own_zero_percent": 100 * (1 - new["plateau_spans_per_second"] / old["plateau_spans_per_second"]),
            "zero_collector_cpu_microseconds_per_span": old["collector_cpu_percent"] * 10000 / old["plateau_spans_per_second"],
            "semconv10_collector_cpu_microseconds_per_span": new["collector_cpu_percent"] * 10000 / new["plateau_spans_per_second"],
        })
    write_json(ROOT / "comparison.json", table)
    with (ROOT / "comparison.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(table[0]))
        writer.writeheader()
        writer.writerows(table)
    plot_results(rows, ROOT, "semconv10-example")
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    fig, axes = plt.subplots(2, 2, figsize=(11.5, 7), constrained_layout=True)
    for column, (title, points) in enumerate([
        ("Zero semconv attributes", read(ROOT / "zero-baseline/results.json")),
        ("10 semconv attributes (example profile)", rows),
    ]):
        for variant in NAMES:
            series = [r for r in points if r["variant"] == variant]
            x = [r["target_spans_per_second"] / 1000 for r in series]
            axes[0, column].plot(x, [r["exported_spans_per_second"] / 1000 for r in series],
                                 "o-", color=COLORS[variant], label=NAMES[variant])
            axes[1, column].plot(x, [r["collector_cpu_cores"] * 100 for r in series],
                                 "o-", color=COLORS[variant])
        top = max(r["target_spans_per_second"] for r in points) / 1000
        axes[0, column].plot([0, top], [0, top], "--", color="#999999", linewidth=1, label="Export = offered")
        axes[0, column].set_ylim(0, max(r["exported_spans_per_second"] for r in points) / 1000 * 1.12)
        axes[0, column].set_title(title)
        axes[0, column].set_ylabel("Export rate (thousand spans/s)")
        axes[0, column].legend(fontsize=9, loc="lower right")
        axes[1, column].axhline(100, linestyle="--", color="#999999", linewidth=1)
        axes[1, column].set_ylim(0, 110)
        axes[1, column].set_ylabel("Collector CPU (% of one core)")
        for axis in axes[:, column]:
            axis.set_xlabel("Offered rate (thousand spans/s)")
            axis.set_xlim(0, top * 1.05)
            axis.grid(alpha=.25)
    fig.suptitle("Collector saturation ramps · 1 CPU, 4 GiB · JSON export to /dev/null\n"
                 "Same binaries and bridge distributions · Separate throughput scales")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(ROOT / ("comparison." + suffix), dpi=180)
    plt.close(fig)
    return table


def report(rows, summary, table):
    variants = summary["variants"]
    lines = ["# JSON collector saturation with ten semconv attributes", "",
        "**Tomislav-RetCtx correction:** this run used JSON file serialization. The intended experiment uses "
        "protobuf serialization (`format: proto`); these numbers describe the JSON pipeline only.", "",
        "Tomislav-RetCtx: measured September 14, 2026 using the illustrative `semconv10-example` profile. "
        "All four ramps reached a CPU-limited export plateau. The generator binary, collector image, resource "
        "limits, bridge histograms, checkpoint share, and pacing settings match the earlier zero-attribute run.", "",
        "| Variant | Zero attributes (spans/s) | Ten attributes (spans/s) | Bridge penalty, zero | Bridge penalty, ten |",
        "| --- | ---: | ---: | ---: | ---: |"]
    for item in table:
        lines.append(f'| {NAMES[item["variant"]]} | {item["zero_plateau_spans_per_second"]:,.0f} | '
            f'{item["semconv10_plateau_spans_per_second"]:,.0f} | {item["zero_reduction_vs_vanilla_percent"]:.2f}% | '
            f'{item["semconv10_reduction_vs_vanilla_percent"]:.2f}% |')
    lines += ["", "Bridge penalty is the reduction in plateau throughput relative to vanilla with the same attribute profile. "
        "Plateaus average the final two overloaded steps. Adding semconv lowers absolute capacity substantially; "
        "bridge metadata contributes a smaller fraction of the total processing cost.", "",
        f'Effective collector CPU time per exported vanilla span rises from '
        f'{table[0]["zero_collector_cpu_microseconds_per_span"]:.2f} to '
        f'{table[0]["semconv10_collector_cpu_microseconds_per_span"]:.2f} microseconds. '
        'This is process CPU divided by export rate at the plateau, not a breakdown from profiling. '
        'The larger baseline cost explains how a similar incremental bridge cost can produce a smaller percentage throughput penalty.', "",
        "[Comparison curves (PNG)](comparison.png) · [PDF](comparison.pdf) · [SVG](comparison.svg) · "
        "[Semconv ramp](ramp.png) · [Per-step CSV](results.csv) · [Comparison CSV](comparison.csv) · "
        "[Audited summary](SUMMARY.json)", "",
        "## Saturation evidence", "",
        "| Variant | Final offered rates (spans/s) | Export range (spans/s) | Collector CPU | Max generator cores | Max collector RSS (MiB) |",
        "| --- | ---: | ---: | ---: | ---: | ---: |"]
    for variant, item in variants.items():
        low, high = item["plateau_point_range"]
        offered = " / ".join(f"{r:,}" for r in item["plateau_offered_rates"])
        lines.append(f'| {NAMES[variant]} | {offered} | {low:,.0f}–{high:,.0f} | '
            f'{item["collector_cpu_percent"]:.2f}% | {item["maximum_generator_cpu_cores"]:.2f} | '
            f'{item["maximum_collector_rss_mib"]:.1f} |')
    lines += ["", "## Configuration", "",
        "- One fresh collector per variant on node-0: CPU 2, one CPU quota, 4 GiB memory, no swap, GOMAXPROCS=1, GOGC=100. "
        "Frequency min/max 2.2 GHz, turbo disabled. The generator uses separate physical cores 4–7, GOMAXPROCS=4, GOGC=100.",
        "- OTLP/gRPC loopback → batch processor (512 spans, 200ms) → JSON file exporter to /dev/null (200ms flush). "
        "JSON serialization is in the measured path; this directly measures the collector pipeline.",
        "- Ten typed span attributes, listed exactly in [semconv10-example.json](semconv10-example.json). "
        "Vanilla carries ten attributes; PB/CGPB/SB carry eleven including _br or ordinary _d/_o. "
        "Resource attributes, scope metadata, events, links, and operation name remain empty. The profile is an example; "
        "the exact ten-attribute profile from the paper has not been recovered.",
        "- PB0/CGP0/SB3 conditional byte distributions from the supplied Uber day 1 random-CPD 2–8 histogram, "
        "with its 4-byte key/type contribution subtracted. Every source bin, including the SB tail, remains eligible.",
        "- Checkpoint share 0.5250588161787073, sampled independently from payload size using seed 42. "
        "This reproduces the marginal checkpoint/payload mix; it does not simulate trace topology. "
        "Ordinary metadata uses synthetic depth 6 and, for SB, ordinal 1.",
        "- Eight RPC workers, at most 512 spans/request, 64 queued batches, 2s RPC timeout, 5s maximum drain. "
        "Rates are aggregate spans/s, starting at 10k then 25k, 50k, 75k, 100k, 150k and higher only if required.",
        "- Each rate runs for 25s with a fresh generator; the collector persists across that variant's steps. "
        "One-second process/Prometheus samples are measured after the first 5s and before the last second; drain is excluded.",
        "- Stop after two points with at least 94% collector CPU, export below 93% of target, and less than 12% throughput change. "
        "These are single ramps, without replication-based confidence intervals. Small differences between bridge types "
        "should not be interpreted as established rankings.", "", "## Accounting and reproducibility", ""]
    aggregate = {key: sum(item["totals"][key] for item in variants.values()) for key in variants["none"]["totals"]}
    lines += [f'All {len(rows)} steady-window rows were recomputed from raw samples. Every phase reconciled '
        'offered = attempted + queue-dropped + unsent, attempted = acknowledged + rejected + failed, and '
        'target = offered + scheduler-unissued. Collector acceptance and export deltas matched acknowledged spans exactly. '
        f'Total acknowledged/exported: {aggregate["acknowledged_spans"]:,}.', "",
        f'Full-phase counts: {aggregate["queue_dropped_spans"]:,} generator queue drops, '
        f'{aggregate["failed_spans"]:,} failed spans, {aggregate["rejected_spans"]:,} rejected spans, '
        f'{aggregate["unsent_spans"]:,} unsent spans, and {aggregate["scheduler_unissued_spans"]:,} scheduler-unissued spans. '
        'The collector reported no receiver refusals or exporter failures. Queue drops record offered traffic that '
        'could not enter the bounded generator queue under collector backpressure; they are not collector refusals.', "",
        'The ramp now waits for both the metrics and OTLP listener before generating. This addresses the initial '
        'connection-refused RPCs in the earlier zero run; those earlier RPCs were outside its measured steady window. '
        'No generator or collector binary change was needed for semconv.', "",
        'The Python accounting tests passed; native dry runs confirmed the exact embedded profile and ten/eleven '
        'wire attributes. Per-step commands, manifests, raw counters, process samples, source snapshots, binary/image '
        'hashes, and frequency/resource records are retained. [analyze.py](analyze.py) rechecks the accounting and '
        'recreates the reports/figures using the saved zero-baseline data. Full earlier raw data remains at '
        '[the zero run](../spanload-zero-ramp-20260914T144131Z/RESULTS.md).', "",
        'All temporary collectors exited cleanly without OOMs/restarts and were removed. Existing application '
        'implementations and deployments were unchanged.', ""]
    (ROOT / "RESULTS.md").write_text("\n".join(lines))


if __name__ == "__main__":
    rows, summary = audit()
    table = comparison(rows, summary)
    report(rows, summary, table)
    print(json.dumps({"steps": len(rows), "comparison": table, "summary": summary}, indent=2))
