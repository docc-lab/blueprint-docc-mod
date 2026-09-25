#!/usr/bin/env python3
"""Tomislav-RetCtx: audit protobuf ramps and reproduce collector/sender comparisons."""
import csv
import json
from pathlib import Path
import re
import statistics
from types import SimpleNamespace

import yaml

from run_spanload_ramp import METRICS, combine_phases, epoch, summarize_step, write_json

ROOT = Path(__file__).resolve().parent
PROFILES = ("zero", "semconv10-example")
NAMES = {"none": "Vanilla", "pb": "PB", "cgpb": "CGPB", "sb": "SB"}
COLORS = {"none": "#333333", "pb": "#0072B2", "cgpb": "#D55E00", "sb": "#009E73"}
COUNTS = ("target_spans", "offered_spans", "attempted_spans", "acknowledged_spans", "queue_dropped_spans",
          "failed_spans", "rejected_spans", "unsent_spans", "scheduler_unissued_spans",
          "attempted_checkpoint_spans", "attempted_protobuf_bytes")


def read(path):
    return json.loads(path.read_text())


def counters(path):
    raw = path.read_text()
    return {name: sum(float(m.group(1)) for m in re.finditer(
        rf"^{name}(?:_total)?(?:\{{[^\n]*\}})?\s+([0-9.eE+-]+)", raw, re.M)) for name in METRICS}


def phases_under(paths):
    phases = []
    for path in paths:
        records = [json.loads(line) for line in (path / "results.jsonl").read_text().splitlines()]
        phase, = [r for r in records if r["type"] == "phase"]
        assert phase["offered_spans"] == phase["attempted_spans"] + phase["queue_dropped_spans"] + phase["unsent_spans"]
        assert phase["attempted_spans"] == phase["acknowledged_spans"] + phase["failed_spans"] + phase["rejected_spans"]
        assert phase["target_spans"] == phase["offered_spans"] + phase["scheduler_unissued_spans"]
        phases.append(phase)
    return phases


def check_deltas(before_path, after_path, phases):
    before, after = counters(before_path), counters(after_path)
    acknowledged = sum(p["acknowledged_spans"] for p in phases)
    for name in (METRICS[0], METRICS[2]):
        assert after[name] - before[name] == acknowledged, (before_path, name, after[name] - before[name], acknowledged)
    for name in (METRICS[1], METRICS[3]):
        assert after[name] == before[name]


def check_collector(directory):
    config = yaml.safe_load((directory / "collector.yaml").read_text())
    assert config["exporters"]["file"]["path"] == "/dev/null"
    assert config["exporters"]["file"]["format"] == "proto"
    assert config["service"]["pipelines"]["traces"] == {"receivers": ["otlp"], "processors": ["batch"], "exporters": ["file"]}
    final = read(directory / "container-final.json")
    assert final["HostConfig"]["NanoCpus"] == 1_000_000_000
    assert final["HostConfig"]["CpusetCpus"] == "2"
    assert final["HostConfig"]["Memory"] == final["HostConfig"]["MemorySwap"] == 4 * (1 << 30)
    assert "GOMAXPROCS=1" in final["Config"]["Env"]
    assert final["State"]["ExitCode"] == 0 and not final["State"]["OOMKilled"] and final["RestartCount"] == 0


