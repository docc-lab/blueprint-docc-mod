#!/usr/bin/env python3
"""Aggregate ramp_bridges.sh: cost of bridge bytes on the call path.

THE COMPARISON IS WITHIN-ROUND AND PAIRED. 24 B is added to the REQUEST ONLY (the
call path), so the round-trip change is +6.1% at p10 but only +0.11% at p90 -- the
effect is comparable to between-round drift. For each (size, offered rps) we therefore compute the bridge-vs-control
delta SEPARATELY IN EACH ROUND, then average those deltas -- rather than averaging
each arm across rounds and subtracting. The two differ whenever rounds drift, and
only the paired form cancels the drift.

The reported +- is the between-round sd OF THE PAIRED DELTA, so if it straddles 0
the measurement does not resolve the effect. With n=2 that error bar is a range,
not a real sd; it is labelled as such.

Usage: analyze_bridges.py <bridges_dir>
"""
import glob, math, os, re, sys

SIZES = [("p10", 200, 192), ("p50", 1536, 320), ("p90", 12000, 10000)]
# nominal MEAN overhead per request; cgpb/sb are distributions, not constants
BRIDGE_BYTES = {"none": 0, "pb": 24, "cgpb": 24.96, "sb": 32.95}
# CV of the bridge's own size distribution -- sb is the only one that varies
BRIDGE_CV = {"pb": 0.0, "cgpb": 0.11, "sb": 2.86}
CONTROL = "none"


def parse(p):
    t = open(p).read()
    a = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not (a and m):
        return None
    d = {"ach": float(a.group(1)), "mean": float(m.group(1)), "sd": float(m.group(2))}
    q = re.search(r"^\s*99\.000%\s+([0-9.]+)(us|ms|s|m)\b", t, re.M)
    d["p99"] = (float(q.group(1)) * {"us": 1e-3, "ms": 1, "s": 1e3, "m": 6e4}[q.group(2)]) if q else None
    b = re.search(r"Non-2xx or 3xx responses:\s*([0-9]+)", t)
    d["bad"] = int(b.group(1)) if b else 0
    return d


def msd(v):
    n = len(v)
    if not n:
        return float("nan"), 0.0, 0
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0), n


def load(d):
    """-> data[size][bridge][round][rps] = record"""
    data = {}
    for f in glob.glob(os.path.join(d, "*", "round_*", "step_*.txt")):
        if "superseded" in f or "INVALID" in f:
            continue
        arm = os.path.basename(os.path.dirname(os.path.dirname(f)))
        if "_" not in arm:
            continue
        size, bridge = arm.rsplit("_", 1)
        rd = int(re.search(r"round_(\d+)", f).group(1))
        rps = int(re.search(r"step_(\d+)", f).group(1))
        p = parse(f)
        if p:
            data.setdefault(size, {}).setdefault(bridge, {}).setdefault(rd, {})[rps] = p
    return data


def main(d):
    data = load(d)
    bridges = sorted({b for s in data.values() for b in s} - {CONTROL})

    for name, req, res in SIZES:
        if name not in data or CONTROL not in data[name]:
            continue
        rt = req + res
        ctl = data[name][CONTROL]
        for br in bridges:
            if br not in data[name]:
                continue
            ob = BRIDGE_BYTES.get(br)
            pct = f"{100.0*ob/rt:+.2f}%" if ob is not None else "?"
            cv = BRIDGE_CV.get(br)
            cvs = f", CV_size={cv:.2f}" if cv is not None else ""
            print(f"\n=== {name}: {req}/{res} B ({rt} B round trip) — bridge '{br}' "
                  f"(mean +{ob} B on request, {pct} of round trip{cvs}) ===")
            print(f"{'offered':>8} {'ctl_mean':>10} {'brg_mean':>10} {'Δmean%':>16} "
                  f"{'Δp99%':>16} {'Δsd%':>16} {'Δach%':>9} {'n':>3}")
            brd = data[name][br]
            rounds = sorted(set(ctl) & set(brd))
            allrps = sorted({r for rd in rounds for r in set(ctl[rd]) & set(brd[rd])})
            rows = []
            for rps in allrps:
                dm, dmp, dpp, dsp, dap, cm, bm = [], [], [], [], [], [], []
                for rd in rounds:
                    if rps not in ctl[rd] or rps not in brd[rd]:
                        continue
                    c, b = ctl[rd][rps], brd[rd][rps]
                    cm.append(c["mean"]); bm.append(b["mean"])
                    dm.append(b["mean"] - c["mean"])
                    dmp.append(100 * (b["mean"] - c["mean"]) / c["mean"])
                    if c["p99"] and b["p99"]:
                        dpp.append(100 * (b["p99"] - c["p99"]) / c["p99"])
                    if c["sd"]:
                        dsp.append(100 * (b["sd"] - c["sd"]) / c["sd"])
                    dap.append(100 * (b["ach"] - c["ach"]) / c["ach"])
                if not dm:
                    continue
                mu, e, n = msd(dm)
                mup, ep, _ = msd(dmp)
                p9, e9, _ = msd(dpp)
                sdv, esd, _ = msd(dsp)
                ap, _, _ = msd(dap)
                rows.append((rps, mu, e, mup, ep, n))
                print(f"{rps:>8} {sum(cm)/len(cm):>10.3f} {sum(bm)/len(bm):>10.3f} "
                      f"{mup:>+9.2f}%±{ep:<5.2f} {p9:>+9.2f}%±{e9:<5.2f} "
                      f"{sdv:>+9.2f}%±{esd:<5.2f} {ap:>+8.2f}% {n:>3}")

            # Pre-knee summary: the flat region is where a per-request cost is
            # readable. Past the knee latency is dominated by queue growth and a
            # 24 B difference is invisible under 100%+ swings.
            pre = [r for r in rows if r[0] <= (allrps[-1] * 0.6)]
            if pre:
                w = [r[3] for r in pre]
                mu, e, n = msd(w)
                res_str = "RESOLVED" if n > 1 and abs(mu) > e else "NOT resolved (error spans 0)"
                print(f"  pre-knee mean Δ = {mu:+.2f}% (spread {e:.2f} across {len(pre)} steps) -> {res_str}")
    print(f"""
HOW TO READ THIS
  * Every Δ is computed WITHIN a round then averaged across rounds (paired), so
    round-to-round drift cancels. '±' is the between-round spread of that paired
    delta; with n=2 it is a range, not a real sd.
  * 'Δach%' should be ~0 below saturation (the load generator sets the rate).
    A negative Δach% at a step where the control kept up means the bridge arm
    saturated FIRST -- that is a capacity effect, not a latency effect.
  * Expect the effect to scale with the RELATIVE size change: p10 (+6.1% of round
    trip) is where 24 B should be detectable; p90 (+0.11%) mostly bounds it.
  * 'Δsd%' is the point of the sb arm. sb's size CV is 2.86 (p99.99 ~3 KB, 15x the
    p10 base request) while its MEAN is only ~33 B. If heavy-tailed baggage
    inflates latency variance, it shows up here and not in Δmean%.
  * Measured per-byte cost on this rig is ~24 ns/B (from the p10-vs-p90 baseline
    gap at 6k rps), so 24 B predicts ~0.6 us/request -- about 0.1% of a 0.5 ms
    response, i.e. an order of magnitude BELOW the +-1-3% step noise. A null here
    is expected; the useful output is the upper bound, not a point estimate.""")


if __name__ == "__main__":
    main(sys.argv[1])
