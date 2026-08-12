#!/usr/bin/env python3
"""Aggregate ramp_variance.sh: per-arm curves + knee, with between-round error bars.

Knee = peak of the ROUND-AVERAGED achieved curve. NOT the average of per-round maxima:
averaging a per-round max is an upward-biased extreme-value statistic (documented in
examples/dsb_sn/notes/nt_ceiling_below_sampled_tracing.md, where it inflated every knee).

Usage: analyze_rampvar.py <results_dir>
"""
import glob, math, os, re, sys

ARMS = ["fixed", "exp", "bimodal"]
CVS = {"fixed": "0", "exp": "1.0", "bimodal": "3.31"}


def parse(p):
    t = open(p).read()
    a = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not (a and m):
        return None
    d = {"ach": float(a.group(1)), "mean": float(m.group(1)), "sd": float(m.group(2))}
    q = re.search(r"^\s*99\.000%\s+([0-9.]+)(us|ms|s|m)\b", t, re.M)
    if q:
        d["p99"] = float(q.group(1)) * {"us": 1e-3, "ms": 1, "s": 1e3, "m": 6e4}[q.group(2)]
    b = re.search(r"Non-2xx or 3xx responses:\s*([0-9]+)", t)
    d["bad"] = int(b.group(1)) if b else 0
    return d


def msd(v):
    n = len(v)
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0), n


def main(d):
    data = {}   # arm -> rps -> list of parsed
    for arm in ARMS:
        for f in glob.glob(os.path.join(d, arm, "round_*", "step_*.txt")):
            rps = int(re.search(r"step_(\d+)", f).group(1))
            p = parse(f)
            if p:
                data.setdefault(arm, {}).setdefault(rps, []).append(p)
    if not data:
        sys.exit("no data")

    steps = sorted(set().union(*[set(v) for v in data.values()]))
    print("ACHIEVED THROUGHPUT (mean +- between-round sd)")
    hdr = f"{'offered':>8}"
    for a in ARMS:
        hdr += f" | {a+' (CV='+CVS[a]+')':>22}"
    print(hdr)
    for rps in steps:
        row = f"{rps:>8}"
        for a in ARMS:
            rs = data.get(a, {}).get(rps, [])
            if rs:
                mu, sd, n = msd([r["ach"] for r in rs])
                row += f" | {mu:>13.0f} ±{sd:<7.0f}"
            else:
                row += f" | {'--':>22}"
        print(row)

    print("\nMEAN RESPONSE TIME ms (mean +- between-round sd)  and  bimodal penalty vs fixed")
    hdr = f"{'offered':>8}"
    for a in ARMS:
        hdr += f" | {a:>18}"
    hdr += f" | {'penalty':>8}"
    print(hdr)
    for rps in steps:
        row = f"{rps:>8}"
        vals = {}
        for a in ARMS:
            rs = data.get(a, {}).get(rps, [])
            if rs:
                mu, sd, n = msd([r["mean"] for r in rs])
                vals[a] = mu
                row += f" | {mu:>10.3f} ±{sd:<5.3f}"
            else:
                row += f" | {'--':>18}"
        if "fixed" in vals and "bimodal" in vals and vals["fixed"]:
            row += f" | {100*(vals['bimodal']-vals['fixed'])/vals['fixed']:>+7.1f}%"
        print(row)

    print("\nKNEE = peak of the round-averaged achieved curve (never mean-of-per-round-maxima)")
    knees = {}
    for a in ARMS:
        if a not in data:
            continue
        curve = []
        for rps in sorted(data[a]):
            mu, sd, n = msd([r["ach"] for r in data[a][rps]])
            curve.append((mu, rps, sd, n))
        pk = max(curve)
        knees[a] = pk[0]
        print(f"  {a:<9} (CV_size={CVS[a]:>4}) knee = {pk[0]:>8.0f} rps ±{pk[2]:.0f}  at offered {pk[1]} (n={pk[3]})")
    if "fixed" in knees and knees["fixed"]:
        for a in ("exp", "bimodal"):
            if a in knees:
                print(f"  => {a} knee is {100*(knees[a]-knees['fixed'])/knees['fixed']:+.1f}% vs fixed")

    tot_bad = sum(r["bad"] for a in data for rps in data[a] for r in data[a][rps])
    print(f"\ntotal non-2xx across all runs: {tot_bad}")
    print("""
READING THIS vs the rho-matched experiment
  At matched OFFERED RPS the penalty compounds capacity loss AND queueing amplification
  -- the total effect an operator sees. The rho-matched table isolates the queueing
  mechanism alone. Quote effect SIZE from the rho-matched points (the knee region is
  near-vertical, so small rps offsets swing latency wildly) and use these curves for
  shape and knee location.""")


if __name__ == "__main__":
    main(sys.argv[1])