def audit_ramps():
    summary, all_rows = {}, []
    for profile in PROFILES:
        root = ROOT / profile
        settings = read(root / "manifest.json")
        assert settings["export_format"] == "proto" and len(settings["generator_cpus"]) == 4
        old = read(ROOT / "json-baselines" / profile / "manifest.json")
        assert settings["generator_binary_sha256"] == old["generator_binary_sha256"]
        assert settings["collector_image"]["Id"] == old["collector_image"]["Id"]
        rows = read(root / "results.json")
        all_rows.extend(rows)
        assert all(read(root / "completion.json").values())
        variants = {}
        for variant in NAMES:
            directory = root / variant
            check_collector(directory)
            series = [r for r in rows if r["variant"] == variant]
            complete = read(directory / "completion.json")
            assert complete["saturated"] and complete["steps"] == len(series)
            totals, cp_bytes = dict.fromkeys(COUNTS, 0), 0
            for row in series:
                stem = f'{row["target_spans_per_second"]:09d}'
                paths = [directory / f"{stem}-generator-{i}" for i in range(4)]
                phases = phases_under(paths)
                check_deltas(directory / (stem + "-before.prom"), directory / (stem + "-after.prom"), phases)
                combined = combine_phases(phases)
                assert combined == read(directory / (stem + "-combined-phase.json"))
                samples = read(directory / (stem + "-samples.json"))
                recalculated = summarize_step(variant, row["target_spans_per_second"], combined, samples, SimpleNamespace(**settings))
                assert recalculated == row
                assert row["metric_scrape_errors"] == 0
                for path, phase in zip(paths, phases):
                    manifest = read(path / "manifest.json")
                    assert manifest["config"]["profile"] == profile
                    assert manifest["config"]["bridge"] == variant
                    if variant != "none":
                        assert manifest["config"]["checkpoint_fraction"] == settings["checkpoint_fraction"]
                    for key in totals:
                        totals[key] += phase[key]
                    cp_bytes += phase.get("attempted_checkpoint_payload", {}).get("total_bytes", 0)
            plateau = statistics.mean(r["exported_spans_per_second"] for r in series[-2:])
            assert plateau == complete["plateau_spans_per_second"]
            variants[variant] = {"plateau_spans_per_second": plateau,
                "plateau_point_range": sorted(r["exported_spans_per_second"] for r in series[-2:]),
                "plateau_offered_rates": [r["target_spans_per_second"] for r in series[-2:]],
                "collector_cpu_percent": statistics.mean(r["collector_cpu_cores"] for r in series[-2:]) * 100,
                "maximum_generator_cpu_cores_per_process": max(max(r["generator_cpu_cores_per_process"]) for r in series),
                "maximum_collector_rss_mib": max(r["collector_peak_rss_bytes"] for r in series) / (1 << 20),
                "steps": len(series), "totals": totals,
                "mean_checkpoint_value_bytes": cp_bytes / totals["attempted_checkpoint_spans"] if cp_bytes else None,
                "observed_checkpoint_fraction": totals["attempted_checkpoint_spans"] / totals["attempted_spans"]}
        for item in variants.values():
            item["reduction_vs_vanilla_percent"] = 100 * (1 - item["plateau_spans_per_second"] / variants["none"]["plateau_spans_per_second"])
        summary[profile] = variants
    write_json(ROOT / "SUMMARY.json", {"status": "passed", "export_format": "proto", "generator_processes": 4,
        "all_counters_reconciled": True, "profiles": summary})
    return all_rows, summary


def audit_capacity(root_name="capacity-checks", result_name="capacity-comparison.json"):
    root = ROOT / root_name
    rows = read(root / "results.json")
    assert read(root / "completion.json") == {"completed": len(rows), "all_counts_reconciled": True}
    for row in rows:
        directory = root / row["name"]
        check_collector(directory)
        phases = phases_under([directory / f"generator-{i}" for i in range(row["generators"])])
        check_deltas(directory / "before.prom", directory / "after.prom", phases)
        for key in ("acknowledged_spans", "queue_dropped_spans", "failed_spans", "rejected_spans", "unsent_spans", "scheduler_unissued_spans"):
            assert row[key] == sum(p[key] for p in phases)
        assert row["failed_spans"] == row["rejected_spans"] == 0
        config = yaml.safe_load((directory / "collector.yaml").read_text())
        for setting in ("send_batch_size", "send_batch_max_size"):
            assert config["processors"]["batch"][setting] == row.get("collector_batch", 512)
        if row.get("pprof"):
            assert (directory / "cpu.pprof").stat().st_size > 0
        samples = read(directory / "samples.json")
        start = max(epoch(p["started"]) for p in phases) + 5
        end = min(epoch(p["started"]) + p["generation_seconds"] for p in phases) - 1
        eligible = [s for s in samples if start <= s["time"] <= end and s["collector"] and all(s["generators"])]
        a, b = eligible[0], eligible[-1]
        duration = b["time"] - a["time"]
        assert row["accepted_spans_per_second"] == (b["counters"][METRICS[0]] - a["counters"][METRICS[0]]) / duration
        assert row["exported_spans_per_second"] == (b["counters"][METRICS[2]] - a["counters"][METRICS[2]]) / duration
        assert row["collector_cpu_cores"] == (b["collector"]["cpu_seconds"] - a["collector"]["cpu_seconds"]) / duration
        assert row["generator_cpu_cores"] == [(y["cpu_seconds"] - x["cpu_seconds"]) / duration
                                              for x, y in zip(a["generators"], b["generators"])]
    write_json(ROOT / result_name, rows)
    return rows


