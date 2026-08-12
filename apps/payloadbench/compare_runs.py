#!/usr/bin/env python3
"""Paired comparison of two multi-round run_ramp.sh campaigns.

Answers: does the response-time DISTRIBUTION differ between two workloads
(e.g. fixed vs uniformly-spread payload sizes) at matched offered load?

For each offered-RPS step present in both runs, reports the per-round-averaged
mean, p99 and request-level stddev for each run, plus the delta and a paired
two-sample t statistic (Welch) on the request-level stddev -- the quantity of
interest when asking "is response time more variable?".

Also reports CV (sd/mean), the scale-free comparator, since a stddev change
that merely tracks a mean change is not an increase in relative variability.

Usage: compare_runs.py <runA_dir> <runB_dir> [--lo RPS] [--hi RPS] [--label-a A] [--label-b B]
"""
import argparse, glob, math, os, re


def parse(p):
    t = open(p).read()
    m = re.search(r"^Requests/sec:\s+([0-9.]+)", t, re.M)
    if not m:
        return None
    d = {"ach": float(m.group(1))}
    m = re.search(r"#\[Mean\s*=\s*([0-9.]+),\s*StdDeviation\s*=\s*([0-9.]+)", t)
    if not m:
        return None
    d["mean"], d["sd"] = float(m.group(1)), float(m.group(2))

    def pct(q):
        m = re.search(rf"^\s*{q}\.000%\s+([0-9.]+)(us|ms|s|m)\b", t, re.M)
        return float(m.group(1)) * {"us": 1e-3, "ms": 1.0, "s": 1e3, "m": 6e4}[m.group(2)] if m else None

    d["p50"], d["p99"] = pct(50), pct(99)
    return d


def load(run):
    steps = {}
    for f in glob.glob(os.path.join(run, "round_*", "step_*.txt")):
        rps = int(re.search(r"step_(\d+)", f).group(1))
        d = parse(f)
        if d:
            steps.setdefault(rps, []).append(d)
    return steps


def msd(v):
    n = len(v)
    mu = sum(v) / n
    return mu, (math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0), n


def welch(a, b):
    """Welch t on two small samples; returns (t, approx_significant_at_95)."""
    ma, sa, na = msd(a)
    mb, sb, nb = msd(b)
    se = math.sqrt(sa ** 2 / na + sb ** 2 / nb)
    if se == 0:
        return float("nan"), False
    t = (mb - ma) / se
    # conservative: |t|>2.78 ~ p<0.05 two-sided at df=4 (n=5 vs n=5 worst case)
    return t, abs(t) > 2.78


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("runA")
    ap.add_argument("runB")
    ap.add_argument("--lo", type=int, default=0)
    ap.add_argument("--hi", type=int, default=10 ** 9)
    ap.add_argument("--label-a", default="A")
    ap.add_argument("--label-b", default="B")
    args = ap.parse_args()

    A, B = load(args.runA), load(args.runB)
    common = sorted(set(A) & set(B))
    common = [r for r in common if args.lo <= r <= args.hi]
    if not common:
        raise SystemExit("no overlapping steps")

    la, lb = args.label_a, args.label_b
    print(f"{la} = {args.runA}")
    print(f"{lb} = {args.runB}")
    print()
    print(f"{'offered':>7} {'nA/nB':>6} | {'mean_'+la:>10} {'mean_'+lb:>10} {'Δmean':>7} "
          f"| {'p99_'+la:>9} {'p99_'+lb:>9} {'Δp99':>7} "
          f"| {'sd_'+la:>8} {'sd_'+lb:>8} {'Δsd':>7} {'Δsd%':>6} {'t':>6} {'sig':>3} "
          f"| {'CV_'+la:>5} {'CV_'+lb:>5}")
    acc = []
    for rps in common:
        a, b = A[rps], B[rps]
        mA, _, nA = msd([x["mean"] for x in a]);  mB, _, nB = msd([x["mean"] for x in b])
        pA, _, _ = msd([x["p99"] for x in a]);    pB, _, _ = msd([x["p99"] for x in b])
        sA, _, _ = msd([x["sd"] for x in a]);     sB, _, _ = msd([x["sd"] for x in b])
        t, sig = welch([x["sd"] for x in a], [x["sd"] for x in b])
        dpct = 100 * (sB - sA) / sA if sA else float("nan")
        print(f"{rps:>7} {nA:>2}/{nB:<3} | {mA:>10.2f} {mB:>10.2f} {mB-mA:>7.2f} "
              f"| {pA:>9.2f} {pB:>9.2f} {pB-pA:>7.2f} "
              f"| {sA:>8.2f} {sB:>8.2f} {sB-sA:>7.2f} {dpct:>5.1f}% {t:>6.2f} {'*' if sig else '':>3} "
              f"| {sA/mA:>5.2f} {sB/mB:>5.2f}")
        acc.append((sA, sB, sA / mA, sB / mB))
    n = len(acc)
    aA = sum(x[0] for x in acc) / n; aB = sum(x[1] for x in acc) / n
    cA = sum(x[2] for x in acc) / n; cB = sum(x[3] for x in acc) / n
    print()
    print(f"BAND AVG over {n} steps: sd {aA:.2f} -> {aB:.2f} ms "
          f"({100*(aB-aA)/aA:+.1f}%),  CV {cA:.2f} -> {cB:.2f} ({100*(cB-cA)/cA:+.1f}%)")
    wins = sum(1 for x in acc if x[1] > x[0])
    print(f"sign test: {lb} sd higher at {wins}/{n} steps "
          f"(pure noise would give ~{n/2:.1f})")


if __name__ == "__main__":
    main()
