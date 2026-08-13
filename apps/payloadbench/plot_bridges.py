#!/usr/bin/env python3
"""Plot the tracing-bridge ramps: does baggage on the call path cost anything?

LAYOUT — rows are the two campaigns, then the answer:
  row 1  p-bridge campaign      control vs pb            (mean latency, log y)
  row 2  cg/s-bridge campaign   control vs cgpb vs sb    (mean latency, log y)
  row 3  paired Δ vs each arm's OWN control              (the result)
Columns are the three SOSP'23 base sizes.

The two campaigns are plotted SEPARATELY rather than pooled: they ran in
different sessions, and every delta is computed within its own campaign (and
within a round). Overlaying absolute curves across sessions would confound the
bridge with the rebuild -- the same error the paired design exists to avoid.

Encoding: the control is NEUTRAL GRAY -- it is a baseline, not a fourth identity
-- and the three bridges take categorical slots 1-3 (blue/orange/aqua), which
validate all-pairs in both modes. Palette checked with the skill's six checks
(ported to Python; this cluster has no node): worst all-pairs CVD ΔE 9.2
(target >=8), worst normal-vision ΔE 21.8 (floor >=15). Aqua sits at 2.74
contrast on the light surface, below the 3.0 minimum, so the RELIEF RULE
applies: every series is direct-labeled, not identified by color alone.

Latency panels use log y: the range spans 0.4 ms to seconds, and a linear axis
would flatten the entire pre-knee region -- which is the region under test.

Usage: plot_bridges.py <pb_dir> <cgsb_dir> [out.png]
"""
import glob, math, os, re, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

# categorical slots 1-3 + neutral baseline (see module docstring for validation)
CTRL = "#52514e"
COLOR = {"pb": "#2a78d6", "cgpb": "#eb6834", "sb": "#1baf7a"}
LABEL = {"none": "no bridge", "pb": "p-bridge", "cgpb": "cg-bridge", "sb": "s-bridge"}
INK, INK2, INK3 = "#0b0b0b", "#52514e", "#8a8981"
SURFACE = "#fcfcfb"
GRID = "#e6e5e0"

SIZES = [("p10", "p10 — 200 B request", 40000),
         ("p50", "p50 — 1536 B request", 34000),
         ("p90", "p90 — 12 KB request", 8000)]


def parse(p):
    t = open(p).read()
    a = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not (a and m):
        return None
    return {"ach": float(a.group(1)), "mean": float(m.group(1)), "sd": float(m.group(2))}


def load(d, arm):
    """-> {round: {rps: rec}}"""
    o = {}
    for f in glob.glob(os.path.join(d, arm, "round_*", "step_*.txt")):
        if "superseded" in f or "INVALID" in f:
            continue
        rd = int(re.search(r"round_(\d+)", f).group(1))
        r = parse(f)
        if r:
            o.setdefault(rd, {})[int(re.search(r"step_(\d+)", f).group(1))] = r
    return o


def msd(v):
    n = len(v)
    if not n:
        return float("nan"), 0.0
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0)


def style(ax, xlab, ylab, title):
    ax.set_facecolor(SURFACE)
    for s in ("top", "right"):
        ax.spines[s].set_visible(False)
    for s in ("left", "bottom"):
        ax.spines[s].set_color(INK3); ax.spines[s].set_linewidth(0.8)
    ax.grid(True, color=GRID, linewidth=0.8, zorder=0)
    ax.set_axisbelow(True)
    ax.tick_params(colors=INK2, labelsize=9, length=3, width=0.8)
    ax.set_title(title, fontsize=10.5, color=INK, pad=7, loc="left")
    ax.set_xlabel(xlab, fontsize=9, color=INK2)
    ax.set_ylabel(ylab, fontsize=9, color=INK2)


def ramp(ax, d, arms, key="mean"):
    """Absolute ramp: round-averaged curve per arm, error bars = between-round sd."""
    for arm in arms:
        rounds = load(d, arm)
        if not rounds:
            continue
        allr = sorted({r for v in rounds.values() for r in v})
        xs, ys, es = [], [], []
        for rps in allr:
            v = [rounds[rd][rps][key] for rd in rounds if rps in rounds[rd]]
            if not v:
                continue
            mu, sd = msd(v)
            xs.append(rps); ys.append(mu); es.append(sd)
        br = arm.split("_")[1]
        col = CTRL if br == "none" else COLOR[br]
        lo = [min(e, y * 0.9) for e, y in zip(es, ys)]   # keep log axis valid
        ctl_arm = br == "none"
        ax.errorbar(xs, ys, yerr=[lo, es], color=col,
                    linewidth=3.4 if ctl_arm else 1.7,
                    marker="o", markersize=5.5 if ctl_arm else 3.4,
                    capsize=2, elinewidth=0.9, alpha=1.0 if ctl_arm else 0.95,
                    markeredgecolor=SURFACE, markeredgewidth=0.8,
                    label=LABEL[br], zorder=2 if ctl_arm else 3)
    ax.set_yscale("log")