def plot(rows, capacity):
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    fig, axes = plt.subplots(2, 2, figsize=(11.5, 7), constrained_layout=True)
    for column, profile in enumerate(PROFILES):
        points = [r for r in rows if r["profile"] == profile]
        for variant in NAMES:
            series = [r for r in points if r["variant"] == variant]
            x = [r["target_spans_per_second"] / 1000 for r in series]
            axes[0, column].plot(x, [r["exported_spans_per_second"] / 1000 for r in series],
                                "o-", color=COLORS[variant], label=NAMES[variant])
            axes[1, column].plot(x, [r["collector_cpu_cores"] * 100 for r in series], "o-", color=COLORS[variant])
        top = max(r["target_spans_per_second"] for r in points) / 1000
        axes[0, column].plot([0, top], [0, top], "--", color="#999999", linewidth=1, label="Export = offered")
        axes[0, column].set_ylim(0, max(r["exported_spans_per_second"] for r in points) / 1000 * 1.12)
        axes[0, column].set_title("Zero semconv attributes" if profile == "zero" else "10 semconv attributes (example)")
        axes[0, column].set_ylabel("Export rate (thousand spans/s)")
        axes[0, column].legend(fontsize=9, loc="lower right")
        axes[1, column].axhline(100, linestyle="--", color="#999999", linewidth=1)
        axes[1, column].set_ylim(0, 110)
        axes[1, column].set_ylabel("Collector CPU (% of one core)")
        for axis in axes[:, column]:
            axis.set_xlabel("Aggregate offered rate (thousand spans/s)")
            axis.set_xlim(0, top * 1.05)
            axis.grid(alpha=.25)
    fig.suptitle("Protobuf file export to /dev/null · One collector, 1 CPU, 4 GiB\n"
                 "Four independent generators · Separate throughput scales")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(ROOT / ("ramps." + suffix), dpi=180)
    plt.close(fig)


