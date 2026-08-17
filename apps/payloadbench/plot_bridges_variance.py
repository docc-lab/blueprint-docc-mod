#!/usr/bin/env python3
"""Latency VARIANCE through the ramp, for every bridge configuration.

Companion to plot_bridges.py (which plots the mean). The hypothesis under test is
about spread, not level: s-bridge's baggage has CV_size 2.86, so if heavy-tailed
metadata inflates response-time variance it must show up here.

LAYOUT — rows are the metric/campaign, columns are the three SOSP'23 base sizes:
  row 1  SD  — p-bridge run        (control vs pb)                log y
  row 2  SD  — cg/s-bridge run     (control vs cgpb vs sb)        log y
  row 3  CV = SD/mean — ALL arms, both campaigns                  linear
  row 4  paired Δ SD% vs each arm's OWN control                   linear

Rows 1-2 keep the campaigns separate because absolute SD in ms is session-
dependent -- pooling it across a rebuild would confound bridge with session, the
error the paired design exists to avoid.

Row 3 deliberately DOES put both campaigns on one axis, because CV is
self-normalising: dividing by that arm's own mean removes the level, so a CV
curve is far more robust to session drift than absolute ms. Both controls are
drawn, and their agreement is itself the evidence that pooling is safe here.

SD is plotted, not variance: variance is in ms^2, spans ~6 orders of magnitude
across a ramp, and is not comparable to the latency axis. SD is the same quantity
in the units the reader already has (and is what wrk reports).

Usage: plot_bridges_variance.py <pb_dir> <cgsb_dir> [out.png]
"""
import glob, math, os, re, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

CTRL = "#52514e"
COLOR = {"pb": "#2a78d6", "cgpb": "#eb6834", "sb": "#1baf7a"}
LABEL = {"none": "no bridge", "pb": "p-bridge", "cgpb": "cg-bridge", "sb": "s-bridge"}
INK, INK2, INK3 = "#0b0b0b", "#52514e", "#8a8981"
SURFACE = "#fcfcfb"
GRID = "#e6e5e0"

# (key, title, pre-knee cap, Δ-SD y-limit). p90 needs a WIDER limit: it has only
# 4-5 pre-knee steps below a ~13k knee and its paired ΔSD genuinely reaches +46%
# with rounds disagreeing 7x (+134% vs +18% at one step). Clipping that to the
# other panels' scale would hide the disagreement rather than show it.
SIZES = [("p10", "p10 — 200 B request", 40000, 32),
         ("p50", "p50 — 1536 B request", 34000, 32),
         ("p90", "p90 — 12 KB request", 8000, 80)]


def parse(p):
    t = open(p).read()
    a = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not (a and m):
        return None
    return {"ach": float(a.group(1)), "mean": float(m.group(1)), "sd": float(m.group(2))}


def load(d, arm):
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


def series(ax, d, arm, metric, cap=None, dashed=False, suffix="", logy=True):
    rounds = load(d, arm)
    if not rounds:
        return
    xs, ys, es = [], [], []
    for rps in sorted({r for v in rounds.values() for r in v}):
        if cap and rps > cap:
            continue
        v = []
        for rd in rounds:
            if rps not in rounds[rd]:
                continue
            rec = rounds[rd][rps]
            v.append(rec["sd"] / rec["mean"] if metric == "cv" else rec["sd"])
        if not v:
            continue
        mu, sd = msd(v)
        xs.append(rps); ys.append(mu); es.append(sd)
    if not xs:
        return
    br = arm.split("_")[1]
    ctl_arm = br == "none"
    col = CTRL if ctl_arm else COLOR[br]
    lo = [min(e, y * 0.9) if logy else min(e, y) for e, y in zip(es, ys)]
    ax.errorbar(xs, ys, yerr=[lo, es], color=col,
                linewidth=3.2 if ctl_arm else 1.7,
                linestyle=(0, (5, 2)) if dashed else "-",
                marker="o", markersize=5.2 if ctl_arm else 3.4,
                capsize=2, elinewidth=0.9,
                markeredgecolor=SURFACE, markeredgewidth=0.8,
                label=LABEL[br] + suffix, zorder=2 if ctl_arm else 3)


