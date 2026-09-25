#!/usr/bin/env python3
"""Tomislav-RetCtx: verify one protobuf/file collector's capacity with independent senders."""

import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import urllib.request

import yaml

from run_spanload_ramp import REPO, METRICS, command, epoch, process_stats, scrape, sha256, write_json


IMAGE = "spanload-collector:ramp-20260914"
BINARY = REPO / "utils/spanload/bin/spanload"
PROFILES = REPO / "utils/spanload/profiles/uber-day1-random2-8"
FRACTION = 249659504 / 475488643
GENERATOR_CORES = ["4-7", "14-17", "8-11", "0,1,18,19"]


def cases():
    # Hold one collector, protobuf/file export, payloads, and aggregate offer fixed;
    # increase independent processes, connections, workers, and sender cores.
    result = []
    for profile in ("zero", "semconv10-example"):
        for bridge in ("none", "sb"):
            for generators in (1, 2, 4):
                result.append(dict(profile=profile, bridge=bridge, exporter="proto", generators=generators,
                                   collector_cores=1, rate=4000000 if profile == "zero" else 1000000))
    # Profile separately so sampling overhead is not hidden in comparison runs.
    for profile in ("zero", "semconv10-example"):
        result.append(dict(profile=profile, bridge="none", exporter="proto", generators=4,
                           collector_cores=1, rate=4000000 if profile == "zero" else 1000000, pprof=True))
    for case in result:
        case["name"] = f'{case["profile"]}-{case["bridge"]}-{case["exporter"]}-{case["collector_cores"]}cpu-{case["generators"]}gen' + ("-pprof" if case.get("pprof") else "")
    return result


def fetch_profile(url, target):
    with urllib.request.urlopen(url + "/debug/pprof/profile?seconds=10", timeout=20) as response:
        target.write_bytes(response.read())


