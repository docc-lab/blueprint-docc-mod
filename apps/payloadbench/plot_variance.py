#!/usr/bin/env python3
"""Plot the payload-size-variance ramps: mean, p99, SD (sqrt variance) and CV.

Encoding note: the three arms are ORDINAL (ordered by CV_size 0 -> 1.0 -> 3.31),
not nominal, so they take a ONE-HUE ramp with monotone lightness rather than
separate categorical hues -- the reader sees the ordering in the color, and
lightness ordering is preserved under every form of colour-vision deficiency.
Steps are taken from the validated reference blue ramp (300/500/700).

Usage: plot_variance.py <rampvar_dir> [out.png]
"""
import glob, math, os, re, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

# ordinal one-hue ramp (validated reference blue: steps 300, 500, 700)
# ordinal ramp, 4 monotone lightness steps (reference blue 250/400/550/700)
ARMS = [
    ("fixed",   "fixed (CV$_{size}$=0)",         "#86b6ef"),
    ("unif10",  "uniform ±10% (CV$_{size}$=0.06)", "#3987e5"),
    ("exp",     "exponential (CV$_{size}$=1.0)",  "#1c5cab"),
    ("bimodal", "bimodal (CV$_{size}$=3.31)",     "#0d366b"),
]
INK, INK2, INK3 = "#0b0b0b", "#52514e", "#8a8981"
SURFACE = "#fcfcfb"
# 28k is EXCLUDED: every arm is past its knee there (unif10 mean 24.6ms with a
# +-21ms between-round sd -- one round collapsed), so the column is noise, not signal.
# The knee region is near-vertical; a small rps offset swings latency by 10x.
LO, HI = 4000, 26000


def parse(p):
    t = open(p).read()
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not m:
        return None
    d = {"mean": float(m.group(1)), "sd": float(m.group(2))}
    q = re.search(r"^\s*99\.000%\s+([0-9.]+)(us|ms|s|m)\b", t, re.M)
    if not q:
        return None
    d["p99"] = float(q.group(1)) * {"us": 1e-3, "ms": 1, "s": 1e3, "m": 6e4}[q.group(2)]
    return d


def msd(v):
    n = len(v)
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0)


def main(d, out=None):
    out = out or os.path.join(d, "variance_ramps.png")
    data = {}
    for arm, _, _ in ARMS:
        for f in glob.glob(os.path.join(d, arm, "round_*", "step_*.txt")):
            rps = int(re.search(r"step_(\d+)", f).group(1))
            if not (LO <= rps <= HI):
                continue
            p = parse(f)
            if p:
                data.setdefault(arm, {}).setdefault(rps, []).append(p)

    panels = [
        ("mean", "Mean response time (ms)", "Mean latency"),
        ("p99", "p99 response time (ms)", "Tail latency (p99)"),
        ("sd", "Std. deviation (ms)", r"Latency spread (SD = $\sqrt{variance}$)"),
        ("cv", "Coefficient of variation", "Relative spread (CV = SD / mean)"),
    ]
    fig, axes = plt.subplots(2, 2, figsize=(12.5, 8.4), facecolor=SURFACE)
    fig.suptitle("Payload-size variance vs response time — matched mean payload (4250 B), n=3",
                 fontsize=13.5, color=INK, x=0.5, y=0.985, ha="center")

    for ax, (key, ylab, title) in zip(axes.ravel(), panels):
        ax.set_facecolor(SURFACE)
        for spine in ("top", "right"):
            ax.spines[spine].set_visible(False)
        for spine in ("left", "bottom"):
            ax.spines[spine].set_color(INK3)
            ax.spines[spine].set_linewidth(0.8)
        ax.grid(True, color="#e6e5e0", linewidth=0.8, zorder=0)
        ax.set_axisbelow(True)
        ax.tick_params(colors=INK2, labelsize=9.5, length=3, width=0.8)
        ax.set_title(title, fontsize=11, color=INK, pad=8, loc="left")
        ax.set_xlabel("Offered load (requests/sec)", fontsize=9.5, color=INK2)
        ax.set_ylabel(ylab, fontsize=9.5, color=INK2)

        for arm, label, color in ARMS:
            xs = sorted(data.get(arm, {}))
            ys, es = [], []
            for x in xs:
                rs = data[arm][x]
                if key == "cv":
                    v = [r["sd"] / r["mean"] for r in rs]
                else:
                    v = [r[key] for r in rs]
                mu, sd = msd(v)
                ys.append(mu); es.append(sd)
            lo_err = [min(e, y) for e, y in zip(es, ys)]   # clip at 0: SD/CV/latency >= 0
            ax.errorbar(xs, ys, yerr=[lo_err, es], color=color, linewidth=2, marker="o",
                        markersize=4.5, capsize=2.5, elinewidth=1,
                        markeredgecolor=SURFACE, markeredgewidth=0.8,
                        label=label, zorder=3)
            # selective direct label: last point only
            if xs:
                dy = {"fixed": -11, "unif10": 3, "exp": 13, "bimodal": -1}[arm]
                ax.annotate(label.split(" (")[0], (xs[-1], ys[-1]),
                            textcoords="offset points", xytext=(7, dy),
                            fontsize=8.5, color=INK2, va="center")
        ax.set_ylim(bottom=0)
        ax.set_xlim(LO - 1200, HI + 6200)
        ax.ticklabel_format(axis="x", style="plain")
        ax.set_xticks(range(4000, HI + 1, 4000))
        ax.set_xticklabels([f"{v//1000}k" for v in range(4000, HI + 1, 4000)])
        ax.set_xlim(LO - 1200, HI + 7000)

    for ax, (key, _, _) in zip(axes.ravel(), panels):
        if key in ("sd", "cv"):
            ax.annotate("one anomalous fixed-arm round\n(kept, not excluded)",
                        xy=(20000, 1.98 if key == "sd" else 1.62),
                        xytext=(13200, 3.6 if key == "sd" else 2.9),
                        fontsize=7.5, color=INK3, ha="left",
                        arrowprops=dict(arrowstyle="-", color=INK3, linewidth=0.7))
    h, l = axes[0][0].get_legend_handles_labels()
    leg = fig.legend(h, l, loc="lower center", ncol=4, frameon=False,
                     fontsize=9.5, labelcolor=INK2, bbox_to_anchor=(0.5, 0.005))
    fig.text(0.5, 0.052,
             "All arms: mean payload 4250 B both directions; only the size distribution differs. "
             "Error bars = between-round sd (n=3). 28k excluded: all arms past the knee there.",
             ha="center", fontsize=8.5, color=INK3)
    fig.tight_layout(rect=[0, 0.085, 1, 0.965])
    fig.savefig(out, dpi=160, facecolor=SURFACE)
    print(f"wrote {out}")
    # numeric companion so the figure is never the only record
    print(f"\n{'offered':>8}", end="")
    for _, lab, _ in ARMS:
        print(f" | {lab.split(' (')[0]:>26}", end="")
    print()
    for x in sorted(data.get("fixed", {})):
        print(f"{x:>8}", end="")
        for arm, _, _ in ARMS:
            rs = data.get(arm, {}).get(x, [])
            if not rs:
                print(f" | {'--':>26}", end=""); continue
            mu, _ = msd([r["mean"] for r in rs])
            p9, _ = msd([r["p99"] for r in rs])
            sd, _ = msd([r["sd"] for r in rs])
            print(f" | mean{mu:6.3f} p99{p9:7.3f} sd{sd:6.3f}", end="")
        print()


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else None)
