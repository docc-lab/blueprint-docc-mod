# Tomislav-RetCtx: SN real-work nt vs v per rate across repetitions (+ which node each stateful pod ran on)
import json, glob, statistics as st, sys
D = "/users/tomislav/deployments/dsb-sn"
roots = {"nt": f"{D}/retctx-snrw-final-nt-20260924T163105Z", "v": f"{D}/retctx-snrw-final-v-20260924T163105Z"}
data = {}
for k, R in roots.items():
    for c in sorted(glob.glob(f"{R}/run/0?-{k}")):
        for f in glob.glob(f"{c}/rate-*/result.json"):
            d = json.load(open(f))
            data.setdefault((k, d["offered_rps"]), []).append((d["completed_rps"], d["mean_ms"], d["p99_ms"]))
print("rate | nt: got  mean-of-means  median-p99  [per-rep p99] | v: got  mean-of-means  median-p99  [per-rep p99]")
for r in range(2000, 3601, 100):
    row = f"{r} |"
    for k in ("nt", "v"):
        x = data.get((k, r), [])
        reps = " ".join("%.0f" % c for _, _, c in x)
        row += f" {st.mean(a for a, _, _ in x):6.0f} {st.mean(b for _, b, _ in x):7.1f} {st.median(c for _, _, c in x):7.1f} [{reps}] |"
    print(row)
print()
for k, R in roots.items():
    for c in sorted(glob.glob(f"{R}/run/0?-{k}")):
        pj = sorted(glob.glob(f"{c}/rate-*/after/pods.json"))[0]
        pods = json.load(open(pj))
        items = pods.get("items", pods) if isinstance(pods, dict) else pods
        where = {}
        for p in items:
            n = p["metadata"]["name"]
            if any(s in n for s in ("usertimeline", "mongo", "wrk2api", "composepost", "post-storage", "hometimeline")):
                where[n.split("-v-")[0].split("-nt-")[0][:28]] = p["spec"].get("nodeName", "?").replace("node-", "n")
        print(k, c[-5:], " ".join(f"{a}@{b}" for a, b in sorted(where.items())))
