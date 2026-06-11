#!/usr/bin/env python3
"""Continuous memory monitor for otelcol pods.
Polls every 5s, scrapes via port-forward, writes TSV:
  time<TAB>pod<TAB>metric<TAB>value
plus emits a one-line stdout event per tick so it can be Monitor-watched.
"""
import subprocess, time, sys, os

VARIANT = sys.argv[1] if len(sys.argv) > 1 else "sbrev0"
OUT = sys.argv[2] if len(sys.argv) > 2 else f"/tmp/{VARIANT}_mem.tsv"
INTERVAL = int(sys.argv[3]) if len(sys.argv) > 3 else 5

POD_LABEL = f"io.kompose.service=otelcol-{VARIANT}-ctr"

# Metrics to scrape
METRICS = [
    "otelcol_process_runtime_heap_alloc_bytes",
    "otelcol_process_runtime_total_alloc_bytes",
    "otelcol_process_runtime_total_sys_memory_bytes",
    "otelcol_process_memory_rss",
    "otelcol_receiver_accepted_spans",
    "otelcol_processor_refused_spans",
    "otelcol_exporter_sent_spans",
    "otelcol_exporter_send_failed_spans",
    "otelcol_exporter_enqueue_failed_spans",
]

def scrape_pod(pod, port):
    proc = subprocess.Popen(
        ["kubectl","port-forward",pod,f"{port}:8888"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    time.sleep(1.3)
    try:
        r = subprocess.run(
            ["curl","-s","--max-time","3",f"http://localhost:{port}/metrics"],
            capture_output=True, text=True, timeout=5
        )
        return r.stdout
    finally:
        proc.kill()
        proc.wait()

def extract(raw, metric, exporter_filter=None):
    """Sum all label-sets for `metric`. Optionally filter by exporter label."""
    total = 0.0
    for line in raw.split("\n"):
        if line.startswith("#"): continue
        if "_bucket" in line or "_count{" in line: continue
        if not line.startswith(metric): continue
        # Check it's not a sub-metric like metric_total
        rest = line[len(metric):]
        if rest and not (rest[0] == '{' or rest[0] == ' ' or rest.startswith('_sum') or rest.startswith('_total')):
            continue
        parts = line.rsplit(" ", 1)
        if len(parts) != 2: continue
        head, val = parts
        try: val = float(val)
        except ValueError: continue
        if exporter_filter:
            # Simple substring check — labels look like exporter="otlp"
            if f'exporter="{exporter_filter}"' not in head:
                continue
        total += val
    return total

with open(OUT, "w") as f:
    f.write("time\tpod\tmetric\tvalue\n")

base_port = 19100
while True:
    t = int(time.time())
    # Get current pods (re-discover each tick)
    try:
        pods_raw = subprocess.run(
            ["kubectl","get","pods","-l",POD_LABEL,"--no-headers","-o","name"],
            capture_output=True, text=True, timeout=10
        ).stdout
    except subprocess.TimeoutExpired:
        time.sleep(INTERVAL)
        continue
    pods = [p.replace("pod/","").strip() for p in pods_raw.split("\n") if p.strip()]

    tick_data = {}
    for i, pod in enumerate(pods):
        port = base_port + i
        raw = scrape_pod(pod, port)
        if not raw:
            continue
        for m in METRICS:
            if "exporter" in m and "queue" not in m and m != "otelcol_exporter_enqueue_failed_spans":
                # Get otlp value specifically
                v = extract(raw, m, exporter_filter="otlp")
                tick_data.setdefault(pod, {})[f"{m}_otlp"] = v
            elif "exporter" in m:
                v_otlp = extract(raw, m, exporter_filter="otlp")
                tick_data.setdefault(pod, {})[f"{m}_otlp"] = v_otlp
            else:
                v = extract(raw, m)
                tick_data.setdefault(pod, {})[m] = v

    # Write data
    with open(OUT, "a") as f:
        for pod, mdict in tick_data.items():
            for m, v in mdict.items():
                f.write(f"{t}\t{pod}\t{m}\t{int(v)}\n")

    # Emit one-line event: total alloc, max alloc, max rss across pods
    if tick_data:
        allocs = [d.get("otelcol_process_runtime_heap_alloc_bytes", 0) for d in tick_data.values()]
        rsss = [d.get("otelcol_process_memory_rss", 0) for d in tick_data.values()]
        total_acc = sum(d.get("otelcol_receiver_accepted_spans", 0) for d in tick_data.values())
        total_sent = sum(d.get("otelcol_exporter_sent_spans_otlp", 0) for d in tick_data.values())
        total_refused = sum(d.get("otelcol_processor_refused_spans", 0) for d in tick_data.values())
        total_enq_fail = sum(d.get("otelcol_exporter_enqueue_failed_spans_otlp", 0) for d in tick_data.values())
        max_alloc_mb = int(max(allocs) / 1048576) if allocs else 0
        max_rss_mb = int(max(rsss) / 1048576) if rsss else 0
        sum_alloc_mb = int(sum(allocs) / 1048576)
        print(
            f"t={t} pods={len(tick_data)} "
            f"max_alloc={max_alloc_mb}MB max_rss={max_rss_mb}MB sum_alloc={sum_alloc_mb}MB "
            f"acc={int(total_acc):,} sent={int(total_sent):,} "
            f"ref={int(total_refused):,} enq_fail={int(total_enq_fail):,}",
            flush=True
        )
    time.sleep(INTERVAL)
