#!/usr/bin/env python3
"""Full per-step + aggregate comparison across vanilla and SB runs."""
import sys
from collections import defaultdict

def load_deltas(path):
    """Return list of per-step deltas. Each entry: dict step -> {(cat,metric): value} after-before."""
    rows = []
    with open(path) as f:
        next(f)  # header
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) < 7: continue
            t, _, step, cat, src, metric, val = parts[:7]
            try:
                t = int(t); step = int(step); val = float(val)
            except ValueError:
                continue
            rows.append((t, step, cat, src, metric, val))

    # For each (step>0, time), aggregate across sources to per-step pre/post pair.
    times_per_step = defaultdict(list)
    for t,step,*_ in rows:
        if t not in times_per_step[step]:
            times_per_step[step].append(t)
    for step in times_per_step:
        times_per_step[step].sort()

    def aggregate(step, t):
        out = defaultdict(float)
        for r in rows:
            rt,rs,cat,src,metric,val = r
            if rs==step and rt==t:
                out[(cat,metric)] += val
        return out

    steps = sorted(s for s in times_per_step if s>0)
    deltas = {}
    for step in steps:
        ts = times_per_step[step]
        if len(ts)<2: continue
        b = aggregate(step, ts[0])
        a = aggregate(step, ts[-1])
        d = {}
        for k in set(list(b.keys())+list(a.keys())):
            d[k] = a.get(k,0) - b.get(k,0)
        deltas[step] = d
    return deltas

def aggregate_total(deltas, *keys):
    return sum(deltas[s].get(k, 0) for s in deltas for k in keys)

def total(deltas, key):
    return int(sum(deltas[s].get(key, 0) for s in deltas))

v = load_deltas("/tmp/v_run/snapshots.tsv")
sb = load_deltas("/tmp/sb_run/snapshots.tsv")

# --- Aggregate totals across whole ramp ---
print("=" * 96)
print("HEAD-TO-HEAD AGGREGATE TOTALS (whole 2k→4k ramp, matched pinning, back-to-back runs)")
print("=" * 96)
print()

def line(label, v_val, sb_val, formatter=lambda x: f"{int(x):>14,}"):
    print(f"  {label:<48} {formatter(v_val):>20}  {formatter(sb_val):>20}")

print(f"  {'':<48} {'VANILLA':>20}  {'SB v2':>20}")
print(f"  {'-'*48} {'-'*20}  {'-'*20}")
# Receiver / processor / exporter
line("Collector receiver_accepted_spans",
     total(v, ("otelcol","otelcol_receiver_accepted_spans")),
     total(sb, ("otelcol","otelcol_receiver_accepted_spans")))
line("Collector receiver_refused_spans",
     total(v, ("otelcol","otelcol_receiver_refused_spans")),
     total(sb, ("otelcol","otelcol_receiver_refused_spans")))
line("Collector processor_refused_spans (mem_lim)",
     total(v, ("otelcol","otelcol_processor_refused_spans")),
     total(sb, ("otelcol","otelcol_processor_refused_spans")))
line("Collector exporter_sent_spans",
     total(v, ("otelcol","otelcol_exporter_sent_spans")),
     total(sb, ("otelcol","otelcol_exporter_sent_spans")))
line("Collector exporter_send_failed_spans",
     total(v, ("otelcol","otelcol_exporter_send_failed_spans")),
     total(sb, ("otelcol","otelcol_exporter_send_failed_spans")))
line("Collector exporter_enqueue_failed_spans (silent)",
     total(v, ("otelcol","otelcol_exporter_enqueue_failed_spans")),
     total(sb, ("otelcol","otelcol_exporter_enqueue_failed_spans")))
print()
line("SDK spans_received",
     total(v, ("sdk","spans_received")),
     total(sb, ("sdk","spans_received")))
line("SDK spans_sent",
     total(v, ("sdk","spans_sent")),
     total(sb, ("sdk","spans_sent")))
line("SDK spans_dropped",
     total(v, ("sdk","spans_dropped")),
     total(sb, ("sdk","spans_dropped")))
line("SDK send_unavailable",
     total(v, ("sdk","send_unavailable")),
     total(sb, ("sdk","send_unavailable")))
