#!/usr/bin/env python3
import re
from pathlib import Path
import numpy as np
import matplotlib.pyplot as plt

# ============================================================
# CONFIG
# ============================================================
DATA_DIR = Path("perf_runs")   # directory containing perf_node-*_iterNNN.txt
OUT_DIR = Path("plots")
OUT_DIR.mkdir(exist_ok=True)

NODES = ["node-0", "node-2", "node-3"]

# Colors per node:
COLORS = {
    "node-0": "#1f77b4",   # blue
    "node-2": "#ff7f0e",   # orange
    "node-3": "#2ca02c",   # green
}

# ============================================================
# PARSE PERF FILES
# ============================================================

def parse_perf_file(path: Path):
    """
    Parse a single perf output file.
    Returns list of (dt, joules) samples for both S0 and S1 merged.
    """
    samples = []
    prev_time = None

    for line in path.read_text().splitlines():
        parts = line.split(",")
        if len(parts) < 4:
            continue
        t = float(parts[0])
        joules = float(parts[3])

        if prev_time is None:
            prev_time = t
            continue

        dt = t - prev_time
        prev_time = t
        if dt > 0:
            samples.append((dt, joules))

    return samples


def compute_power_stats(samples):
    """
    Given samples = [(dt, joules), ...], compute:
    - mean power
    - min instantaneous power
    - max instantaneous power
    """
    if not samples:
        return None, None, None

    inst_power = [j / dt for dt, j in samples]
    time_total = sum(dt for dt, j in samples)
    energy_total = sum(j for dt, j in samples)

    mean_power = energy_total / time_total
    min_power = min(inst_power)
    max_power = max(inst_power)

    return mean_power, min_power, max_power


# ============================================================
# COLLECT POWER STATS ACROSS ITERATIONS
# ============================================================

stats = {node: {"mean": [], "min": [], "max": []} for node in NODES}

for node in NODES:
    for file in DATA_DIR.glob(f"perf_{node}_iter*.txt"):
        samples = parse_perf_file(file)
        mean_p, min_p, max_p = compute_power_stats(samples)
        if mean_p is not None:
            stats[node]["mean"].append(mean_p)
            stats[node]["min"].append(min_p)
            stats[node]["max"].append(max_p)

# ============================================================
# PLOTTER WITH COLORS
# ============================================================

def colored_boxplot(data_dict, title, filename):
    """
    data_dict: { node: [values] }
    """
    labels = list(data_dict.keys())
    data = [data_dict[n] for n in labels]
    colors = [COLORS[n] for n in labels]

    fig, ax = plt.subplots(figsize=(9,6))

    bp = ax.boxplot(
        data,
        patch_artist=True,
        labels=labels,
        boxprops=dict(linewidth=1.2),
        medianprops=dict(color="black", linewidth=1.2),
        whiskerprops=dict(linewidth=1.2),
        capprops=dict(linewidth=1.2)
    )

    # apply node-specific colors
    for patch, color in zip(bp['boxes'], colors):
        patch.set_facecolor(color)
        patch.set_alpha(0.55)

    ax.set_title(title)
    ax.set_ylabel("Power (W)")
    ax.grid(True, linestyle="--", alpha=0.4)

    plt.tight_layout()
    plt.savefig(OUT_DIR / filename, dpi=200)
    plt.close()


# ============================================================
# BUILD PLOTS
# ============================================================

colored_boxplot(
    {n: stats[n]["mean"] for n in NODES},
    "Mean Power per Node",
    "box_mean_power.png"
)

colored_boxplot(
    {n: stats[n]["min"] for n in NODES},
    "Min Power per Node",
    "box_min_power.png"
)

colored_boxplot(
    {n: stats[n]["max"] for n in NODES},
    "Max Power per Node",
    "box_max_power.png"
)

print(f"Plots written to {OUT_DIR.resolve()}")
