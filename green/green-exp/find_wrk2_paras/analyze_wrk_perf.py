#!/usr/bin/env python3
import re
import os
from pathlib import Path

import matplotlib.pyplot as plt

# === CONFIG ===
RATES = list(range(800, 5001, 200))
WRK_DIR = Path("wrk_logs")
PERF_DIR = Path("perf_logs")
OUT_DIR = Path("plots")
OUT_DIR.mkdir(exist_ok=True)

# === HELPERS ===

def parse_time_with_unit(s: str) -> float:
    """
    Parse strings like '19.66ms', '3.74s', '521.26us' into milliseconds (float).
    """
    m = re.match(r"([0-9.]+)([a-zA-Z]+)", s.strip())
    if not m:
        raise ValueError(f"Cannot parse time string: {s}")
    val = float(m.group(1))
    unit = m.group(2)
    if unit.lower() in ("ms",):
        return val
    if unit.lower() in ("s", "sec"):
        return val * 1000.0
    if unit.lower() in ("us", "µs"):
        return val / 1000.0
    raise ValueError(f"Unknown time unit in: {s}")

def parse_wrk_file(path: Path) -> dict:
    """
    Extract metrics from a wrk2 output file.
    Returns:
        {
          "R_target": int,
          "lat_avg_ms": float,
          "lat_99_ms": float,
          "reqs_sec": float,
          "non_2xx": int or 0
        }
    """
    text = path.read_text()
    R_target = int(re.search(r"wrk_R(\d+)\.txt", path.name).group(1))

    lat_avg_ms = None
    lat_99_ms = None
    reqs_sec = None
    non_2xx = 0

    for line in text.splitlines():
        line_strip = line.strip()
        # Latency line under "Thread Stats"
        if line_strip.startswith("Latency"):
            # Example:
            # Latency    19.66ms    8.82ms  39.42ms   63.70%
            parts = line_strip.split()
            # parts[0] = "Latency"
            lat_avg_ms = parse_time_with_unit(parts[1])
            lat_99_ms = parse_time_with_unit(parts[3])
        # Overall Requests/sec line
        elif line_strip.startswith("Requests/sec:"):
            # Example: "Requests/sec:    998.58"
            m = re.search(r"Requests/sec:\s*([0-9.]+)", line_strip)
            if m:
                reqs_sec = float(m.group(1))
        # Non-2xx or 3xx responses
        elif line_strip.startswith("Non-2xx or 3xx responses:"):
            m = re.search(r"Non-2xx or 3xx responses:\s*(\d+)", line_strip)
            if m:
                non_2xx = int(m.group(1))

    if lat_avg_ms is None or lat_99_ms is None or reqs_sec is None:
        raise ValueError(f"Failed to parse wrk file {path}")

    return {
        "R_target": R_target,
        "lat_avg_ms": lat_avg_ms,
        "lat_99_ms": lat_99_ms,
        "reqs_sec": reqs_sec,
        "non_2xx": non_2xx,
    }

def parse_perf_file(path: Path) -> dict:
    """
    Parse perf stat -I 1000 -a --per-socket output.
    Assumes CSV with lines like:
        1.0010,S0,1,38.25,Joules,power/energy-pkg/,...
    Returns:
        {
          "P_S0_W": float,
          "P_S1_W": float,
          "P_total_W": float,
        }
    """
    if not path.exists():
        raise FileNotFoundError(path)
    # buckets: socket -> list of (dt, joules)
    # but -I1000 already gives energy over ~1s, so we can
    # approximate power as avg(joules) over intervals.
    # We'll still compute dt from timestamps to be a bit more accurate.
    per_socket = {}
    prev_time = {}

    with path.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            parts = line.split(",")
            if len(parts) < 4:
                continue
            t = float(parts[0])
            socket = parts[1]  # "S0", "S1"
            joules = float(parts[3])

            if socket not in per_socket:
                per_socket[socket] = []
                prev_time[socket] = t
            else:
                dt = t - prev_time[socket]
                prev_time[socket] = t
                if dt > 0:
                    per_socket[socket].append((dt, joules))

    powers = {}
    for sock, samples in per_socket.items():
        if not samples:
            continue
        # Average P = sum(J) / sum(dt)
        total_J = sum(j for dt, j in samples)
        total_dt = sum(dt for dt, j in samples)
        P = total_J / total_dt
        powers[sock] = P

    P_S0 = powers.get("S0", 0.0)
    P_S1 = powers.get("S1", 0.0)
    return {
        "P_S0_W": P_S0,
        "P_S1_W": P_S1,
        "P_total_W": P_S0 + P_S1,
    }

