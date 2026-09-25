# Tomislav-RetCtx: vanilla SDK exact loss counters (vanilla_processor_metrics) per point, before->after deltas summed over services
import glob, gzip, re, os, sys
R = sys.argv[1]
F = ("spans_received", "spans_sent", "spans_dropped", "batches_dropped", "send_deadline", "send_unavailable",
     "send_exhausted", "send_canceled", "send_other")
def last(p):
    try: t = gzip.open(p, "rt", errors="replace").read()
    except FileNotFoundError: return None
    m = re.findall(r"vanilla_processor_metrics (.*)", t)
    return {k: int(v) for k, v in re.findall(r"(\w+)=(\d+)", m[-1])} if m else None
for case in sorted(glob.glob(f"{R}/run/0?-v")):
    out = []
    for P in sorted(glob.glob(f"{case}/rate-*")):
        if not os.path.exists(f"{P}/result.json"): continue
        s = dict.fromkeys(F, 0)
        for a in glob.glob(f"{P}/after/logs-*-service-*.txt.gz"):
            la, lb = last(a), last(a.replace("/after/", "/before/"))
            if la:
                for k in F: s[k] += la.get(k, 0) - (lb or {}).get(k, 0)
        bad = {k: s[k] for k in F[2:] if s[k]}
        out.append(f"{int(P[-5:])}:{'dropped ' + str(bad) if bad else 'ok'}")
    print(os.path.basename(case), " ".join(out))
