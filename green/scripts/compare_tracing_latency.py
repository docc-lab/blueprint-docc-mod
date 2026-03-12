#!/usr/bin/env python3
from __future__ import annotations

import re
from pathlib import Path
from typing import Dict, List, Tuple

import matplotlib.pyplot as plt

TRACING_PATH = "power_runs/tracing/wrk2.log"
NO_TRACING_PATH = "power_runs/no_tracing/wrk2.log"
SUMMARY_TXT_PATH = "wrk_latency_summary_tracing_vs_no_tracing.txt"


def to_ms(value: float, unit: str) -> float:
    unit = unit.strip().lower()
    if unit == "ms":
        return value
    if unit == "s":
        return value * 1000.0
    raise ValueError(f"Unsupported unit: {unit}")


def parse_wrk_file(path: str) -> Dict:
    text = Path(path).read_text()

    result: Dict = {
        "path": path,
        "label": Path(path).parent.name,
        "avg_ms": None,
        "stdev_ms": None,
        "p99_thread_ms": None,
        "requests_per_sec": None,
        "transfer_per_sec": None,
        "non_2xx_3xx": 0,
        "percentiles": [],
    }

    # Thread Stats latency line
    m = re.search(
        r"Latency\s+([0-9.]+)(ms|s)\s+([0-9.]+)(ms|s)\s+([0-9.]+)(ms|s)",
        text,
    )
    if m:
        result["avg_ms"] = to_ms(float(m.group(1)), m.group(2))
        result["stdev_ms"] = to_ms(float(m.group(3)), m.group(4))
        result["p99_thread_ms"] = to_ms(float(m.group(5)), m.group(6))

    # Overall Requests/sec
    m = re.search(r"Requests/sec:\s+([0-9.]+)", text)
    if m:
        result["requests_per_sec"] = float(m.group(1))

    # Transfer/sec
    m = re.search(r"Transfer/sec:\s+([0-9.]+)([KMG]?B)", text)
    if m:
        result["transfer_per_sec"] = f"{m.group(1)}{m.group(2)}"

    # Non-2xx or 3xx responses
    m = re.search(r"Non-2xx or 3xx responses:\s+(\d+)", text)
    if m:
        result["non_2xx_3xx"] = int(m.group(1))

    # Latency Distribution block
    dist_block = re.search(
        r"Latency Distribution \(HdrHistogram - Recorded Latency\)\n(.*?)(?:\n\n|\n  Detailed Percentile spectrum:)",
        text,
        re.DOTALL,
    )
    if dist_block:
        lines = dist_block.group(1).strip().splitlines()
        for line in lines:
            m = re.search(r"([0-9.]+)%\s+([0-9.]+)(ms|s)", line.strip())
            if m:
                pct = float(m.group(1))
                val_ms = to_ms(float(m.group(2)), m.group(3))
                result["percentiles"].append((pct, val_ms))

    if not result["percentiles"]:
        raise ValueError(f"Could not parse percentile distribution from {path}")

    return result


def pretty_label(label: str) -> str:
    s = label.lower()
    if "no" in s and "tracing" in s:
        return "No tracing"
    if "tracing" in s:
        return "Tracing"
    return label


def values_for_selected_percentiles(
    percentiles: List[Tuple[float, float]],
    selected: List[float],
) -> List[float]:
    mapping = {p: v for p, v in percentiles}
    return [mapping[p] for p in selected]


