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
import signal
import socket
import statistics
import subprocess
import time
import urllib.request

import psutil
import yaml


REPO = Path(__file__).resolve().parent.parent
METRICS = ("otelcol_receiver_accepted_spans", "otelcol_receiver_refused_spans",
           "otelcol_exporter_sent_spans", "otelcol_exporter_send_failed_spans")


def write_json(path, value):
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, allow_nan=False) + "\n")
    temporary.replace(path)


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


def combine_phases(phases):
    """Tomislav-RetCtx: sum sender counts; measure only their shared active interval."""
    combined = {"type": "combined_phase", "generator_processes": len(phases),
                "started": max(phases, key=lambda p: epoch(p["started"]))["started"]}
    combined["generation_seconds"] = min(epoch(p["started"]) + p["generation_seconds"] for p in phases) - epoch(combined["started"])
    if combined["generation_seconds"] <= 0:
        raise ValueError("generator phases do not overlap")
    for key in ("offered_spans", "attempted_spans", "acknowledged_spans", "attempted_checkpoint_spans",
                "attempted_protobuf_bytes", "queue_dropped_spans", "failed_spans", "rejected_spans",
                "unsent_spans", "scheduler_unissued_spans"):
        combined[key] = sum(p[key] for p in phases)
    combined["offered_spans_per_second"] = sum(p["offered_spans"] / p["generation_seconds"] for p in phases)
    payloads = [p["attempted_checkpoint_payload"] for p in phases if p.get("attempted_checkpoint_payload")]
    if payloads:
        count = sum(p["count"] for p in payloads)
        total = sum(p["total_bytes"] for p in payloads)
        combined["attempted_checkpoint_payload"] = {
            "count": count, "total_bytes": total, "mean_bytes": total / count}
    return combined


