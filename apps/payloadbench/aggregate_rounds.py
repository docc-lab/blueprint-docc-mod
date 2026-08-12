#!/usr/bin/env python3
"""Aggregate a multi-round run_ramp.sh campaign (results/<run>/round_*/step_*.txt).

Per offered-RPS step, reports two distinct kinds of statistics:

  1. BETWEEN-ROUND (the error bars): each round contributes ONE value of each
     statistic (its mean, p50, p99, achieved rps); we report the average of
     those across rounds +/- the sample stddev of those N values. This is the
     round-to-round variability -- the honest uncertainty for comparing steps
     or configs. (Percentiles averaged this way are "mean of per-round pXX",
     NOT the pXX of the pooled distribution.)

  2. POOLED request-level stddev (descriptive width): law of total variance
     over all rounds' requests, count-weighted:
         var_pooled = sum(n_r*(sd_r^2 + (mu_r - mu_w)^2)) / sum(n_r)
     where mu_w is the count-weighted grand mean. This is what the stddev of
     the union of all requests would be -- it includes between-round shift.

Usage: aggregate_rounds.py <run_dir>
"""
import glob, math, os, re, sys


def parse_step(path):
    """Extract achieved rps, mean/stddev (ms), p50/p99 (ms), total count, non-2xx."""
    txt = open(path).read()
    out = {}
    m = re.search(r"^Requests/sec:\s+([0-9.]+)", txt, re.M)
    if not m:
        return None
    out["achieved"] = float(m.group(1))
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", txt)
    if not m:
        return None
    out["mean"], out["sd"] = float(m.group(1)), float(m.group(2))
    m = re.search(r"Total count\s*=\s*([0-9]+)", txt)
    out["n"] = int(m.group(1)) if m else 0

    def pct(p):
        m = re.search(rf"^\s*{p}\.000%\s+([0-9.]+)(us|ms|s|m)\b", txt, re.M)
        if not m:
            return None
        v, u = float(m.group(1)), m.group(2)
        return v * {"us": 1e-3, "ms": 1.0, "s": 1e3, "m": 6e4}[u]

    out["p50"], out["p99"] = pct(50), pct(99)
    m = re.search(r"Non-2xx or 3xx responses:\s*([0-9]+)", txt)
    out["non2xx"] = int(m.group(1)) if m else 0
    return out


def mean_sd(vals):
    n = len(vals)
    mu = sum(vals) / n
    sd = math.sqrt(sum((v - mu) ** 2 for v in vals) / (n - 1)) if n > 1 else 0.0
    return mu, sd


def main(run_dir):
    rounds = sorted(glob.glob(os.path.join(run_dir, "round_*")))
    if not rounds:
        sys.exit(f"no round_* dirs under {run_dir}")
    # step rps -> list of per-round parses
    steps = {}
    for rdir in rounds:
        for f in glob.glob(os.path.join(rdir, "step_*.txt")):
            rps = int(re.search(r"step_(\d+)\.txt", f).group(1))
            p = parse_step(f)
            if p:
                steps.setdefault(rps, []).append(p)

    hdr = (f"{'offered':>8} {'n':>2} {'achieved':>9} {'+-':>7} "
           f"{'mean_ms':>8} {'+-':>6} {'p50_ms':>7} {'+-':>6} "
           f"{'p99_ms':>8} {'+-':>7} {'pooled_sd':>9} {'non2xx':>6}")
    print(hdr)
    for rps in sorted(steps):
        rs = steps[rps]
        ach_mu, ach_sd = mean_sd([r["achieved"] for r in rs])
        mean_mu, mean_sd_ = mean_sd([r["mean"] for r in rs])
        p50_mu, p50_sd = mean_sd([r["p50"] for r in rs if r["p50"] is not None])
        p99_mu, p99_sd = mean_sd([r["p99"] for r in rs if r["p99"] is not None])
        # pooled request-level sd (law of total variance, count-weighted)
        N = sum(r["n"] for r in rs)
        if N:
            mu_w = sum(r["n"] * r["mean"] for r in rs) / N
            var_p = sum(r["n"] * (r["sd"] ** 2 + (r["mean"] - mu_w) ** 2) for r in rs) / N
            pooled = math.sqrt(var_p)
        else:
            pooled = float("nan")
        bad = sum(r["non2xx"] for r in rs)
        print(f"{rps:>8} {len(rs):>2} {ach_mu:>9.1f} {ach_sd:>7.1f} "
              f"{mean_mu:>8.3f} {mean_sd_:>6.3f} {p50_mu:>7.2f} {p50_sd:>6.2f} "
              f"{p99_mu:>8.2f} {p99_sd:>7.2f} {pooled:>9.3f} {bad:>6}")


if __name__ == "__main__":
    main(sys.argv[1])
