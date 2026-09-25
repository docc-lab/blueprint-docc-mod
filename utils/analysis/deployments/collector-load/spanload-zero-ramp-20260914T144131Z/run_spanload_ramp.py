#!/usr/bin/env python3
"""Tomislav-RetCtx: isolated, CPU-limited collector ramps with steady-window metrics."""

import argparse
import csv
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import statistics
import subprocess
import time
import urllib.request

import psutil


REPO = Path(__file__).resolve().parent.parent
METRICS = ("otelcol_receiver_accepted_spans", "otelcol_receiver_refused_spans",
           "otelcol_exporter_sent_spans", "otelcol_exporter_send_failed_spans")


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, allow_nan=False) + "\n")


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def command(args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def cpu_state():
    result = {}
    for name in ("scaling_governor", "scaling_min_freq", "scaling_max_freq", "scaling_cur_freq"):
        path = Path("/sys/devices/system/cpu/cpu2/cpufreq") / name
        if path.exists():
            result[name] = path.read_text().strip()
    path = Path("/sys/devices/system/cpu/intel_pstate/no_turbo")
    if path.exists():
        result["no_turbo"] = path.read_text().strip()
    return result


def process_stats(pid):
    try:
        process = psutil.Process(pid)
        cpu = process.cpu_times()
        return {"cpu_seconds": cpu.user + cpu.system, "rss_bytes": process.memory_info().rss,
                "threads": process.num_threads()}
    except psutil.NoSuchProcess:
        return None


def scrape(url):
    start = time.time()
    with urllib.request.urlopen(url, timeout=3) as response:
        raw = response.read().decode()
    end = time.time()
    counters = {}
    for name in METRICS:
        counters[name] = sum(float(m.group(1)) for m in re.finditer(
            rf"^{name}(?:_total)?(?:\{{[^\n]*\}})?\s+([0-9.eE+-]+)", raw, re.M))
    return raw, counters, (start + end) / 2, end - start


def epoch(value):
    # Go records RFC3339 nanoseconds; Python 3.10 accepts 3/6 fractional digits.
    value = re.sub(r"\.(\d+)", lambda m: "." + (m.group(1) + "000000")[:6], value)
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def summarize_step(variant, rate, phase, samples, args):
    start = epoch(phase["started"])
    eligible = [s for s in samples if "counters" in s and s["collector"] and s["generator"]
                and start + args.discard_first <= s["time"] <= start + phase["generation_seconds"] - 1]
    if len(eligible) < 5:
        raise RuntimeError("too few steady-window metric samples")
    first, last = eligible[0], eligible[-1]
    duration = last["time"] - first["time"]
    rates = {name: (last["counters"][name] - first["counters"][name]) / duration for name in METRICS}
    cp = phase.get("attempted_checkpoint_payload")
    row = {"variant": variant, "target_spans_per_second": rate,
           "offered_spans_per_second": phase["offered_spans"] / phase["generation_seconds"],
           "steady_start": first["time"], "steady_end": last["time"], "steady_seconds": duration,
           "accepted_spans_per_second": rates[METRICS[0]], "exported_spans_per_second": rates[METRICS[2]],
           "refused_spans_per_second": rates[METRICS[1]], "export_failed_spans_per_second": rates[METRICS[3]],
           "collector_cpu_cores": (last["collector"]["cpu_seconds"] - first["collector"]["cpu_seconds"]) / duration,
           "generator_cpu_cores": (last["generator"]["cpu_seconds"] - first["generator"]["cpu_seconds"]) / duration,
           "collector_peak_rss_bytes": max(s["collector"]["rss_bytes"] for s in eligible),
           "generator_peak_rss_bytes": max(s["generator"]["rss_bytes"] for s in eligible),
           "queue_dropped_spans": phase["queue_dropped_spans"], "failed_spans": phase["failed_spans"],
           "rejected_spans": phase["rejected_spans"], "unsent_spans": phase["unsent_spans"],
           "scheduler_unissued_spans": phase["scheduler_unissued_spans"],
           "attempted_checkpoint_fraction": phase["attempted_checkpoint_spans"] / phase["attempted_spans"] if phase["attempted_spans"] else 0,
           "mean_checkpoint_value_bytes": cp["mean_bytes"] if cp else None,
           "attempted_protobuf_bytes_per_span": phase["attempted_protobuf_bytes"] / phase["attempted_spans"] if phase["attempted_spans"] else 0,
           "metric_scrape_errors": sum("error" in s for s in samples)}
    row["capacity_limited"] = (row["collector_cpu_cores"] >= .94 and row["exported_spans_per_second"] < rate * .93)
    return row


def run_step(variant, rate, directory, collector_pid, grpc_port, metrics_url, fraction, args):
    stem = f"{rate:09d}"
    output = directory / stem
    argv = ["taskset", "-c", args.generator_cpus, str(args.binary), "--endpoint", f"127.0.0.1:{grpc_port}",
            "--insecure", "--profile", "zero", "--bridge", variant, "--rates", str(rate),
            "--duration", f"{args.seconds}s", "--workers", str(args.workers), "--queue", "64", "--batch-size", "512",
            "--timeout", "2s", "--drain-timeout", "5s", "--report-interval", "2s", "--payload-seed", "42",
            "--metrics-url", metrics_url, "--settle", "1s", "--allow-errors", "--out", str(output)]
    if variant != "none":
        argv += ["--bridge-distribution", str(args.profiles / (variant + ".json")), "--checkpoint-fraction", repr(fraction)]
    write_json(directory / (stem + "-command.json"), argv)
    stdout = (directory / (stem + "-stdout.jsonl")).open("w")
    stderr = (directory / (stem + "-stderr.log")).open("w")
    process = subprocess.Popen(argv, stdout=stdout, stderr=stderr, env={**os.environ, "GOMAXPROCS": "4", "GOGC": "100"})
    samples = []
    raw_directory = directory / (stem + "-metrics")
    raw_directory.mkdir()
    try:
        while process.poll() is None:
            begun = time.monotonic()
            try:
                raw, counters, timestamp, latency = scrape(metrics_url)
                (raw_directory / f"{len(samples):04d}.prom").write_text(raw)
                sample = {"time": timestamp, "scrape_seconds": latency, "counters": counters,
                          "collector": process_stats(collector_pid), "generator": process_stats(process.pid)}
            except OSError as error:
                sample = {"time": time.time(), "error": str(error)}
            samples.append(sample)
            if time.monotonic() - begun < 1:
                time.sleep(1 - (time.monotonic() - begun))
        if process.returncode != 0:
            raise RuntimeError(f"spanload exited {process.returncode}; see {stem}-stderr.log")
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        stdout.close()
        stderr.close()
        write_json(directory / (stem + "-samples.json"), samples)
    records = [json.loads(line) for line in (output / "results.jsonl").read_text().splitlines()]
    phase, = [r for r in records if r["type"] == "phase"]
    row = summarize_step(variant, rate, phase, samples, args)
    write_json(directory / (stem + "-summary.json"), row)
    print(json.dumps(row), flush=True)
    return row


def run_variant(variant, args, fraction):
    directory = args.out / variant
    directory.mkdir()
    reservations = []
    for _ in range(3):
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        reservations.append(sock)
    grpc_port, http_port, metrics_port = [s.getsockname()[1] for s in reservations]
    config = (REPO / "utils/spanload/collector.yaml").read_text()
    config = config.replace("14317", str(grpc_port)).replace("14318", str(http_port)).replace("18888", str(metrics_port))
    (directory / "collector.yaml").write_text(config)
    for sock in reservations:
        sock.close()
    name = f"spanload-ramp-{os.getpid()}-{variant}"
    argv = ["docker", "run", "-d", "--name", name, "--network", "host", "--cpuset-cpus", str(args.collector_cpu),
            "--cpus", "1", "--memory", "4g", "--memory-swap", "4g", "-e", "GOMAXPROCS=1", "-e", "GOGC=100",
            "-v", f"{directory}/collector.yaml:/collector.yaml:ro", args.collector_image, "--config", "/collector.yaml"]
    write_json(directory / "collector-command.json", argv)
    command(argv)
    rows = []
    saturated = False
    try:
        details = json.loads(command(["docker", "inspect", name]))[0]
        write_json(directory / "container.json", details)
        assert details["HostConfig"]["NanoCpus"] == 1_000_000_000
        assert details["HostConfig"]["Memory"] == 4 * (1 << 30)
        pid = details["State"]["Pid"]
        url = f"http://127.0.0.1:{metrics_port}/metrics"
        deadline = time.monotonic() + 20
        while True:
            try:
                scrape(url)
                break
            except OSError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("collector did not become ready")
                time.sleep(.1)
        for rate in args.rates:
            row = run_step(variant, rate, directory, pid, grpc_port, url, fraction, args)
            rows.append(row)
            write_json(directory / "ramp.json", rows)
            if row["generator_cpu_cores"] >= 3.5:
                raise RuntimeError("generator is near its CPU budget; add generator resources before interpreting saturation")
            if row["collector_peak_rss_bytes"] >= 3.5 * (1 << 30):
                raise RuntimeError("collector memory approaches its budget")
            if len(rows) >= 2 and row["capacity_limited"] and rows[-2]["capacity_limited"]:
                gain = row["exported_spans_per_second"] / rows[-2]["exported_spans_per_second"] - 1
                if abs(gain) < .12:
                    saturated = True
                    break
        write_json(directory / "completion.json", {"saturated": saturated, "steps": len(rows),
                   "plateau_spans_per_second": statistics.mean(r["exported_spans_per_second"] for r in rows[-2:]) if saturated else None})
        return rows, saturated
    finally:
        (directory / "collector.log").write_text(command(["docker", "logs", name]))
        command(["docker", "stop", "-t", "10", name])
        write_json(directory / "container-final.json", json.loads(command(["docker", "inspect", name]))[0])
        command(["docker", "rm", name])


def plot_results(rows, out):
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.4), constrained_layout=True)
    colors = {"none": "#333333", "pb": "#0072B2", "cgpb": "#D55E00", "sb": "#009E73"}
    names = {"none": "Vanilla", "pb": "PB", "cgpb": "CGPB", "sb": "SB"}
    for variant in colors:
        series = [r for r in rows if r["variant"] == variant]
        if not series:
            continue
        x = [r["target_spans_per_second"] / 1000 for r in series]
        axes[0].plot(x, [r["exported_spans_per_second"] / 1000 for r in series], "o-", color=colors[variant], label=names[variant])
        axes[1].plot(x, [r["collector_cpu_cores"] * 100 for r in series], "o-", color=colors[variant], label=names[variant])
    top = max(r["target_spans_per_second"] for r in rows) / 1000
    axes[0].plot([0, top], [0, top], linestyle="--", color="#999999", linewidth=1, label="Export = offered")
    axes[1].axhline(100, color="#999999", linestyle="--", linewidth=1)
    for axis in axes:
        axis.set_xlabel("Offered rate (thousand spans/s)")
        axis.grid(alpha=.25)
        axis.set_xlim(left=0)
        axis.set_ylim(bottom=0)
    axes[0].set_ylabel("Collector export rate (thousand spans/s)")
    axes[1].set_ylabel("Collector CPU (% of one core)")
    axes[0].legend(fontsize=9)
    axes[1].set_ylim(0, 110)
    fig.suptitle("Zero semconv attributes · 1 collector CPU, 4 GiB · JSON export to /dev/null")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(out / ("ramp." + suffix), dpi=180)
    plt.close(fig)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--binary", type=Path, default=REPO / "utils/spanload/bin/spanload")
    parser.add_argument("--profiles", type=Path, default=REPO / "utils/spanload/profiles/uber-day1-random2-8")
    parser.add_argument("--collector-image", default="spanload-collector:ramp-20260914")
    parser.add_argument("--variants", default="none,pb,cgpb,sb")
    parser.add_argument("--rates", default="10000,25000,50000,75000,100000,150000,200000,300000,450000,650000,900000,1200000,1600000,2000000")
    parser.add_argument("--seconds", type=int, default=25)
    parser.add_argument("--discard-first", type=int, default=5)
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--collector-cpu", type=int, default=2)
    parser.add_argument("--generator-cpus", default="4-7")
    args = parser.parse_args()
    args.out = args.out.resolve()
    args.rates = [int(n) for n in args.rates.split(",")]
    variants = args.variants.split(",")
    if args.seconds < args.discard_first + 8 or any(r <= 0 for r in args.rates) or args.rates != sorted(set(args.rates)):
        parser.error("use increasing positive rates and at least 8 seconds after the initial discard")
    if any(v not in ("none", "pb", "cgpb", "sb") for v in variants):
        parser.error("unknown variant")
    args.out.mkdir(parents=True)
    manifest = json.loads((args.profiles / "manifest.json").read_text())
    fractions = {item["payload_count"] / manifest["corpus"]["spans"] for item in manifest["bridges"].values()}
    if len(fractions) != 1:
        raise ValueError("this comparison expects the same measured checkpoint fraction for all bridge types")
    fraction, = fractions
    settings = {k: str(v) if isinstance(v, Path) else v for k, v in vars(args).items()}
    settings.update({"checkpoint_fraction": fraction, "generator_binary_sha256": sha256(args.binary),
                     "collector_image": json.loads(command(["docker", "image", "inspect", args.collector_image]))[0],
                     "cpu_frequency": cpu_state(), "cpu_topology": command(["lscpu", "-e=CPU,CORE,SOCKET,NODE"]),
                     "created": datetime.now(timezone.utc).isoformat(), "source_revision": command(["git", "-C", str(REPO), "rev-parse", "HEAD"]),
                     "note": "Tomislav-RetCtx: one ramp per variant; warmup/drain excluded from steady-window rates; stop after two CPU-saturated overload points with <12% throughput change."})
    write_json(args.out / "manifest.json", settings)
    shutil.copytree(args.profiles, args.out / "profiles")
    shutil.copy2(__file__, args.out / "run_spanload_ramp.py")
    rows, completion = [], {}
    for variant in variants:
        print(f"Starting {variant} ramp", flush=True)
        values, saturated = run_variant(variant, args, fraction)
        rows.extend(values)
        completion[variant] = saturated
        write_json(args.out / "results.json", rows)
        with (args.out / "results.csv").open("w", newline="") as stream:
            writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)
        plot_results(rows, args.out)
        print(f"Completed {variant}: saturation={saturated}", flush=True)
    write_json(args.out / "completion.json", completion)
    if not all(completion.values()):
        raise SystemExit("Some variants did not saturate; extend the rate range.")


if __name__ == "__main__":
    main()