def summarize_step(variant, rate, phase, samples, args):
    start = epoch(phase["started"])
    eligible = [s for s in samples if "counters" in s and s["collector"] and s["generator"]
                and start + args.discard_first <= s["time"] <= start + phase["generation_seconds"] - 1]
    if len(eligible) < 5:
        raise RuntimeError("too few steady-window metric samples")
    first, last = eligible[0], eligible[-1]
    duration = last["time"] - first["time"]
    if duration < getattr(args, "min_steady_seconds", 0):
        raise RuntimeError(f"steady measurement window is only {duration:.3f}s")
    rates = {name: (last["counters"][name] - first["counters"][name]) / duration for name in METRICS}
    cp = phase.get("attempted_checkpoint_payload")
    row = {"variant": variant, "profile": getattr(args, "profile", "zero"),
           "export_format": getattr(args, "export_format", "json"), "target_spans_per_second": rate,
           "offered_spans_per_second": phase.get("offered_spans_per_second", phase["offered_spans"] / phase["generation_seconds"]),
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
    if "generators" in first:
        row["generator_processes"] = len(first["generators"])
        row["generator_cpu_cores_per_process"] = [
            (b["cpu_seconds"] - a["cpu_seconds"]) / duration
            for a, b in zip(first["generators"], last["generators"])]
    row["capacity_limited"] = (row["collector_cpu_cores"] >= .94 and row["exported_spans_per_second"] < rate * .93)
    return row


def has_plateau(rows):
    """Tomislav-RetCtx: detect saturation independently of whether the ramp stops."""
    if len(rows) < 2 or not all(row["capacity_limited"] for row in rows[-2:]):
        return False
    previous, current = [row["exported_spans_per_second"] for row in rows[-2:]]
    return previous > 0 and abs(current / previous - 1) < .12


def run_step(variant, rate, directory, collector_pid, grpc_port, metrics_url, fraction, args):
    stem = f"{rate:09d}"
    output = directory / stem
    processes, streams, outputs, samples = [], [], [], []
    raw_directory = directory / (stem + "-metrics")
    raw_directory.mkdir()
    try:
        before_raw, before_counters, _, _ = scrape(metrics_url)
        (directory / (stem + "-before.prom")).write_text(before_raw)
        for index, cores in enumerate(args.generator_cpus):
            suffix = "" if len(args.generator_cpus) == 1 else f"-generator-{index}"
            target = output.with_name(output.name + suffix)
            outputs.append(target)
            argv = ["taskset", "-c", cores, str(args.binary), "--endpoint", f"127.0.0.1:{grpc_port}",
                    "--insecure", "--profile", args.profile, "--bridge", variant,
                    "--rates", str(rate // len(args.generator_cpus) + (index < rate % len(args.generator_cpus))),
                    "--duration", f"{args.seconds}s", "--workers", str(args.workers), "--queue", "64", "--batch-size", "512",
                    "--timeout", "2s", "--drain-timeout", "5s", "--report-interval", "2s", "--payload-seed", "42",
                    "--allow-errors", "--out", str(target)]
            if variant != "none":
                argv += ["--bridge-distribution", str(args.profiles / (variant + ".json")), "--checkpoint-fraction", repr(fraction)]
            write_json(directory / (stem + suffix + "-command.json"), argv)
            stdout = (directory / (stem + suffix + "-stdout.jsonl")).open("w")
            stderr = (directory / (stem + suffix + "-stderr.log")).open("w")
            streams += [stdout, stderr]
            processes.append(subprocess.Popen(argv, stdout=stdout, stderr=stderr,
                env={**os.environ, "GOMAXPROCS": "4", "GOGC": "100"}))
        while any(process.poll() is None for process in processes):
            begun = time.monotonic()
            try:
                raw, counters, timestamp, latency = scrape(metrics_url)
                (raw_directory / f"{len(samples):04d}.prom").write_text(raw)
                generators = [process_stats(process.pid) for process in processes]
                aggregate = {key: sum(p[key] for p in generators) for key in generators[0]} if all(generators) else None
                sample = {"time": timestamp, "scrape_seconds": latency, "counters": counters,
                          "collector": process_stats(collector_pid), "generator": aggregate, "generators": generators}
            except OSError as error:
                sample = {"time": time.time(), "error": str(error)}
            samples.append(sample)
            if time.monotonic() - begun < 1:
                time.sleep(1 - (time.monotonic() - begun))
        if any(process.returncode != 0 for process in processes):
            raise RuntimeError(f"spanload failed; see {stem} generator stderr logs")
        time.sleep(1)
        after_raw, after_counters, _, _ = scrape(metrics_url)
        (directory / (stem + "-after.prom")).write_text(after_raw)
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        for stream in streams:
            stream.close()
        write_json(directory / (stem + "-samples.json"), samples)
    phases = []
    for output in outputs:
        records = [json.loads(line) for line in (output / "results.jsonl").read_text().splitlines()]
        phase, = [r for r in records if r["type"] == "phase"]
        assert phase["offered_spans"] == phase["attempted_spans"] + phase["queue_dropped_spans"] + phase["unsent_spans"]
        assert phase["attempted_spans"] == phase["acknowledged_spans"] + phase["failed_spans"] + phase["rejected_spans"]
        assert phase["target_spans"] == phase["offered_spans"] + phase["scheduler_unissued_spans"]
        phases.append(phase)
    phase = combine_phases(phases)
    write_json(directory / (stem + "-combined-phase.json"), phase)
    row = summarize_step(variant, rate, phase, samples, args)
    # Full-phase delivery accounting includes drain and remains separate from steady rates.
    deltas = {name: after_counters[name] - before_counters[name] for name in METRICS}
    row["full_phase_collector_deltas"] = deltas
    row["acknowledged_spans"] = phase["acknowledged_spans"]
    row["counts_reconciled"] = (deltas[METRICS[0]] == deltas[METRICS[2]] == phase["acknowledged_spans"])
    write_json(directory / (stem + "-summary.json"), row)
    print(json.dumps(row), flush=True)
    if not row["counts_reconciled"]:
        raise RuntimeError("collector acceptance/export counters do not reconcile with acknowledgements; inspect raw artifacts")
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
    config = yaml.safe_load(args.collector_config.read_text())
    config["exporters"]["file"]["format"] = args.export_format
    config["receivers"]["otlp"]["protocols"]["grpc"]["endpoint"] = f"127.0.0.1:{grpc_port}"
    config["receivers"]["otlp"]["protocols"]["http"]["endpoint"] = f"127.0.0.1:{http_port}"
    config["service"]["telemetry"]["metrics"]["readers"][0]["pull"]["exporter"]["prometheus"]["port"] = metrics_port
    (directory / "collector.yaml").write_text(yaml.safe_dump(config, sort_keys=False))
    for sock in reservations:
        sock.close()
    name = f"spanload-ramp-{os.getpid()}-{variant}"
    argv = ["docker", "run", "-d", "--name", name, "--network", "host", "--cpuset-cpus", str(args.collector_cpu),
            "--cpus", "1", "--memory", "4g", "--memory-swap", "4g", "-e", "GOMAXPROCS=1", "-e", "GOGC=100"]
    if args.collector_gomemlimit:
        argv += ["-e", "GOMEMLIMIT=" + args.collector_gomemlimit]
    argv += ["-v", f"{directory}/collector.yaml:/collector.yaml:ro", args.collector_image, "--config", "/collector.yaml"]
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
                # Metrics can bind before OTLP. Wait for the ingest listener
                # so a cold gRPC connection does not enter reconnect backoff.
                with socket.create_connection(("127.0.0.1", grpc_port), timeout=1):
                    pass
                break
            except OSError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("collector did not become ready")
                time.sleep(.1)
        for rate in args.rates:
            write_json(args.out / "status.json", {"variant": variant, "target_spans_per_second": rate,
                       "finished_steps_in_variant": len(rows), "planned_steps_in_variant": len(args.rates),
                       "collector_name": name, "collector_pid": pid, "updated": datetime.now(timezone.utc).isoformat()})
            row = run_step(variant, rate, directory, pid, grpc_port, url, fraction, args)
            rows.append(row)
            write_json(directory / "ramp.json", rows)
            if max(row["generator_cpu_cores_per_process"]) >= 3.5:
                raise RuntimeError("generator is near its CPU budget; add generator resources before interpreting saturation")
            if row["collector_peak_rss_bytes"] >= 3.5 * (1 << 30):
                raise RuntimeError("collector memory approaches its budget")
            saturated = saturated or has_plateau(rows)
            if saturated and not args.complete_grid:
                break
        write_json(directory / "completion.json", {"saturated": saturated, "steps": len(rows),
                   "complete_grid": len(rows) == len(args.rates),
                   "plateau_spans_per_second": statistics.mean(r["exported_spans_per_second"] for r in rows[-2:]) if saturated else None})
        return rows, saturated
    finally:
        (directory / "collector.log").write_text(command(["docker", "logs", name]))
        command(["docker", "stop", "-t", "10", name])
        final = json.loads(command(["docker", "inspect", name]))[0]
        write_json(directory / "container-final.json", final)
        command(["docker", "rm", name])
        assert final["State"]["ExitCode"] == 0 and not final["State"]["OOMKilled"] and final["RestartCount"] == 0


def plot_results(rows, out, profile="zero"):
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
    axes[0].set_ylim(0, max(r["exported_spans_per_second"] for r in rows) / 1000 * 1.12)
    axes[1].set_ylabel("Collector CPU (% of one core)")
    axes[0].legend(fontsize=9)
    axes[1].set_ylim(0, 110)
    label = "Zero semconv attributes" if profile == "zero" else "10 semconv attributes (example profile)"
    formats = {r.get("export_format", "json") for r in rows}
    if len(formats) != 1:
        raise ValueError("plot one export format at a time")
    export_format, = formats
    fig.suptitle(f"{label} · 1 collector CPU, 4 GiB · {export_format.upper()} export to /dev/null")
    for suffix in ("png", "pdf", "svg"):
        fig.savefig(out / ("ramp." + suffix), dpi=180)
    plt.close(fig)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--binary", type=Path, default=REPO / "utils/spanload/bin/spanload")
    parser.add_argument("--profiles", type=Path, default=REPO / "utils/spanload/profiles/uber-day1-random2-8")
    parser.add_argument("--collector-image", default="spanload-collector:ramp-20260914")
    parser.add_argument("--collector-config", type=Path, default=REPO / "utils/spanload/collector.yaml")
    parser.add_argument("--collector-gomemlimit", help="explicit Go memory budget for the collector, e.g. 2400MiB")
    # Tomislav-RetCtx: the intended /dev/null experiment retains protobuf
    # serialization. Record the format explicitly so JSON runs cannot be confused with it.
    parser.add_argument("--export-format", choices=("proto", "json"), default="proto")
    # Tomislav-RetCtx: repeat the same ramp with ten baseline attributes;
    # bridge metadata remains additional to the selected span profile.
    parser.add_argument("--profile", choices=("zero", "semconv10-example"), default="zero")
    parser.add_argument("--variants", default="none,pb,cgpb,sb")
    parser.add_argument("--rates", default="10000,25000,50000,75000,100000,150000,200000,300000,450000,650000,900000,1200000,1600000,2000000")
    parser.add_argument("--seconds", type=int, default=25)
    parser.add_argument("--discard-first", type=int, default=5)
    parser.add_argument("--min-steady-seconds", type=float, default=0)
    parser.add_argument("--complete-grid", action="store_true", help="record saturation but run every requested rate for every variant")
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--collector-cpu", type=int, default=2)
    parser.add_argument("--generator-cpus", action="append", help="CPU set per generator process; repeat for independent senders (default: 4-7)")
    args = parser.parse_args()
    args.generator_cpus = args.generator_cpus or ["4-7"]
    args.out = args.out.resolve()
    args.collector_config = args.collector_config.resolve()
    args.rates = [int(n) for n in args.rates.split(",")]
    variants = args.variants.split(",")
    if args.seconds < args.discard_first + 8 or any(r < len(args.generator_cpus) for r in args.rates) or args.rates != sorted(set(args.rates)):
        parser.error("use increasing positive rates and at least 8 seconds after the initial discard")
    if args.discard_first < 0 or args.min_steady_seconds < 0 or args.seconds < args.discard_first + args.min_steady_seconds + 3:
        parser.error("allow at least 3 seconds beyond settling and the minimum steady measurement window")
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
                     "collector_config_sha256": sha256(args.collector_config),
                     "note": "Tomislav-RetCtx: one ramp per variant; warmup/drain excluded from steady-window rates. " +
                     ("Complete the entire shared rate grid even after saturation." if args.complete_grid else
                      "Stop after two CPU-saturated overload points with <12% throughput change.")})
    write_json(args.out / "manifest.json", settings)
    shutil.copytree(args.profiles, args.out / "profiles")
    if args.profile == "semconv10-example":
        shutil.copy2(REPO / "utils/spanload/profiles/semconv10-example.json", args.out / "semconv10-example.json")
    shutil.copy2(__file__, args.out / "run_spanload_ramp.py")
    shutil.copy2(args.collector_config, args.out / "collector-template.yaml")
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
        plot_results(rows, args.out, args.profile)
        print(f"Completed {variant}: saturation={saturated}", flush=True)
    write_json(args.out / "completion.json", completion)
    write_json(args.out / "status.json", {"finished": True, "steps": len(rows), "updated": datetime.now(timezone.utc).isoformat()})
    if not args.complete_grid and not all(completion.values()):
        raise SystemExit("Some variants did not saturate; extend the rate range.")


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, signal.default_int_handler)
    main()
