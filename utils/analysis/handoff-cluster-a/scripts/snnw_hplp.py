"""Tomislav-RetCtx: per-point HP/LP loss for an SN (or hotel) bridges case from the priority queue stage counters
(agents: HP/LP enqueued, send_failed = gateway refusals, evicted; gateway: HP/LP in vs out) and, for vanilla, spans lost
before the agents (spans/request reaching the agents vs the no-loss value). usage: snnw_hplp.py <case_dir> [full_spans_per_req]"""
import sys, glob, gzip, re, json, os
D = sys.argv[1]; full = sys.argv[2] if len(sys.argv) > 2 else None
# Tomislav-RetCtx: full = 'auto' -> the no-loss spans/request = max over the case's first five points
if full == 'auto':
    vals = []
    for P in sorted(glob.glob(f'{D}/rate-*'))[:5]:
        try:
            d = json.load(open(f'{P}/result.json')); vals.append(d['collector_deltas']['otelcol_receiver_accepted_spans_total'] / d['completed_requests'])
        except Exception: pass
    full = max(vals) if vals else None
elif full: full = float(full)
F = ('hp_enqueued', 'lp_enqueued', 'hp_sent', 'lp_sent', 'hp_send_failed', 'lp_send_failed', 'lp_evicted_spans')
def last(path):
    try: txt = gzip.open(path, 'rt', errors='replace').read()
    except FileNotFoundError: return None
    m = re.findall(r'priority_queue_metrics\t(\{.*\})', txt)
    return json.loads(m[-1]) if m else None
V = ('spans_received', 'spans_dropped', 'send_deadline', 'send_unavailable', 'send_exhausted', 'send_canceled', 'send_other',
     'cp_dropped', 'lp_dropped')
def vlast(path):
    # Tomislav-RetCtx: the SDK's own exact counters, vanilla_processor_metrics or {pb,cgpb,sb}_processor_metrics
    try: txt = gzip.open(path, 'rt', errors='replace').read()
    except FileNotFoundError: return None
    m = re.findall(r'\b(?:vanilla|pb|cgpb|sb)_processor_metrics (.*)', txt)
    return {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m[-1])} if m else None
for P in sorted(glob.glob(f'{D}/rate-*')):
    if not os.path.exists(f'{P}/result.json'): continue
    d = json.load(open(f'{P}/result.json')); c = d.get('collector_deltas')
    if not c: continue
    spr = c.get('otelcol_receiver_accepted_spans_total', 0) / d['completed_requests']
    a = dict.fromkeys(F, 0); g = dict.fromkeys(F, 0)
    for x in glob.glob(f'{P}/after/logs-otel*.txt.gz'):
        la, lb = last(x), last(x.replace('/after/', '/before/'))
        if not la: continue
        t = g if 'otelgw' in x else a
        for f in F: t[f] += la.get(f, 0) - (lb or {}).get(f, 0)
    line = f"{d['offered_rps']:6d}: got {d['completed_rps']:7,.0f} | spans/req at agents {spr:4.1f}"
    # Tomislav-RetCtx (2026-09-24): SDK-side loss from the SDK's exact counters, for every kind (spans the SDK dropped
    # never reach the agents, so the agent / gateway queue counters cannot see them)
    s = dict.fromkeys(V, 0); seen_v = False
    for x in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
        la, lb = vlast(x), vlast(x.replace('/after/', '/before/'))
        if la:
            seen_v = True
            for f in V: s[f] += la.get(f, 0) - (lb or {}).get(f, 0)
    if a['hp_enqueued'] + a['lp_enqueued']:
        lp_lost = a['lp_send_failed'] + a['lp_evicted_spans'] + g['lp_evicted_spans'] + s['lp_dropped']
        line += (f" | HP lost: SDK {s['cp_dropped']}, agents {a['hp_send_failed']}"
                 f" | LP lost {100 * lp_lost / max(a['lp_enqueued'] + s['lp_dropped'], 1):5.1f}% (SDK {s['lp_dropped']})"
                 f" | HP share {100 * a['hp_enqueued'] / (a['hp_enqueued'] + a['lp_enqueued']):.0f}%")
    else:
        # Tomislav-RetCtx (2026-09-24): vanilla loss from the SDK's exact counters (vanilla_processor_metrics), not the
        # spans/request ratio (window-edge spans put that ratio 0.1-0.2% above the true per-request count)
        if seen_v:
            fails = sum(s[f] for f in V[2:])
            line += (f" | SDK spans dropped {s['spans_dropped']} | send failures {fails}"
                     f" | spans lost before agents {100 * s['spans_dropped'] / max(s['spans_received'], 1):5.2f}%")
        elif full:
            line += f" | spans lost before agents {100 * max(0, 1 - spr / full):5.1f}%"
    print(line)