def delta(ax, pairs, cap):
    """Paired Δmean% vs own control, computed WITHIN each round then averaged."""
    ax.axhline(0, color=INK3, linewidth=1, linestyle=(0, (4, 3)), zorder=1)
    for d, sz, br in pairs:
        ctl, brd = load(d, f"{sz}_none"), load(d, f"{sz}_{br}")
        rounds = sorted(set(ctl) & set(brd))
        allr = sorted({r for rd in rounds for r in set(ctl[rd]) & set(brd[rd])})
        xs, ys, es = [], [], []
        for rps in allr:
            if rps > cap:
                continue
            v = [100 * (brd[rd][rps]["mean"] - ctl[rd][rps]["mean"]) / ctl[rd][rps]["mean"]
                 for rd in rounds if rps in ctl[rd] and rps in brd[rd]]
            if not v:
                continue
            mu, sd = msd(v)
            xs.append(rps); ys.append(mu); es.append(sd)
        if not xs:
            continue
        ax.errorbar(xs, ys, yerr=es, color=COLOR[br], linewidth=1.8, marker="o",
                    markersize=4, capsize=2, elinewidth=0.9,
                    markeredgecolor=SURFACE, markeredgewidth=0.8,
                    label=LABEL[br], zorder=3)


def kfmt(ax, hi):
    step = 12000 if hi > 30000 else (2000 if hi <= 14000 else 8000)
    ticks = list(range(0, int(hi) + 1, step))
    ax.set_xticks(ticks)
    ax.set_xticklabels([f"{v//1000}k" for v in ticks])


def main(pbd, cgd, out=None):
    out = out or "bridges_ramps.png"
    fig, axes = plt.subplots(3, 3, figsize=(13.4, 11.2), facecolor=SURFACE)
    fig.suptitle("Tracing-bridge baggage on the call path — request-side bytes only, cpd 6",
                 fontsize=13.5, color=INK, y=0.982)

    for col, (sz, sztitle, cap) in enumerate(SIZES):
        # row 1 — p-bridge campaign
        ax = axes[0][col]
        style(ax, "Offered load (requests/sec)", "Mean response time (ms)" if col == 0 else "",
              f"{sztitle}  ·  p-bridge run")
        ramp(ax, pbd, [f"{sz}_none", f"{sz}_pb"])
        kfmt(ax, ax.get_xlim()[1])

        # row 2 — cg/s-bridge campaign
        ax = axes[1][col]
        style(ax, "Offered load (requests/sec)", "Mean response time (ms)" if col == 0 else "",
              f"{sztitle}  ·  cg/s-bridge run")
        ramp(ax, cgd, [f"{sz}_none", f"{sz}_cgpb", f"{sz}_sb"])
        kfmt(ax, ax.get_xlim()[1])

        # row 3 — the answer
        ax = axes[2][col]
        style(ax, "Offered load (requests/sec)",
              "Δ mean latency vs control (%)" if col == 0 else "",
              f"{sztitle}  ·  paired Δ (pre-knee)")
        delta(ax, [(pbd, sz, "pb"), (cgd, sz, "cgpb"), (cgd, sz, "sb")], cap)
        ax.set_xlim(0, cap * 1.06)
        kfmt(ax, cap)
        ax.set_ylim(-17, 20)

    # RELIEF RULE (aqua is below 3:1 on this surface): every series direct-labeled,
    # so identity never rests on color alone. Legend is present as well.
    # Labels go UPPER-LEFT: every panel's curves start low-left and rise to the
    # right, so that corner is the one reliably empty region.
    for row, arms in ((0, ("none", "pb")), (1, ("none", "cgpb", "sb")), (2, ("pb", "cgpb", "sb"))):
        ax = axes[row][2]
        for i, br in enumerate(arms):
            c = CTRL if br == "none" else COLOR[br]
            ax.annotate(LABEL[br], xy=(0.03, 0.96 - 0.09 * i), xycoords="axes fraction",
                        fontsize=8.5, color=c, ha="left", va="top", weight="bold")

    h, l = axes[1][0].get_legend_handles_labels()
    h2, l2 = axes[0][0].get_legend_handles_labels()
    for hh, ll in zip(h2, l2):
        if ll not in l:
            h.append(hh); l.append(ll)
    fig.legend(h, l, loc="lower center", ncol=4, frameon=False, fontsize=10,
               labelcolor=INK2, bbox_to_anchor=(0.5, 0.055))
    fig.text(0.5, 0.037,
             "Rows 1–2: round-averaged ramps; error bars = between-round sd (n=2). "
             "Row 3: Δ computed within each round vs that campaign's own control, then averaged.",
             ha="center", fontsize=8.5, color=INK3)
    fig.text(0.5, 0.017,
             "Campaigns are never pooled — each bridge is compared only to the control beside it. "
             "Arm order counterbalanced (r1 control-first, r2 bridge-first).",
             ha="center", fontsize=8.5, color=INK3)
    fig.tight_layout(rect=[0, 0.085, 1, 0.968])
    fig.savefig(out, dpi=150, facecolor=SURFACE)
    print(f"wrote {out}")

    # numeric companion so the figure is never the only record
    print(f"\n{'cell':<14} {'Δmean%':>9} {'spread':>8} {'steps':>6}")
    for sz, _, cap in SIZES:
        for d, br in ((pbd, "pb"), (cgd, "cgpb"), (cgd, "sb")):
            ctl, brd = load(d, f"{sz}_none"), load(d, f"{sz}_{br}")
            rounds = sorted(set(ctl) & set(brd))
            v = []
            for rps in sorted({r for rd in rounds for r in set(ctl[rd]) & set(brd[rd])}):
                if rps > cap:
                    continue
                v += [100 * (brd[rd][rps]["mean"] - ctl[rd][rps]["mean"]) / ctl[rd][rps]["mean"]
                      for rd in rounds if rps in ctl[rd] and rps in brd[rd]]
            if v:
                mu, sd = msd(v)
                print(f"{sz+'/'+br:<14} {mu:>+8.2f}% {sd:>7.2f} {len(v):>6}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else None)