print()
print("  Per-priority breakdown (SB only — vanilla SDK doesn't classify):")
line("  SB SDK cp_received",  0, total(sb, ("sdk","cp_received")))
line("  SB SDK lp_received",  0, total(sb, ("sdk","lp_received")))
line("  SB SDK cp_sent",      0, total(sb, ("sdk","cp_sent")))
line("  SB SDK lp_sent",      0, total(sb, ("sdk","lp_sent")))
line("  SB SDK cp_dropped",   0, total(sb, ("sdk","cp_dropped")))
line("  SB SDK lp_dropped",   0, total(sb, ("sdk","lp_dropped")))

print()
# Estimate vanilla CP loss using SB-derived CP fraction at the SDK
sb_cp_recv = total(sb, ("sdk","cp_received"))
sb_lp_recv = total(sb, ("sdk","lp_received"))
sb_total_recv = sb_cp_recv + sb_lp_recv
sdk_cp_frac = sb_cp_recv / sb_total_recv if sb_total_recv else 0
print(f"  SB-derived global SDK CP fraction = {sb_cp_recv:,} / {sb_total_recv:,} = {sdk_cp_frac:.3f}")

# Vanilla total loss
v_collector_drops = total(v, ("otelcol","otelcol_exporter_enqueue_failed_spans")) + \
                    total(v, ("otelcol","otelcol_exporter_send_failed_spans")) + \
                    total(v, ("otelcol","otelcol_processor_refused_spans"))
v_sdk_drops = total(v, ("sdk","spans_dropped"))
v_total_loss = v_collector_drops + v_sdk_drops
v_cp_lost_est = int(sdk_cp_frac * v_total_loss)
print(f"\n  Vanilla total spans lost (collector+SDK): {v_total_loss:,}")
print(f"  Vanilla estimated CP lost (× CP frac): {v_cp_lost_est:,}")

# SB total CP loss (exact at SDK + collector)
sb_collector_cp = 0  # extract per-priority later if needed; collector also has its own cp_dropped
sb_sdk_cp = total(sb, ("sdk","cp_dropped"))
print(f"\n  SB SDK cp_dropped (exact): {sb_sdk_cp:,}")
# Note: collector-side per-priority lives in priority_processor logs not Prom.
# For now, treat collector-side SB drops as a separate bucket.

print()
print("=" * 96)
print("PER-STEP DELTAS")
print("=" * 96)
print()
for variant, deltas, name in [("v", v, "VANILLA"), ("sb", sb, "SB v2")]:
    print(f"\n### {name}")
    cols = ["step", "recv_acc", "recv_refused", "proc_refused", "exp_sent",
            "exp_send_failed", "exp_enq_failed",
            "sdk_recv", "sdk_sent", "sdk_dropped", "sdk_send_un"]
    if variant=="sb":
        cols += ["cp_recv", "cp_sent", "cp_dropped", "lp_dropped"]
    print("  " + " ".join(f"{c:>12}" for c in cols))
    for s in sorted(deltas):
        d = deltas[s]
        row = [
            f"{s}",
            f"{int(d.get(('otelcol','otelcol_receiver_accepted_spans'), 0)):,}",
            f"{int(d.get(('otelcol','otelcol_receiver_refused_spans'), 0)):,}",
            f"{int(d.get(('otelcol','otelcol_processor_refused_spans'), 0)):,}",
            f"{int(d.get(('otelcol','otelcol_exporter_sent_spans'), 0)):,}",
            f"{int(d.get(('otelcol','otelcol_exporter_send_failed_spans'), 0)):,}",
            f"{int(d.get(('otelcol','otelcol_exporter_enqueue_failed_spans'), 0)):,}",
            f"{int(d.get(('sdk','spans_received'), 0)):,}",
            f"{int(d.get(('sdk','spans_sent'), 0)):,}",
            f"{int(d.get(('sdk','spans_dropped'), 0)):,}",
            f"{int(d.get(('sdk','send_unavailable'), 0)):,}",
        ]
        if variant=="sb":
            row += [
                f"{int(d.get(('sdk','cp_received'), 0)):,}",
                f"{int(d.get(('sdk','cp_sent'), 0)):,}",
                f"{int(d.get(('sdk','cp_dropped'), 0)):,}",
                f"{int(d.get(('sdk','lp_dropped'), 0)):,}",
            ]
        print("  " + " ".join(f"{x:>12}" for x in row))