def delta_sd(ax, pairs, cap):
    ax.axhline(0, color=INK3, linewidth=1, linestyle=(0, (4, 3)), zorder=1)
    for d, sz, br in pairs:
        ctl, brd = load(d, f"{sz}_none"), load(d, f"{sz}_{br}")
        rounds = sorted(set(ctl) & set(brd))
        xs, ys, es = [], [], []
        for rps in sorted({r for rd in rounds for r in set(ctl[rd]) & set(brd[rd])}):
            if rps > cap:
                continue
            v = [100 * (brd[rd][rps]["sd"] - ctl[rd][rps]["sd"]) / ctl[rd][rps]["sd"]
                 for rd in rounds if rps in ctl[rd] and rps in brd[rd] and ctl[rd][rps]["sd"]]
            if not v:
                continue
            mu, sd = msd(v)
            xs.append(rps); ys.append(mu); es.append(sd)
        if xs:
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
    out = out or "bridges_variance.png"
    fig, axes = plt.subplots(4, 3, figsize=(13.4, 14.8), facecolor=SURFACE)
    fig.suptitle("Latency variance through the ramp — every bridge configuration (cpd 6, request-side baggage)",
                 fontsize=13.5, color=INK, y=0.988)

    for c, (sz, sztitle, cap, dylim) in enumerate(SIZES):
        ax = axes[0][c]
        style(ax, "", "Std. deviation (ms)" if c == 0 else "", f"{sztitle}  ·  SD, p-bridge run")
        for arm in (f"{sz}_none", f"{sz}_pb"):
            series(ax, pbd, arm, "sd")
        ax.set_yscale("log"); kfmt(ax, ax.get_xlim()[1])

        ax = axes[1][c]
        style(ax, "", "Std. deviation (ms)" if c == 0 else "", f"{sztitle}  ·  SD, cg/s-bridge run")
        for arm in (f"{sz}_none", f"{sz}_cgpb", f"{sz}_sb"):
            series(ax, cgd, arm, "sd")
        ax.set_yscale("log"); kfmt(ax, ax.get_xlim()[1])

        # CV pools both campaigns: self-normalising, so session level cancels.
        ax = axes[2][c]
        style(ax, "", "CV  (SD / mean)" if c == 0 else "", f"{sztitle}  ·  relative spread (CV)")
        series(ax, pbd, f"{sz}_none", "cv", cap=cap, dashed=True, suffix=" (pb run)", logy=False)
        series(ax, pbd, f"{sz}_pb", "cv", cap=cap, logy=False)
        series(ax, cgd, f"{sz}_none", "cv", cap=cap, suffix=" (cg/sb run)", logy=False)
        series(ax, cgd, f"{sz}_cgpb", "cv", cap=cap, logy=False)
        series(ax, cgd, f"{sz}_sb", "cv", cap=cap, logy=False)
        ax.set_ylim(bottom=0); ax.set_xlim(0, cap * 1.06); kfmt(ax, cap)

        ax = axes[3][c]
        style(ax, "Offered load (requests/sec)", "Δ SD vs control (%)" if c == 0 else "",
              f"{sztitle}  ·  paired Δ SD (pre-knee)")
        delta_sd(ax, [(pbd, sz, "pb"), (cgd, sz, "cgpb"), (cgd, sz, "sb")], cap)
        ax.set_xlim(0, cap * 1.06); kfmt(ax, cap); ax.set_ylim(-dylim, dylim)

    # RELIEF RULE: aqua is below 3:1 on this surface, so every series is
    # direct-labeled in the rightmost column as well as carrying a legend.
    for row, arms in ((0, ("none", "pb")), (1, ("none", "cgpb", "sb")),
                      (2, ("none", "pb", "cgpb", "sb")), (3, ("pb", "cgpb", "sb"))):
        ax = axes[row][2]
        for i, br in enumerate(arms):
            col = CTRL if br == "none" else COLOR[br]
            ax.annotate(LABEL[br], xy=(0.03, 0.96 - 0.085 * i), xycoords="axes fraction",
                        fontsize=8.5, color=col, ha="left", va="top", weight="bold")

    axes[3][2].annotate("only 4–5 pre-knee steps here;\nrounds disagree up to 7x",
                        xy=(0.97, 0.06), xycoords="axes fraction", fontsize=7.5,
                        color=INK3, ha="right", va="bottom")

    h, l = axes[2][0].get_legend_handles_labels()
    seen, hh, ll = set(), [], []
    for a, b in zip(h, l):
        if b not in seen:
            seen.add(b); hh.append(a); ll.append(b)
    fig.legend(hh, ll, loc="lower center", ncol=5, frameon=False, fontsize=9.5,
               labelcolor=INK2, bbox_to_anchor=(0.5, 0.043))
    fig.text(0.5, 0.028,
             "SD is plotted rather than variance (ms², ~6 orders of magnitude across a ramp, not comparable to the latency axis). "
             "Error bars = between-round sd (n=2).",
             ha="center", fontsize=8.5, color=INK3)
    fig.text(0.5, 0.013,
             "Rows 1–2 keep campaigns separate (absolute ms is session-dependent). Row 3 pools them: CV divides out each arm's own mean, "
             "and the two controls agreeing is the evidence that is safe.",
             ha="center", fontsize=8.5, color=INK3)
    fig.tight_layout(rect=[0, 0.062, 1, 0.977])
    fig.savefig(out, dpi=150, facecolor=SURFACE)
    print(f"wrote {out}")

    # numeric companion
    print(f"\n  pre-knee SD and CV, round-averaged")
    print(f"  {'cell':<16} {'SD ms':>8} {'CV':>7} {'ΔSD% vs ctl':>13}")
    for sz, _, cap, _ in SIZES:
        for d, br in ((pbd, "none"), (pbd, "pb"), (cgd, "none"), (cgd, "cgpb"), (cgd, "sb")):
            rounds = load(d, f"{sz}_{br}")
            sds, cvs = [], []
            for rd in rounds:
                for rps, rec in rounds[rd].items():
                    if rps <= cap:
                        sds.append(rec["sd"]); cvs.append(rec["sd"] / rec["mean"])
            if not sds:
                continue
            tag = f"{sz}/{br}" + ("*" if d == pbd else "")
            ds = ""
            if br != "none":
                ctl, brd = load(d, f"{sz}_none"), rounds
                v = [100 * (brd[rd][r]["sd"] - ctl[rd][r]["sd"]) / ctl[rd][r]["sd"]
                     for rd in set(ctl) & set(brd) for r in set(ctl[rd]) & set(brd[rd])
                     if r <= cap and ctl[rd][r]["sd"]]
                if v:
                    ds = f"{sum(v)/len(v):+12.2f}%"
            print(f"  {tag:<16} {sum(sds)/len(sds):>8.3f} {sum(cvs)/len(cvs):>7.3f} {ds:>13}")
    print("  * = p-bridge campaign; unmarked = cg/s-bridge campaign")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else None)
