#!/usr/bin/env python3
import pandas as pd
import matplotlib.pyplot as plt

TRACING_DIR = "power_runs/tracing"
NO_TRACING_DIR = "power_runs/no_tracing"
NODES = ["node-0", "node-2", "node-3"]


def load_power_series(path: str) -> pd.DataFrame:
    return pd.read_csv(
        path,
        header=None,
        usecols=[0, 1],
        names=["time_s", "power_W"],
    )


def main() -> None:
    plt.figure(figsize=(9, 5))

    for node in NODES:
        tracing_path = f"{TRACING_DIR}/{node}_perf.csv"
        no_tracing_path = f"{NO_TRACING_DIR}/{node}_perf.csv"

        df_tr = load_power_series(tracing_path)
        df_no = load_power_series(no_tracing_path)

        # Assume same length and ordering; compute diff by index
        min_len = min(len(df_tr), len(df_no))
        df_tr = df_tr.iloc[:min_len].reset_index(drop=True)
        df_no = df_no.iloc[:min_len].reset_index(drop=True)

        time_s = df_tr["time_s"].to_numpy()
        power_diff = (df_tr["power_W"] - df_no["power_W"]).to_numpy()

        plt.plot(
            time_s,
            power_diff,
            marker="o",
            linewidth=1.5,
            label=node,
        )

    plt.axhline(0.0, color="black", linewidth=1, alpha=0.6)
    plt.xlabel("Time (s)")
    plt.ylabel("Power diff (W)  [tracing − no tracing]")
    plt.title("Per-node power difference over 70 seconds")
    plt.grid(True, alpha=0.3)
    plt.legend(title="Node")
    plt.tight_layout()
    plt.savefig("power_diff_tracing_vs_no_tracing.png", dpi=200)
    # plt.show()


if __name__ == "__main__":
    main()