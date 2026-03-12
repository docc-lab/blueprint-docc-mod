#!/usr/bin/env python3
import pandas as pd
import matplotlib.pyplot as plt

# Paths to your CSVs
paths = {
    "node-0": "power_runs/tracing/node-0_perf.csv",
    "node-2": "power_runs/tracing/node-2_perf.csv",
    "node-3": "power_runs/tracing/node-3_perf.csv",
}

plt.figure(figsize=(9, 5))

for label, path in paths.items():
    # CSV has no header
    # col0 ~ time in seconds since start (1..70),
    # col1 = energy over that 1-second interval (Joules) ≈ average power in Watts.
    df = pd.read_csv(
        path,
        header=None,
        usecols=[0, 1],
        names=["time_s", "power_W"],
    )

    plt.plot(
        df["time_s"].to_numpy(),
        df["power_W"].to_numpy(),
        marker="o",
        linewidth=1.5,
        label=label,
    )

plt.xlabel("Time (s)")
plt.ylabel("Power (W)")
plt.title("Node power consumption tracing-on")
plt.grid(True, alpha=0.3)
plt.legend()
plt.tight_layout()
plt.savefig("with_tracing.png", dpi=200)
# Or comment the line above and uncomment the next to show interactively:
# plt.show()