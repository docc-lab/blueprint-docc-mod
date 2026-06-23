import glob, os, statistics
import matplotlib; matplotlib.use('Agg')
import matplotlib.pyplot as plt

RUNS = "/users/tomislav/runs"
RPS = [2000+200*i for i in range(16)]
COLORS = {'vanilla':'tab:blue','pbridge':'tab:red','cgpb':'tab:green','sbridge':'tab:purple'}

CPD = {
 2: {
   'vanilla': 'ileav6_vNATGC_r[2-6]_*',
   'pbridge': 'ileav6_pbNATGC_r[2-6]_*',
   'cgpb':    'cgpb6_NATGC_esrev2_cpd2_*_r[2-6]_*',
   'sbridge': 'sb6_NATGC_esrev2_cpd2_*_r[2-6]_*',
 },
 6: {
   'vanilla': 'cpd6il_vNATGC_r[2-6]_*',
   'pbridge': 'cpd6il_pbNATGC_r[2-6]_*',
   'cgpb':    'cpd6il_cgpbNATGC_r[2-6]_*',
   'sbridge': 'cpd6il_sbNATGC_r[2-6]_*',
 },
}

def cluster_eps(rundir):
    """per-step cluster exporter throughput (spans/s) = sum_pods(delta sent_spans)/window."""
    f = os.path.join(rundir, "snapshots.tsv")
    if not os.path.exists(f): return {}
    # step -> pod -> list[(t,val)]
    byd = {}
    for line in open(f):
        p = line.rstrip("\n").split("\t")
        if len(p) != 7: continue
        t, ph, step, cat, src, metric, val = p
        if cat != "otelcol" or metric != "otelcol_exporter_sent_spans": continue
        try: t=int(t); step=int(step); val=int(val)
        except: continue
        if step not in RPS: continue
        byd.setdefault(step, {}).setdefault(src, []).append((t, val))
    out = {}
    for step, pods in byd.items():
        dsum = 0; ts = []
        for src, samples in pods.items():
            samples.sort()
            if len(samples) < 2: continue
            d = samples[-1][1] - samples[0][1]
            if d < 0: continue          # counter reset (pod restart) -> skip pod
            dsum += d
            ts += [samples[0][0], samples[-1][0]]
        if not ts: continue
        dt = max(ts) - min(ts)
        if dt > 0: out[step] = dsum / dt
    return out

def agg(glob_pat):
    rounds = [cluster_eps(d) for d in sorted(glob.glob(os.path.join(RUNS, glob_pat)))]
    rounds = [r for r in rounds if r]
    mean, worst = [], []
    for s in RPS:
        vals = [r[s] for r in rounds if s in r]
        mean.append(statistics.mean(vals) if vals else float('nan'))
        worst.append(min(vals) if vals else float('nan'))   # worst throughput = lowest
    return mean, worst, len(rounds)

for cpd in (2, 6):
    fig, ax = plt.subplots(figsize=(10, 6))
    print(f"\n=== cpd={cpd} cluster export throughput (spans/s) — mean | worst-round ===")
    print(f"{'rps':>5} | " + " | ".join(f"{v:>20}" for v in CPD[cpd]))
    tbl = {}
    for v, pat in CPD[cpd].items():
        m, w, n = agg(pat)
        tbl[v] = (m, w, n)
        ax.plot(RPS, m, color=COLORS[v], lw=2.2, marker='o', ms=4, label=f"{v} avg (n={n})")
        ax.plot(RPS, w, color=COLORS[v], lw=1.6, ls='--', marker='x', ms=4, label=f"{v} worst")
    for i, s in enumerate(RPS):
        print(f"{s:>5} | " + " | ".join(f"{tbl[v][0][i]:>8.0f}/{tbl[v][1][i]:>8.0f}" for v in CPD[cpd]))
    ax.set_xlabel("offered load (rps)")
    ax.set_ylabel("collector cluster export throughput (spans/s)")
    ax.set_title(f"DSB-SN cpd={cpd}, natural GC — collector exporter throughput\n(solid = per-cluster avg of 5 rounds, dashed = worst round at each step)")
    ax.grid(True, alpha=0.3); ax.legend(fontsize=8, ncol=2)
    fig.tight_layout()
    out = f"{RUNS}/throughput_cpd{cpd}_cluster_avg_vs_worst.png"
    fig.savefig(out, dpi=130)
    print("saved", out)
