#!/usr/bin/env python3
"""Analyze variance_experiment.sh output: does size variance inflate latency variance?

Reports, per utilisation rho, both arms' response-time mean / sd / CV averaged over
rounds with between-round error bars, and the fixed->variable ratios. Then checks the
Pollaczek-Khinchine prediction: at matched rho and matched mean service time, waiting
time should scale as (1 + CV_S^2)/2, so

    E[W]_variable / E[W]_fixed  ~=  (1 + CV_S_var^2) / (1 + CV_S_fix^2)

Waiting time is estimated as E[R] - E[S], using each arm's own measured E[S] from the
rho~0.1 run -- that isolates queueing from service, which is what the hypothesis is about.

Usage: analyze_variance.py <results_dir>
"""
import glob, math, os, re, sys


def parse(path):
    t = open(path).read()
    g = lambda p: (re.search(p, t, re.M).group(1) if re.search(p, t, re.M) else None)
    m = g(r"#\[Mean\s*=\s*([0-9.]+)")
    s = g(r"StdDeviation\s*=\s*([0-9.]+)")
    a = g(r"^Requests/sec:\s+([0-9.]+)")
    if not (m and s and a):
        return None
    return {"mean": float(m), "sd": float(s), "ach": float(a)}


def msd(v):
    n = len(v)
    mu = sum(v) / n
    sd = math.sqrt(sum((x - mu) ** 2 for x in v) / (n - 1)) if n > 1 else 0.0
    return mu, sd, n


def main(d):
    # measured service time per arm (rho ~ 0.1, no queueing)
    svc = {}
    for f in glob.glob(os.path.join(d, "svc_*.txt")):
        arm = re.search(r"svc_(\w+)\.txt", f).group(1)
        p = parse(f)
        if p:
            svc[arm] = {"ES": p["mean"], "CVS": p["sd"] / p["mean"]}
    print("MEASURED SERVICE TIME (rho~0.1, queueing-free)")
    for arm, v in sorted(svc.items()):
        print(f"  {arm:<9} E[S]={v['ES']:.3f} ms   CV_S={v['CVS']:.3f}")
    if "fixed" in svc and "bimodal" in svc:
        a, b = "bimodal", "fixed"
        pk = (1 + svc[a]["CVS"] ** 2) / (1 + svc[b]["CVS"] ** 2)
        print(f"\n  P-K predicts E[W] ratio ({a}/{b}) = (1+CV_S^2)/(1+CV_S^2) = {pk:.2f}x")

    # rho sweep
    runs = {}
    for f in glob.glob(os.path.join(d, "*_rho*_rd*.txt")):
        m = re.search(r"(\w+)_rho([0-9.]+)_rd(\d+)\.txt", os.path.basename(f))
        if not m:
            continue
        p = parse(f)
        if p:
            runs.setdefault((m.group(2), m.group(1)), []).append(p)

    rhos = sorted({k[0] for k in runs})
    arms = sorted({k[1] for k in runs})
    print(f"\nRESPONSE TIME BY UTILISATION (n rounds, +- = between-round sd)")
    hdr = f"{'rho':>5} |"
    for arm in arms:
        hdr += f" {arm+' mean_ms':>18} {arm+' CV':>14} |"
    hdr += f" {'ΔCV%':>7} {'E[W] ratio':>11} {'vs P-K':>8}"
    print(hdr)
    for rho in rhos:
        row = f"{rho:>5} |"
        cvs, ws = {}, {}
        for arm in arms:
            rs = runs.get((rho, arm), [])
            if not rs:
                row += f" {'--':>18} {'--':>14} |"
                continue
            mu, musd, n = msd([r["mean"] for r in rs])
            cv, cvsd, _ = msd([r["sd"] / r["mean"] for r in rs])
            cvs[arm] = cv
            es = svc.get(arm, {}).get("ES", 0.0)
            ws[arm] = max(mu - es, 1e-9)   # waiting time = response - service
            row += f" {mu:>10.3f}±{musd:<7.3f} {cv:>7.3f}±{cvsd:<6.3f} |"
        if "fixed" in cvs and "bimodal" in cvs:
            a, b = "bimodal", "fixed"
            dcv = 100 * (cvs[a] - cvs[b]) / cvs[b]
            wr = ws[a] / ws[b]
            pkr = ""
            if a in svc and b in svc:
                pk = (1 + svc[a]["CVS"] ** 2) / (1 + svc[b]["CVS"] ** 2)
                pkr = f"{wr/pk:>7.2f}x" if pk else ""
            row += f" {dcv:>+6.1f}% {wr:>10.2f}x {pkr:>8}"
        print(row)

    print("""
HOW TO READ THIS
  * 'ΔCV%'      : response-time CV, variable arm vs fixed arm, at matched rho.
                  Positive and growing with rho => hypothesis SUPPORTED.
  * 'E[W] ratio': queueing delay ratio (response minus that arm's own service time).
                  This is the quantity P-K makes a prediction about.
  * 'vs P-K'    : measured ratio / predicted ratio. ~1.0 means the inflation is
                  quantitatively what M/G/1 predicts from the measured CV_S.
  * If ΔCV% ~ 0 at every rho while CV_S differs a lot between arms, the hypothesis
    is falsified ON THIS SYSTEM, and the bound is what matters for the writeup.""")


if __name__ == "__main__":
    main(sys.argv[1])
