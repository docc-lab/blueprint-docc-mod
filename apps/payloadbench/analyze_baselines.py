#!/usr/bin/env python3
"""Aggregate ramp_baselines.sh: per-size throughput/latency curves + knee + byte rate.

Knee = peak of the ROUND-AVERAGED achieved curve (never the mean of per-round maxima,
which is an upward-biased extreme-value statistic).

Usage: analyze_baselines.py <baselines_dir>
"""
import glob, math, os, re, sys

SIZES = [("p10", 200, 192), ("p50", 1536, 320), ("p90", 12000, 10000)]


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
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0), n


def main(d):
    for name, req, res in SIZES:
        steps = {}
        for f in glob.glob(os.path.join(d, name, "round_*", "step_*.txt")):
            # quarantined dirs (…_superseded / INVALID_…) also match round_* — skip them
            if "superseded" in f or "INVALID" in f:
                continue
            rps = int(re.search(r"step_(\d+)", f).group(1))
            p = parse(f)
            if p:
                steps.setdefault(rps, []).append(p)
        if not steps:
            continue
        rt = req + res     # round-trip payload bytes
        print(f"\n=== {name}: {req} B req / {res} B res  ({rt} B round trip) ===")
        print(f"{'offered':>8} {'achieved':>18} {'mean_ms':>15} {'p99_ms':>15} {'sd_ms':>8} {'MB/s':>7} {'non2xx':>7}")
        curve = []
        for rps in sorted(steps):
            rs = steps[rps]
            a, ae, n = msd([r["ach"] for r in rs])
            m, me, _ = msd([r["mean"] for r in rs])
            s, _, _ = msd([r["sd"] for r in rs])
            p9 = [r["p99"] for r in rs if r["p99"] is not None]
            p, pe, _ = msd(p9) if p9 else (float("nan"), 0, 0)
            bad = sum(r["bad"] for r in rs)
            if n >= 2: curve.append((a, rps, ae, n))
            disagree = (ae/a > 0.10) or (me/m > 0.50) if a and m else False
            print(f"{rps:>8} {a:>11.0f} ±{ae:<6.0f} {m:>9.3f} ±{me:<5.3f} {p:>9.3f} ±{pe:<5.3f} "
                  f"{s:>8.3f} {a*rt/1e6:>7.1f} {bad:>7}" + ("  <-- rounds disagree" if disagree else ""))
        if not curve:
            print("  (no step measured in both rounds; knee not reported)"); continue
        pk = max(curve)
        print(f"  KNEE (peak of round-averaged curve): {pk[0]:.0f} rps ±{pk[2]:.0f} "
              f"at offered {pk[1]} (n={pk[3]})  =  {pk[0]*rt/1e6:.1f} MB/s round-trip")

    print("""
NOTE
  * 'MB/s' is round-trip payload (request + response) x achieved rps -- the axis on
    which sizes are comparable. Request rate alone is not: capacity depends strongly
    on payload size, so equal rps across sizes is NOT equal load.
  * Saturation was detected per size (achieved < 0.95 x offered), because these sizes
    move very different byte volumes.""")


if __name__ == "__main__":
    main(sys.argv[1])
