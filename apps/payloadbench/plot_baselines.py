#!/usr/bin/env python3
"""Plot the three-size payload baselines (SOSP'23 Fig 6 p10 / p50 / p90).

Encoding: the three sizes are ORDINAL (392 B < 1856 B < 22000 B round trip), so they
take a ONE-HUE ramp with monotone lightness rather than three separate hues -- the
reader sees the ordering in the colour, and lightness ordering survives every form of
colour-vision deficiency. Steps from the validated reference blue ramp (250/450/700).

Latency panels use a LOG y-axis: the measured range spans 0.5 ms to ~5 s, and a linear
axis would flatten the entire pre-knee region into the baseline.

Usage: plot_baselines.py <baselines_dir> [out.png]
"""
import glob, math, os, re, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

SIZES = [
    ("p10", "p10:  200 B / 192 B", 392,   "#86b6ef"),
    ("p50", "p50: 1536 B / 320 B", 1856,  "#2a78d6"),
    ("p90", "p90:  12 KB / 10 KB", 22000, "#0d366b"),
]
INK, INK2, INK3 = "#0b0b0b", "#52514e", "#8a8981"
SURFACE = "#fcfcfb"


def parse(p):
    t = open(p).read()
    a = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not (a and m):
        return None
    d = {"ach": float(a.group(1)), "mean": float(m.group(1)), "sd": float(m.group(2))}
    q = re.search(r"^\s*99\.000%\s+([0-9.]+)(us|ms|s|m)\b", t, re.M)
    d["p99"] = (float(q.group(1)) * {"us": 1e-3, "ms": 1, "s": 1e3, "m": 6e4}[q.group(2)]) if q else None
    return d


def msd(v):
    n = len(v)
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0), n


def main(d, out=None):
    out = out or os.path.join(d, "baselines.png")
    data = {}
    for key, _, _, _ in SIZES:
        for f in glob.glob(os.path.join(d, key, "round_*", "step_*.txt")):
            if "superseded" in f or "INVALID" in f:
                continue
            rps = int(re.search(r"step_(\d+)", f).group(1))
            p = parse(f)
            if p:
                data.setdefault(key, {}).setdefault(rps, []).append(p)

    fig, axes = plt.subplots(2, 2, figsize=(12.6, 8.6), facecolor=SURFACE)
    fig.suptitle("PayloadBench baselines — RPC sizes from Seemakhupt et al., SOSP '23 (Google fleet, Fig 6)",
                 fontsize=13, color=INK, y=0.985)

    panels = [
        ("ach",  "Achieved throughput (rps)", "Throughput — where each size saturates", False),
        ("mbps", "Round-trip payload (MB/s)", "Byte throughput", False),
        ("mean", "Mean response time (ms)",   "Mean latency", True),
        ("p99",  "p99 response time (ms)",    "Tail latency (p99)", True),
    ]
    for ax, (key, ylab, title, logy) in zip(axes.ravel(), panels):
        ax.set_facecolor(SURFACE)
        for sp in ("top", "right"):
            ax.spines[sp].set_visible(False)
        for sp in ("left", "bottom"):
            ax.spines[sp].set_color(INK3); ax.spines[sp].set_linewidth(0.8)
        ax.grid(True, color="#e6e5e0", linewidth=0.8, zorder=0); ax.set_axisbelow(True)
        ax.tick_params(colors=INK2, labelsize=9.5, length=3, width=0.8)
        ax.set_title(title, fontsize=11, color=INK, pad=8, loc="left")
        ax.set_xlabel("Offered load (requests/sec)", fontsize=9.5, color=INK2)
        ax.set_ylabel(ylab, fontsize=9.5, color=INK2)
        if logy:
            ax.set_yscale("log")

        for skey, label, rt, color in SIZES:
            xs = sorted(data.get(skey, {}))
            if not xs:
                continue
            ys, es = [], []
            for x in xs:
                rs = data[skey][x]
                if key == "mbps":
                    v = [r["ach"] * rt / 1e6 for r in rs]
                elif key == "p99":
                    v = [r["p99"] for r in rs if r["p99"] is not None]
                else:
                    v = [r[key] for r in rs]
                if not v:
                    ys.append(float("nan")); es.append(0); continue
                mu, sd, _ = msd(v)
                ys.append(mu); es.append(sd)
            lo = [min(e, y * 0.95) if y > 0 else 0 for e, y in zip(es, ys)]  # keep log axis valid
            ax.errorbar(xs, ys, yerr=[lo, es], color=color, linewidth=2, marker="o",
                        markersize=4, capsize=2.5, elinewidth=1,
                        markeredgecolor=SURFACE, markeredgewidth=0.8, label=label, zorder=3)
        ax.set_xlim(0, 74000)
        ax.set_xticks(range(0, 72001, 12000))
        ax.set_xticklabels([f"{v//1000}k" for v in range(0, 72001, 12000)])
        if key == "ach":   # y=x reference: perfect tracking of offered load
            ax.plot([0, 72000], [0, 72000], color=INK3, linewidth=1,
                    linestyle=(0, (4, 3)), zorder=1)
            ax.annotate("achieved = offered", (46000, 46000), fontsize=8, color=INK3,
                        rotation=31, ha="center", va="bottom")

    h, l = axes[0][0].get_legend_handles_labels()
    fig.legend(h, l, loc="lower center", ncol=3, frameon=False, fontsize=10,
               labelcolor=INK2, bbox_to_anchor=(0.5, 0.005))
    fig.text(0.5, 0.052,
             "Fixed sizes, asymmetric (paper: median response/request ratio < 1). "
             "8-core limits, natural GC, single replica, -c 128. Error bars = between-round sd (n=3).",
             ha="center", fontsize=8.5, color=INK3)
    fig.tight_layout(rect=[0, 0.085, 1, 0.965])
    fig.savefig(out, dpi=160, facecolor=SURFACE)
    print(f"wrote {out}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else None)
