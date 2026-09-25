# Tomislav-RetCtx: reproduce saved ramp figures without running load.
import json
from pathlib import Path

def plot_results(rows, out):
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.4), constrained_layout=True)
    colors = {"none": "#333333", "pb": "#0072B2", "cgpb": "#D55E00", "sb": "#009E73"}
    names = {"none": "Vanilla", "pb": "PB", "cgpb": "CGPB", "sb": "SB"}
    for variant in colors:
        series = [r for r in rows if r["variant"] == variant]
        if not series:
            continue
        x = [r["target_spans_per_second"] / 1000 for r in series]
        axes[0].plot(x, [r["exported_spans_per_second"] / 1000 for r in series], "o-", color=colors[variant], label=names[variant])
        axes[1].plot(x, [r["collector_cpu_cores"] * 100 for r in series], "o-", color=colors[variant], label=names[variant])
    top = max(r["target_spans_per_second"] for r in rows) / 1000
    axes[0].plot([0, top], [0, top], linestyle="--", color="#999999", linewidth=1, label="Export = offered")
    axes[1].axhline(100, color="#999999", linestyle="--", linewidth=1)
    for axis in axes:
        axis.set_xlabel("Offered rate (thousand spans/s)")
        axis.grid(alpha=.25)
        axis.set_xlim(left=0)
        axis.set_ylim(bottom=0)
    axes[0].set_ylabel("Collector export rate (thousand spans/s)")
    axes[0].set_ylim(0, max(r["exported_spans_per_second"] for r in rows) / 1000 * 1.12)
    axes[1].set_ylabel("Collector CPU (% of one core)")
    axes[0].legend(fontsize=9)
    axes[1].set_ylim(0, 110)
    fig.suptitle("Zero semconv attributes · 1 collector CPU, 4 GiB · JSON export to /dev/null")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(out / ("ramp." + suffix), dpi=180)
    plt.close(fig)


if __name__ == "__main__":
    root = Path(__file__).resolve().parent
    plot_results(json.loads((root / "results.json").read_text()), root)