def report(rows, summary, capacity, supplementary):
    lines = ["# Corrected protobuf collector ramps and sender capacity checks", "",
        "Tomislav-RetCtx: September 14, 2026. The initial fixture incorrectly used JSON file serialization. "
        "The intended test is one collector exporting protobuf to /dev/null. The generator and collector binaries "
        "are unchanged; the fixture now defaults to proto and the ramp records the export format explicitly.", "",
        "| Variant | Zero attributes (spans/s) | Ten attributes (spans/s) | Bridge penalty, zero | Bridge penalty, ten |",
        "| --- | ---: | ---: | ---: | ---: |"]
    comparison = []
    for variant in NAMES:
        a, b = [summary[p][variant] for p in PROFILES]
        lines.append(f'| {NAMES[variant]} | {a["plateau_spans_per_second"]:,.0f} | {b["plateau_spans_per_second"]:,.0f} | '
                     f'{a["reduction_vs_vanilla_percent"]:.2f}% | {b["reduction_vs_vanilla_percent"]:.2f}% |')
        comparison.append({"variant": variant, **{p + "_plateau_spans_per_second": summary[p][variant]["plateau_spans_per_second"] for p in PROFILES}})
    lines += ["", "Each plateau is the mean of two final overloaded points. Penalties compare against vanilla with "
        "the same attribute profile. These are single ramps; small differences between bridge types are not established rankings.", "",
        "[Ramp curves](ramps.png) · [PDF](ramps.pdf) · [SVG](ramps.svg) · [Per-step CSV](results.csv) · "
        "[Sender capacity curves](sender-capacity.png) · [Audited summary](SUMMARY.json)", "",
        "## Is the collector the limit?", "",
        "The separate controls hold one collector, one CPU, 4 GiB, protobuf export, payload profile, and aggregate offer "
        "fixed while increasing independent generator processes, connections, workers, and available sender cores. "
        "Each process has four separate physical cores, GOMAXPROCS=4, and eight RPC workers.", "",
        "| Profile / variant | 1 generator (spans/s) | 2 generators | 4 generators | Gain, 2 to 4 | Collector CPU with 4 |",
        "| --- | ---: | ---: | ---: | ---: | ---: |"]
    for profile in PROFILES:
        for variant in ("none", "sb"):
            points = {r["generators"]: r for r in capacity if r["profile"] == profile and r["bridge"] == variant}
            rates = [points[n]["exported_spans_per_second"] for n in (1, 2, 4)]
            label = ("Zero" if profile == "zero" else "Ten") + " / " + NAMES[variant]
            lines.append(f'| {label} | {rates[0]:,.0f} | {rates[1]:,.0f} | {rates[2]:,.0f} | '
                         f'{100*(rates[2]/rates[1]-1):.2f}% | {points[4]["collector_cpu_cores"]*100:.2f}% |')
    lines += ["", "With four senders, CPU is exhausted in the collector while substantial sender CPU remains available. "
        "Adding senders mostly increases RPC waiting time. There is a small feeding gap with one sender, especially "
        "for ten attributes; the corrected ramps use four senders throughout to close it. This evidence supports a "
        "collector limit without relying only on aggregate generator CPU.", "",
        "The preserved [R5 notes](capacity-checks/history/r5_overloaded_collector_throughput.md) report 1.17–1.23 million "
        "spans/s summed across nine one-core collectors, using an older two-default-attribute generator. Those notes "
        "are not evidence of that rate for a single collector. The exact historical single-collector protobuf run "
        "has not been recovered. JSON serialization was a concrete error in the new setup, independent of that historical ambiguity.", ""]
    lines += ["## Batch size and CPU profiles", "",
        "Separate four-generator controls changed only the collector's batch target and maximum from 512 to 8,192 spans. "
        "Generator requests remained at most 512 spans. Profiling was disabled for these comparisons.", "",
        "| Vanilla profile | Collector batch 512 (spans/s) | Collector batch 8,192 | Change |",
        "| --- | ---: | ---: | ---: |"]
    for profile in PROFILES:
        baseline, = [r for r in capacity if r["profile"] == profile and r["bridge"] == "none" and r["generators"] == 4]
        larger, = [r for r in supplementary if r["profile"] == profile and r.get("collector_batch") == 8192]
        a, b = baseline["exported_spans_per_second"], larger["exported_spans_per_second"]
        lines.append(f'| {"Zero" if profile == "zero" else "Ten attributes"} | {a:,.0f} | {b:,.0f} | {100*(b/a-1):+.2f}% |')
    lines += ["", "The larger batch brings zero-attribute vanilla close to one million spans/s, but does not improve the "
        "ten-attribute profile. These are single diagnostic controls, not repeated estimates. The preserved historical "
        "batch defaults are not sufficient to reconstruct the exact older single-collector experiment.", "",
        "Two additional controls sampled ten seconds of collector CPU with the loopback pprof extension. "
        "The collector accumulated 9.91s and 9.95s of CPU samples. The gRPC protobuf decoder's cumulative share was "
        "39.66% for zero attributes and 56.88% for ten; allocation and garbage collection also account for substantial work. "
        "These cumulative percentages overlap and must not be added. Serialization still occurs before writing to /dev/null. "
        "Profiling was disabled for the headline ramps and the sender-count comparisons.", "",
        "[Zero-attribute CPU profile](supplementary/zero-none-proto-1cpu-4gen-pprof/cpu-cumulative.txt) · "
        "[Ten-attribute CPU profile](supplementary/semconv10-example-none-proto-1cpu-4gen-pprof/cpu-cumulative.txt) · "
        "[Supplementary measurements](supplementary-comparison.json)", ""]
    totals = {key: sum(v["totals"][key] for p in summary.values() for v in p.values()) for key in COUNTS}
    lines += ["## Configuration and validation", "",
        "- One collector at a time; CPU 2, one CPU quota, 4 GiB, no swap, GOMAXPROCS=1, GOGC=100. CPU frequency is fixed at 2.2 GHz.",
        "- OTLP/gRPC loopback → batch (512 spans, 200ms) → file (format: proto, path: /dev/null, 200ms flush). Protobuf encoding remains in the measured path.",
        "- Four generators on CPUs 4–7, 14–17, 8–11, and 0/1/18/19; no collector physical-core overlap. "
        "Eight RPC workers and 64 queued batches per generator, at most 512 spans/RPC, 2s RPC timeout, 5s drain limit. "
        "Reported offered rates are aggregate, divided across the four processes.",
        "- Each generator has an independent wall-clock open-loop arrival scheduler. RPC workers wait for responses, "
        "but completion does not schedule arrivals. A full bounded queue drops and counts offered work. Consequently "
        "the configured offered rate can exceed attempted RPC traffic; collector acceptance/export rates measure delivered load.",
        "- The same random-CPD 2–8 PB0/CGP0/SB3 payload histograms, checkpoint share 0.5250588161787073, "
        "and seed 42 per process. These are marginal payload/ordinary mixtures, not a trace topology simulation. "
        "The ten-attribute profile is illustrative; exact values are archived in each profile directory.",
        "- Each point runs 25s. One-second raw samples use only the senders' common active interval, after a 5s discard "
        "and before the final second. Full-phase losses and byte counts are summed separately; checkpoint means are count-weighted. "
        "The collector persists throughout each ramp, and generators restart per step.",
        "- Zero-attribute rates: 100k, 400k, 800k, 1.2M, 1.6M as needed. Ten-attribute rates: 25k, 50k, 100k, 150k, 200k. "
        "Stop after two points at >=94% CPU, exports below 93% of target, and <12% throughput change.", "",
        f'All {len(rows)} ramp points, {len(capacity)} capacity controls, and {len(supplementary)} supplementary controls were independently recalculated. '
        f'The ramps acknowledged/exported {totals["acknowledged_spans"]:,} spans. '
        f'Full-phase generator queue drops: {totals["queue_dropped_spans"]:,}; failed spans: {totals["failed_spans"]:,}; '
        f'rejected spans: {totals["rejected_spans"]:,}; unsent spans: {totals["unsent_spans"]:,}; '
        f'scheduler-unissued spans: {totals["scheduler_unissued_spans"]:,}. '
        'Queue drops record backpressure at the bounded sender queue. All collector acceptance/export deltas match acknowledgements, '
        'with no receiver refusals or exporter failures.', "",
        "The Python accounting tests include staggered sender starts, shared measurement windows, and weighted payload means. "
        "All temporary collectors exited without OOMs/restarts and were removed. Existing applications and Kubernetes deployments were unchanged. "
        "Raw metrics, commands, source/binary/image provenance, manifests, and the earlier JSON baselines are retained. "
        "Run analyze.py to reproduce the audits, report, and figures.", ""]
    (ROOT / "RESULTS.md").write_text("\n".join(lines))
    write_json(ROOT / "comparison.json", comparison)
    with (ROOT / "results.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)


def plot_capacity(capacity):
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    fig, axes = plt.subplots(1, 2, figsize=(10.5, 4), constrained_layout=True)
    for column, profile in enumerate(PROFILES):
        for variant in ("none", "sb"):
            series = [r for r in capacity if r["profile"] == profile and r["bridge"] == variant]
            axes[column].plot([r["generators"] for r in series], [r["exported_spans_per_second"] / 1000 for r in series],
                              "o-", color=COLORS[variant], label=NAMES[variant])
        axes[column].set_title("Zero attributes" if profile == "zero" else "10 semconv attributes (example)")
        axes[column].set_xlabel("Independent generator processes")
        axes[column].set_ylabel("Export rate (thousand spans/s)")
        axes[column].set_xticks([1, 2, 4])
        peak = max(r["exported_spans_per_second"] for r in capacity if r["profile"] == profile) / 1000
        axes[column].set_ylim(0, peak * 1.10)
        axes[column].grid(alpha=.25)
        axes[column].legend()
    fig.suptitle("Sender capacity check · Collector fixed at 1 CPU, 4 GiB · Protobuf to /dev/null")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(ROOT / ("sender-capacity." + suffix), dpi=180)
    plt.close(fig)


if __name__ == "__main__":
    rows, summary = audit_ramps()
    capacity = audit_capacity()
    supplementary = audit_capacity("supplementary", "supplementary-comparison.json")
    plot(rows, capacity)
    plot_capacity(capacity)
    report(rows, summary, capacity, supplementary)
    print(json.dumps(summary, indent=2))
