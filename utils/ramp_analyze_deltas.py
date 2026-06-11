#!/usr/bin/env python3
"""Compute per-step deltas of all otelcol+SDK counters from snapshots.tsv."""
import sys
from collections import defaultdict

if len(sys.argv) < 2:
    print("Usage: analyze_deltas.py <snapshots.tsv> [variant]", file=sys.stderr)
    sys.exit(1)

PATH = sys.argv[1]
VAR = sys.argv[2] if len(sys.argv) > 2 else "?"

# Group snapshots by (step, before/after) => {source -> {metric -> sum-across-pods}}
# A "step" snapshot can appear before OR after a rps step. We group by phase
# inferred from row order. Easier: parse the lines and group by (step, phase),
# where phase is inferred from the rows. But our snapshots.tsv has step but
# not phase tag separately — phase is encoded in "snap" (we set phase as a
# column at write time but the format we used has columns:
#   time, "snap", step, category, source, metric, value
# We lost the before/after distinction in the column structure. Instead,
# rely on the ORDER: same step appears twice (before, then after).

rows = []
with open(PATH) as f:
    header = f.readline()
    for line in f:
        parts = line.rstrip("\n").split("\t")
        if len(parts) < 7: continue
        t, _, step, cat, src, metric, val = parts[:7]
        try:
            t = int(t); step = int(step); val = float(val)
        except ValueError:
            continue
        rows.append((t, step, cat, src, metric, val))

# Group by step → list of distinct snapshot-times in chronological order
times_per_step = defaultdict(list)
for r in rows:
    t, step, *_ = r
    if t not in times_per_step[step]:
        times_per_step[step].append(t)
for step in times_per_step:
    times_per_step[step].sort()

def aggregate(step, time):
    """Return {category: {metric_name: sum-across-sources}} at this snapshot."""
    out = defaultdict(lambda: defaultdict(float))
    for r in rows:
        t, s, cat, src, metric, val = r
        if s == step and t == time:
            out[cat][metric] += val
    return out

# For each rps step, compute delta = after - before
# step 0 has multiple snapshots (pre_warmup, post_warmup); skip
RAMP_STEPS = sorted([s for s in times_per_step if s > 0])

print(f"# Per-step deltas — variant: {VAR}")
print(f"# {len(RAMP_STEPS)} ramp steps: {RAMP_STEPS}")
print()

cols = [
    "step_rps",
    "Δreceiver_accepted",
    "Δreceiver_refused",
    "Δprocessor_refused",
    "Δexporter_sent",
    "Δexporter_send_failed",
    "Δexporter_enqueue_failed",
    "Δsdk_received",
    "Δsdk_sent",
    "Δsdk_dropped",
    "Δsdk_send_unavailable",
    "max_alloc_MB",
    "max_rss_MB",
]
print("\t".join(cols))

def get(agg, cat, metric):
    return agg[cat].get(metric, 0.0)

for step in RAMP_STEPS:
    ts = times_per_step[step]
    if len(ts) < 2:
        # No before/after pair; skip
        continue
    before_t, after_t = ts[0], ts[-1]
    b = aggregate(step, before_t)
    a = aggregate(step, after_t)

    def delta(cat, m):
        return int(get(a, cat, m) - get(b, cat, m))

    # Max alloc/rss seen at this step's post-snap
    max_alloc = int(get(a, "otelcol", "otelcol_process_runtime_heap_alloc_bytes") / 1048576)
    max_rss = int(get(a, "otelcol", "otelcol_process_memory_rss") / 1048576)

    row = [
        str(step),
        str(delta("otelcol", "otelcol_receiver_accepted_spans")),
        str(delta("otelcol", "otelcol_receiver_refused_spans")),
        str(delta("otelcol", "otelcol_processor_refused_spans")),
        str(delta("otelcol", "otelcol_exporter_sent_spans")),
        str(delta("otelcol", "otelcol_exporter_send_failed_spans")),
        str(delta("otelcol", "otelcol_exporter_enqueue_failed_spans")),
        str(delta("sdk", "spans_received")),
        str(delta("sdk", "spans_sent")),
        str(delta("sdk", "spans_dropped")),
        str(delta("sdk", "send_unavailable")),
        str(max_alloc),
        str(max_rss),
    ]
    print("\t".join(row))
