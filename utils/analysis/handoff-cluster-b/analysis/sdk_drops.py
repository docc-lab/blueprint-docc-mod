# Tomislav-RetCtx: exact SDK-side loss per point for any kind: deltas of the <kind>_processor_metrics counters
# (vanilla/pb/cgpb/sb) summed over the app services; usage: sdk_drops.py <case_dir> [rate ...]
import glob, gzip, re, os, sys, json
C = sys.argv[1]; rates = [int(r) for r in sys.argv[2:]]
KEYS = ("spans_received", "spans_sent", "spans_dropped", "cp_dropped", "lp_dropped", "send_deadline", "send_unavailable",
        "send_exhausted", "send_canceled", "send_other", "hp_buffer_depth", "lp_buffer_depth")
def last(p):
    try: t = gzip.open(p, "rt", errors="replace").read()
    except FileNotFoundError: return None
    m = re.findall(r"\w+_processor_metrics (.*)", t)
    return {k: int(v) for k, v in re.findall(r"(\w+)=(\d+)", m[-1])} if m else None
for P in sorted(glob.glob(f"{C}/rate-*")):
    if rates and int(P[-5:]) not in rates or not os.path.exists(f"{P}/result.json"): continue
    s = dict.fromkeys(KEYS, 0); per = {}
    for a in glob.glob(f"{P}/after/logs-*-service-*.txt.gz"):
        la, lb = last(a), last(a.replace("/after/", "/before/"))
        if not la: continue
        name = os.path.basename(a).split("-service")[0][5:]
        for k in KEYS:
            dv = la.get(k, 0) - ((lb or {}).get(k, 0) if "depth" not in k else 0)
            s[k] += dv
            if k in ("spans_dropped", "send_unavailable", "send_deadline") and dv: per.setdefault(name, {})[k] = dv
    d = json.load(open(f"{P}/result.json")); c = d["collector_deltas"]
    print(f"{int(P[-5:])}: spans/req at agents {c['otelcol_receiver_accepted_spans_total'] / d['completed_requests']:.3f} | SDK received {s['spans_received']:,} sent {s['spans_sent']:,}"
          f" dropped {s['spans_dropped']} (HP {s['cp_dropped']}, LP {s['lp_dropped']}) | send unavailable {s['send_unavailable']} deadline {s['send_deadline']}"
          f" exhausted {s['send_exhausted']} | buffered at end HP {s['hp_buffer_depth']} LP {s['lp_buffer_depth']} | agent->gw send_failed {c.get('otelcol_exporter_send_failed_spans_total', 0):.0f} | {per}")