def build_summary_table(tracing: Dict, no_tracing: Dict) -> str:
    a = tracing
    b = no_tracing

    selected = [50.0, 75.0, 90.0, 99.0, 99.9, 100.0]
    a_map = {p: v for p, v in a["percentiles"]}
    b_map = {p: v for p, v in b["percentiles"]}

    lines: List[str] = []
    lines.append("Summary (latency in ms)")
    lines.append("-" * 64)
    lines.append(
        f"{'Metric':<10}"
        f"{'Tracing':>16}"
        f"{'No tracing':>16}"
        f"{'Delta':>16}"
    )
    lines.append("-" * 64)

    # Avg
    lines.append(
        f"{'Avg':<10}"
        f"{a['avg_ms']:>16.2f}"
        f"{b['avg_ms']:>16.2f}"
        f"{(a['avg_ms'] - b['avg_ms']):>16.2f}"
    )

    # Percentiles
    for p in selected:
        lines.append(
            f"{('p' + str(p)): <10}"
            f"{a_map[p]:>16.2f}"
            f"{b_map[p]:>16.2f}"
            f"{(a_map[p] - b_map[p]):>16.2f}"
        )

    # Req/sec
    lines.append(
        f"{'Req/sec':<10}"
        f"{a['requests_per_sec']:>16.2f}"
        f"{b['requests_per_sec']:>16.2f}"
        f"{(a['requests_per_sec'] - b['requests_per_sec']):>16.2f}"
    )

    # Non-2xx/3xx
    lines.append(
        f"{'Non-2xx':<10}"
        f"{a['non_2xx_3xx']:>16d}"
        f"{b['non_2xx_3xx']:>16d}"
        f"{(a['non_2xx_3xx'] - b['non_2xx_3xx']):>16d}"
    )

    lines.append("-" * 64)
    return "\n".join(lines)


def print_summary(tracing: Dict, no_tracing: Dict) -> None:
    table = build_summary_table(tracing, no_tracing)
    print("\n" + table)
    Path(SUMMARY_TXT_PATH).write_text(table + "\n", encoding="utf-8")


def plot_percentile_curve(tracing: Dict, no_tracing: Dict, out_path: str) -> None:
    fig, ax = plt.subplots(figsize=(8.5, 5.2))

    ax.plot(
        [p for p, _ in tracing["percentiles"]],
        [v for _, v in tracing["percentiles"]],
        marker="o",
        linewidth=2,
        label=pretty_label(tracing["label"]),
    )
    ax.plot(
        [p for p, _ in no_tracing["percentiles"]],
        [v for _, v in no_tracing["percentiles"]],
        marker="o",
        linewidth=2,
        label=pretty_label(no_tracing["label"]),
    )

    ax.set_title("wrk latency comparison by percentile (tracing vs no tracing)")
    ax.set_xlabel("Percentile")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks([50, 75, 90, 99, 99.9, 99.99, 100])
    ax.grid(True, alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out_path, dpi=200)
    plt.close(fig)


def plot_summary_bars(tracing: Dict, no_tracing: Dict, out_path: str) -> None:
    selected = [50.0, 75.0, 90.0, 99.0, 100.0]
    a_vals = values_for_selected_percentiles(tracing["percentiles"], selected)
    b_vals = values_for_selected_percentiles(no_tracing["percentiles"], selected)

    labels = ["p50", "p75", "p90", "p99", "max"]
    x = list(range(len(labels)))
    width = 0.36

    fig, ax = plt.subplots(figsize=(8.5, 5.2))
    ax.bar(
        [i - width / 2 for i in x],
        a_vals,
        width=width,
        label=pretty_label(tracing["label"]),
    )
    ax.bar(
        [i + width / 2 for i in x],
        b_vals,
        width=width,
        label=pretty_label(no_tracing["label"]),
    )

    ax.set_title("wrk latency summary (tracing vs no tracing)")
    ax.set_xlabel("Metric")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(x)
    ax.set_xticklabels(labels)
    ax.grid(True, axis="y", alpha=0.3)
    ax.legend()

    for i, val in enumerate(a_vals):
        ax.text(i - width / 2, val, f"{val:.1f}", ha="center", va="bottom", fontsize=8)
    for i, val in enumerate(b_vals):
        ax.text(i + width / 2, val, f"{val:.1f}", ha="center", va="bottom", fontsize=8)

    fig.tight_layout()
    fig.savefig(out_path, dpi=200)
    plt.close(fig)


def main() -> None:
    tracing = parse_wrk_file(TRACING_PATH)
    no_tracing = parse_wrk_file(NO_TRACING_PATH)

    print_summary(tracing, no_tracing)
    plot_percentile_curve(
        tracing,
        no_tracing,
        "wrk_latency_percentiles_tracing_vs_no_tracing.png",
    )
    plot_summary_bars(
        tracing,
        no_tracing,
        "wrk_latency_summary_tracing_vs_no_tracing.png",
    )

    print("\nWrote:")
    print("  wrk_latency_percentiles_tracing_vs_no_tracing.png")
    print("  wrk_latency_summary_tracing_vs_no_tracing.png")
    print(f"  {SUMMARY_TXT_PATH}")


if __name__ == "__main__":
    main()