# === MAIN: collect metrics ===

results = []

for R in RATES:
    wrk_path = WRK_DIR / f"wrk_R{R}.txt"
    perf_path = PERF_DIR / f"perf_node3_R{R}.txt"

    if not wrk_path.exists():
        print(f"[WARN] Missing {wrk_path}, skipping")
        continue
    if not perf_path.exists():
        print(f"[WARN] Missing {perf_path}, skipping")
        continue

    wrk_metrics = parse_wrk_file(wrk_path)
    perf_metrics = parse_perf_file(perf_path)

    merged = {
        "R_target": R,
        **wrk_metrics,
        **perf_metrics,
    }
    results.append(merged)

# Sort by R
results.sort(key=lambda x: x["R_target"])

if not results:
    print("No results parsed. Check RATES and log paths.")
    exit(1)

# Print summary table
print(f"{'R':>6}  {'Req/s':>8}  {'AvgLat(ms)':>11}  {'P99(ms)':>8}  "
      f"{'Non-2xx':>7}  {'P_S0(W)':>8}  {'P_S1(W)':>8}  {'P_total(W)':>10}")
for r in results:
    print(f"{r['R_target']:6d}  {r['reqs_sec']:8.1f}  {r['lat_avg_ms']:11.2f}  "
          f"{r['lat_99_ms']:8.2f}  {r['non_2xx']:7d}  "
          f"{r['P_S0_W']:8.2f}  {r['P_S1_W']:8.2f}  {r['P_total_W']:10.2f}")

# === Build arrays for plotting ===
Rs = [r["R_target"] for r in results]
lat_avg = [r["lat_avg_ms"] for r in results]
lat_p99 = [r["lat_99_ms"] for r in results]
reqs_sec = [r["reqs_sec"] for r in results]
non2xx = [r["non_2xx"] for r in results]
P_S0 = [r["P_S0_W"] for r in results]
P_S1 = [r["P_S1_W"] for r in results]
P_total = [r["P_total_W"] for r in results]

# === Plot 1: Latency vs R ===
plt.figure()
plt.plot(Rs, lat_avg, marker="o", label="Avg latency (ms)")
plt.plot(Rs, lat_p99, marker="o", linestyle="--", label="P99 latency (ms)")
plt.xlabel("Target rate R (-R, requests/sec)")
plt.ylabel("Latency (ms)")
plt.title("Latency vs Target R")
plt.legend()
plt.grid(True)
plt.tight_layout()
plt.savefig(OUT_DIR / "latency_vs_R.png", dpi=200)

# === Plot 2: Achieved throughput vs R ===
plt.figure()
plt.plot(Rs, reqs_sec, marker="o")
plt.xlabel("Target rate R (-R, requests/sec)")
plt.ylabel("Achieved Requests/sec")
plt.title("Achieved throughput vs Target R")
plt.grid(True)
plt.tight_layout()
plt.savefig(OUT_DIR / "throughput_vs_R.png", dpi=200)

# === Plot 3: Power vs R ===
plt.figure()
plt.plot(Rs, P_S0, marker="o", label="Socket 0")
plt.plot(Rs, P_S1, marker="o", label="Socket 1")
plt.plot(Rs, P_total, marker="o", linestyle="--", label="Total")
plt.xlabel("Target rate R (-R, requests/sec)")
plt.ylabel("Average power (W)")
plt.title("Node3 power vs Target R")
plt.legend()
plt.grid(True)
plt.tight_layout()
plt.savefig(OUT_DIR / "power_vs_R.png", dpi=200)

# === Plot 4 (optional): Non-2xx vs R (overload indicator) ===
plt.figure()
plt.plot(Rs, non2xx, marker="o")
plt.xlabel("Target rate R (-R, requests/sec)")
plt.ylabel("Non-2xx/3xx responses (count in 60s)")
plt.title("Errors vs Target R")
plt.grid(True)
plt.tight_layout()
plt.savefig(OUT_DIR / "errors_vs_R.png", dpi=200)

print(f"\nPlots written to: {OUT_DIR.resolve()}")