def run_case(case, out, seconds):
    directory = out / case["name"]
    directory.mkdir()
    write_json(directory / "case.json", case)
    sockets = [socket.socket() for _ in range(4)]
    for sock in sockets:
        sock.bind(("127.0.0.1", 0))
    grpc_port, http_port, metrics_port, pprof_port = [sock.getsockname()[1] for sock in sockets]
    config = yaml.safe_load((REPO / "utils/spanload/collector.yaml").read_text())
    config["receivers"]["otlp"]["protocols"]["grpc"]["endpoint"] = f"127.0.0.1:{grpc_port}"
    config["receivers"]["otlp"]["protocols"]["http"]["endpoint"] = f"127.0.0.1:{http_port}"
    config["service"]["telemetry"]["metrics"]["readers"][0]["pull"]["exporter"]["prometheus"]["port"] = metrics_port
    if case["exporter"] == "nop":
        config["exporters"] = {"nop": {}}
        config["service"]["pipelines"]["traces"]["exporters"] = ["nop"]
    else:
        config["exporters"]["file"]["format"] = case["exporter"]
    if case.get("pprof"):
        config["extensions"] = {"pprof": {"endpoint": f"127.0.0.1:{pprof_port}"}}
        config["service"]["extensions"] = ["pprof"]
    (directory / "collector.yaml").write_text(yaml.safe_dump(config, sort_keys=False))
    for sock in sockets:
        sock.close()
    collector_name = f"spanload-capacity-{os.getpid()}"
    argv = ["docker", "run", "-d", "--name", collector_name, "--network", "host",
            "--cpuset-cpus", "2" if case["collector_cores"] == 1 else "2,3,12,13",
            "--cpus", str(case["collector_cores"]), "--memory", "4g", "--memory-swap", "4g",
            "-e", f'GOMAXPROCS={case["collector_cores"]}', "-e", "GOGC=100",
            "-v", f"{directory}/collector.yaml:/collector.yaml:ro", IMAGE, "--config", "/collector.yaml"]
    write_json(directory / "collector-command.json", argv)
    command(argv)
    generators, streams, samples = [], [], []
    pool, profile_future = None, None
    try:
        details = json.loads(command(["docker", "inspect", collector_name]))[0]
        write_json(directory / "container.json", details)
        pid = details["State"]["Pid"]
        metrics_url = f"http://127.0.0.1:{metrics_port}/metrics"
        deadline = time.monotonic() + 20
        while True:
            try:
                scrape(metrics_url)
                with socket.create_connection(("127.0.0.1", grpc_port), timeout=1):
                    pass
                break
            except OSError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("collector readiness timed out")
                time.sleep(.1)
        raw, before, _, _ = scrape(metrics_url)
        (directory / "before.prom").write_text(raw)
        for i in range(case["generators"]):
            argv = ["taskset", "-c", GENERATOR_CORES[i], str(BINARY), "--endpoint", f"127.0.0.1:{grpc_port}",
                    "--insecure", "--profile", case["profile"], "--bridge", case["bridge"],
                    "--rates", str(case["rate"] // case["generators"]), "--duration", f"{seconds}s",
                    "--workers", "8", "--queue", "64", "--batch-size", "512", "--timeout", "2s",
                    "--drain-timeout", "5s", "--report-interval", "2s", "--payload-seed", "42",
                    "--allow-errors", "--out", str(directory / f"generator-{i}")]
            if case["bridge"] != "none":
                argv += ["--bridge-distribution", str(PROFILES / (case["bridge"] + ".json")),
                         "--checkpoint-fraction", repr(FRACTION)]
            write_json(directory / f"generator-{i}-command.json", argv)
            stdout = (directory / f"generator-{i}-stdout.jsonl").open("w")
            stderr = (directory / f"generator-{i}-stderr.log").open("w")
            streams += [stdout, stderr]
            generators.append(subprocess.Popen(argv, stdout=stdout, stderr=stderr,
                env={**os.environ, "GOMAXPROCS": "4", "GOGC": "100"}))
        raw_directory = directory / "metrics"
        raw_directory.mkdir()
        launched = time.monotonic()
        while any(p.poll() is None for p in generators):
            began = time.monotonic()
            if case.get("pprof") and profile_future is None and began - launched >= 6:
                pool = ThreadPoolExecutor(max_workers=1)
                profile_future = pool.submit(fetch_profile, f"http://127.0.0.1:{pprof_port}", directory / "cpu.pprof")
            raw, counters, timestamp, latency = scrape(metrics_url)
            (raw_directory / f"{len(samples):04d}.prom").write_text(raw)
            samples.append({"time": timestamp, "counters": counters, "scrape_seconds": latency,
                            "collector": process_stats(pid), "generators": [process_stats(p.pid) for p in generators]})
            if samples[-1]["collector"]["rss_bytes"] > 3.5 * (1 << 30):
                raise RuntimeError("collector approaches memory limit")
            time.sleep(max(0, 1 - (time.monotonic() - began)))
        assert all(p.returncode == 0 for p in generators), "generator failed; inspect stderr"
        if profile_future:
            profile_future.result()
        time.sleep(1)
        raw, after, _, _ = scrape(metrics_url)
        (directory / "after.prom").write_text(raw)
        phases = []
        for i in range(case["generators"]):
            records = [json.loads(line) for line in (directory / f"generator-{i}/results.jsonl").read_text().splitlines()]
            phase, = [r for r in records if r["type"] == "phase"]
            assert phase["offered_spans"] == phase["attempted_spans"] + phase["queue_dropped_spans"] + phase["unsent_spans"]
            assert phase["attempted_spans"] == phase["acknowledged_spans"] + phase["failed_spans"] + phase["rejected_spans"]
            assert phase["target_spans"] == phase["offered_spans"] + phase["scheduler_unissued_spans"]
            phases.append(phase)
        acknowledged = sum(p["acknowledged_spans"] for p in phases)
        assert after[METRICS[0]] - before[METRICS[0]] == acknowledged
        if case["exporter"] != "nop":
            assert after[METRICS[2]] - before[METRICS[2]] == acknowledged
        assert after[METRICS[1]] == before[METRICS[1]] and after[METRICS[3]] == before[METRICS[3]]
        start = max(epoch(p["started"]) for p in phases) + 5
        end = min(epoch(p["started"]) + p["generation_seconds"] for p in phases) - 1
        eligible = [s for s in samples if start <= s["time"] <= end and s["collector"] and all(s["generators"])]
        assert len(eligible) >= 5
        first, last = eligible[0], eligible[-1]
        duration = last["time"] - first["time"]
        row = {**case, "steady_seconds": duration,
               "accepted_spans_per_second": (last["counters"][METRICS[0]] - first["counters"][METRICS[0]]) / duration,
               "exported_spans_per_second": (last["counters"][METRICS[2]] - first["counters"][METRICS[2]]) / duration if case["exporter"] != "nop" else None,
               "collector_cpu_cores": (last["collector"]["cpu_seconds"] - first["collector"]["cpu_seconds"]) / duration,
               "generator_cpu_cores": [(last["generators"][i]["cpu_seconds"] - first["generators"][i]["cpu_seconds"]) / duration for i in range(case["generators"])],
               "collector_peak_rss_mib": max(s["collector"]["rss_bytes"] for s in samples) / (1 << 20),
               "counts_reconciled": True, "acknowledged_spans": acknowledged,
               "mean_rpc_seconds": sum(p["sum_rpc_seconds"] for p in phases) / sum(p["export_requests"] for p in phases),
               **{key: sum(p[key] for p in phases) for key in ("queue_dropped_spans", "failed_spans", "rejected_spans", "unsent_spans", "scheduler_unissued_spans")}}
        write_json(directory / "summary.json", row)
        print(json.dumps(row), flush=True)
        return row
    finally:
        for process in generators:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        for stream in streams:
            stream.close()
        if pool:
            pool.shutdown()
        write_json(directory / "samples.json", samples)
        (directory / "collector.log").write_text(command(["docker", "logs", collector_name]))
        command(["docker", "stop", "-t", "10", collector_name])
        final = json.loads(command(["docker", "inspect", collector_name]))[0]
        write_json(directory / "container-final.json", final)
        command(["docker", "rm", collector_name])
        assert final["State"]["ExitCode"] == 0 and not final["State"]["OOMKilled"] and final["RestartCount"] == 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--seconds", type=int, default=25)
    parser.add_argument("--cases", help="comma-separated exact case names; default runs the full diagnostic matrix")
    args = parser.parse_args()
    if args.seconds < 20:
        parser.error("use at least 20 seconds for steady-window and CPU-profile coverage")
    selected = cases()
    if args.cases:
        names = args.cases.split(",")
        selected = [case for case in selected if case["name"] in names]
        if {case["name"] for case in selected} != set(names):
            parser.error("unknown case name")
    out = args.out.resolve()
    out.mkdir(parents=True)
    write_json(out / "manifest.json", {"created": datetime.now(timezone.utc).isoformat(), "seconds": args.seconds,
        "generator_binary_sha256": sha256(BINARY), "collector_image": json.loads(command(["docker", "image", "inspect", IMAGE]))[0],
        "cases": selected, "checkpoint_fraction": FRACTION,
        "cpu_topology": command(["lscpu", "-e=CPU,CORE,SOCKET,NODE"]),
        "note": "Tomislav-RetCtx: comparison controls; nop results are receiver throughput, not serialized export throughput."})
    shutil.copy2(__file__, out / Path(__file__).name)
    shutil.copy2(REPO / "utils/run_spanload_ramp.py", out / "run_spanload_ramp.py")
    shutil.copytree(PROFILES, out / "profiles")
    shutil.copy2(REPO / "utils/spanload/profiles/semconv10-example.json", out / "semconv10-example.json")
    results = []
    for case in selected:
        print("Starting " + case["name"], flush=True)
        results.append(run_case(case, out, args.seconds))
        write_json(out / "results.json", results)
    write_json(out / "completion.json", {"completed": len(results), "all_counts_reconciled": True})


if __name__ == "__main__":
    main()
