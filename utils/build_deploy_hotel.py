#!/usr/bin/env python3
"""Tomislav-RetCtx: Hotel generation/build/deploy with Social Network collector defaults."""

import argparse
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

from inject_perf_env import GC_MODES, JAEGER_ENV, OTELCOL_ENV, OTELCOL_RES
import checkpoint_distance
import reverse_policy

REPO = Path(__file__).resolve().parents[1]
APP = REPO / "examples/dsb_hotel"
SERVICES = ("frontend", "search", "geo", "rate", "profile", "recomd", "user", "reserv")
SPECS = [f"docker_{kind}{storage}" for kind in ("pb", "cgpb", "sb", "v")
         for storage in ("", "_es")] + ["docker_rc_es"]


def run(command, **kwargs):
    print("+ " + " ".join(map(str, command)), flush=True)
    return subprocess.run(list(map(str, command)), check=True, **kwargs)


def load(path):
    import yaml
    return yaml.safe_load(path.read_text())


def save(path, document):
    import yaml
    path.write_text(yaml.safe_dump(document, sort_keys=False))


def env_map(entries):
    if isinstance(entries, dict):
        return dict(entries)
    return dict(entry.split("=", 1) for entry in (entries or []))


def configure_collector(path, args):
    config = load(path)
    discovery = config["receivers"].get("configdiscovery")
    if discovery is None:
        raise ValueError("collector config needs receivers.configdiscovery for the SDK")
    checkpoint_distance.configure(discovery.setdefault("config_map", {}),
                                  args.cpd, args.cpd_min, args.cpd_max, default=3)
    reverse_policy.configure(discovery["config_map"], args.reverse_policy, args.reverse_probability)
    processors = config.setdefault("processors", {})
    if args.collector == "passthrough":
        # Match social network's overhead runs: no collector shedding.
        for key in list(processors):
            if key.split("/")[0] in ("priority", "memory_limiter"):
                del processors[key]
        processors.setdefault("batch", {})
        config["service"]["pipelines"]["traces"]["processors"] = ["batch"]
    elif "priority" in processors:
        processors["priority"] = {
            "check_interval": "100ms", "soft_percentage": args.soft,
            "hard_percentage": args.hard, "cp_safety_factor": args.cp_safety,
            "force_gc": True, "gc_soft_interval": "1s", "gc_ultrasoft_interval": "0s",
        }
    save(path, config)


def configure_compose(path, suffix, args):
    compose = load(path)
    for name, service in compose["services"].items():
        env = env_map(service.get("environment"))
        if name in {f"{s}_service_{suffix}_ctr" for s in SERVICES}:
            env.update(GC_MODES[args.gc], OTLP_RETRY="off")
            if args.sample_ratio is not None:
                env["OTEL_SAMPLE_RATIO"] = str(args.sample_ratio)
            # Stamp both on and off so an inherited build-shell setting cannot
            # accidentally change only part of the deployed application.
            env.update(REVERSE_TRUSS="on" if args.reverse_truss else "off",
                       RT_ROOT="on" if args.reverse_truss and name.startswith("frontend_") else "off",
                       RT_POLICY=args.rt_policy, RT_DEPTH=str(args.rt_depth),
                       RT_LEAF_REJECT=str(args.rt_leaf_reject))
        elif name == f"jaeger_{suffix}_ctr":
            env.update(JAEGER_ENV, COLLECTOR_OTLP_ENABLED="true")
        elif name == f"otelcol_{suffix}_ctr":
            env.update(OTELCOL_ENV)
        if env:
            service["environment"] = {k: str(v) for k, v in env.items()}
        # d2k8s handles explicitly tagged upstream images consistently.
        image = service.get("image", "")
        if image and ":" not in image.rsplit("/", 1)[-1] and "@" not in image:
            service["image"] += ":latest"
    save(path, compose)
    return compose


def prepare_manifests(directory, suffix, args):
    variant = suffix.replace("_", "-")
    revision = str(time.time_ns())
    for path in sorted(directory.glob("*.yaml")):
        doc = load(path)
        name = doc["metadata"]["name"]
        doc["metadata"].update(namespace=args.namespace)
        labels = {"app.kubernetes.io/part-of": "dsb-hotel",
                  "app.kubernetes.io/instance": variant}
        doc["metadata"].setdefault("labels", {}).update(labels)
        if doc["kind"] in ("Deployment", "DaemonSet"):
            template = doc["spec"]["template"]
            template["metadata"].setdefault("labels", {}).update(labels)
            template["metadata"].setdefault("annotations", {})["blueprint.uservices/build"] = revision
            if doc["kind"] == "Deployment":
                # Avoid overlapping service generations during an update.
                doc["spec"]["strategy"] = {"type": "Recreate"}
            for container in template["spec"]["containers"]:
                container["imagePullPolicy"] = "Always"
                ports = container.get("ports", [])
                if name == f"otelcol-{variant}-ctr":
                    container["resources"] = OTELCOL_RES
                    port = 4317
                elif ports:
                    port = ports[0]["containerPort"]
                else:
                    continue
                container["startupProbe"] = {
                    "tcpSocket": {"port": port}, "periodSeconds": 2,
                    "failureThreshold": 150,
                }
                container["readinessProbe"] = {
                    "tcpSocket": {"port": port}, "periodSeconds": 5,
                }
        if doc["kind"] == "Service" and name == f"frontend-service-{variant}-ctr":
            if args.nodeport is not None:
                doc["spec"]["type"] = "NodePort"
                if args.nodeport:
                    for port in doc["spec"]["ports"]:
                        if port["port"] == 2000:
                            port["nodePort"] = args.nodeport
        save(path, doc)


def one_per_node(directory, suffix, args):
    result = run(["kubectl", "get", "nodes", "-o", "json"], capture_output=True, text=True)
    nodes = []
    for node in json.loads(result.stdout)["items"]:
        if node.get("spec", {}).get("unschedulable"):
            continue
        if any(t["effect"] in ("NoSchedule", "NoExecute") for t in node.get("spec", {}).get("taints", [])):
            continue
        if not any(c["type"] == "Ready" and c["status"] == "True" for c in node["status"]["conditions"]):
            continue
        nodes.append(node["metadata"]["labels"]["kubernetes.io/hostname"])
    nodes.sort()
    if len(nodes) < len(SERVICES):
        raise ValueError(f"--one-per-node requires 8 ready schedulable nodes; found {len(nodes)}")
    variant = suffix.replace("_", "-")
    placement = {}
    for node, service in zip(nodes, SERVICES):
        names = [f"{service}-service-{variant}-ctr"]
        if service in ("geo", "rate", "profile", "recomd", "user", "reserv"):
            names.append(f"{service}-db-{variant}-ctr")
        if service in ("rate", "profile", "reserv"):
            names.append(f"{service}-cache-{variant}-ctr")
        placement[node] = [{name: {}} for name in names]
    path = directory.parent / "node-pinning.yaml"
    save(path, placement)
    return path


def build_images(compose, directory, manifests):
    images = {}
    for path in manifests.glob("*.yaml"):
        doc = load(path)
        if doc["kind"] in ("Deployment", "DaemonSet"):
            images[doc["metadata"]["name"]] = doc["spec"]["template"]["spec"]["containers"][0]["image"]
    done = set()
    for name, service in compose["services"].items():
        image = images[name.replace("_", "-")]
        if image in done:
            continue
        if "build" in service:
            build = service["build"]
            if isinstance(build, str):
                build = {"context": build}
            context = directory / build["context"]
            run(["docker", "build", "-t", image, "-f", context / build.get("dockerfile", "Dockerfile"), context])
        else:
            upstream = service["image"]
            run(["docker", "pull", upstream])
            if upstream != image:
                run(["docker", "tag", upstream, image])
        run(["docker", "push", image])
        done.add(image)


def apply(directory, args):
    # Namespaced apply only; there is no cross-application teardown.
    namespace = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": args.namespace}}
    run(["kubectl", "apply", "-f", "-"], input=json.dumps(namespace), text=True)
    run(["kubectl", "apply", "-f", directory])
    for path in sorted(directory.glob("*.yaml")):
        doc = load(path)
        if doc["kind"] in ("Deployment", "DaemonSet"):
            run(["kubectl", "-n", args.namespace, "rollout", "status",
                 f'{doc["kind"].lower()}/{doc["metadata"]["name"]}', f"--timeout={args.wait_timeout}"])


def parser():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    p.add_argument("-s", "--spec", choices=SPECS, default="docker_cgpb_es")
    p.add_argument("-n", "--name", help="Build name; output defaults to examples/dsb_hotel/build_<name>")
    p.add_argument("--output", type=Path, help="Alternative output directory (must not exist)")
    p.add_argument("--extra", default="", help="Extra suffix on generated resources, e.g. rev2")
    p.add_argument("--registry", default="10.10.1.1:30000")
    p.add_argument("--image-tag", default="latest", help="Registry tag; --skip-build must reference images already built")
    p.add_argument("--collector-image", help="Custom collector base image; defaults to <registry>/otelcontribcol:latest")
    p.add_argument("--collector-config", type=Path, help="Override the variant's collector YAML")
    p.add_argument("--collector", choices=("default", "passthrough"), default="default")
    p.add_argument("--cpd", type=int, help="Fixed checkpoint distance (default 3); exclusive with --cpd-min/--cpd-max")
    p.add_argument("--cpd-min", type=int, help="Minimum randomized checkpoint distance, inclusive (PB/CGPB/SB)")
    p.add_argument("--cpd-max", type=int, help="Maximum randomized checkpoint distance, inclusive (1..256)")
    p.add_argument("--soft", type=int, default=50)
    p.add_argument("--hard", type=int, default=70)
    p.add_argument("--cp-safety", type=float, default=1)
    p.add_argument("--gc", choices=tuple(GC_MODES), default="forced")
    p.add_argument("--rpc-timeout", default="5s")
    p.add_argument("--sample-ratio", type=float, help="OTel head sampling ratio, between 0 and 1")
    p.add_argument("--reverse-truss", action="store_true", help="Enable reverse checkpoints, consuming them at frontend")
    p.add_argument("--reverse-policy", choices=reverse_policy.POLICIES, help="Collector-discovered reverse routing policy (SDK default: ttl)")
    p.add_argument("--reverse-probability", type=float, help="Per-truss probability in [0,1]; requires --reverse-policy probability")
    p.add_argument("--rt-policy", choices=("1", "2"), default="1", help="Deprecated; use --reverse-policy")
    p.add_argument("--rt-depth", type=int, default=3, help="Deprecated; reverse distances use collector CPD settings")
    p.add_argument("--rt-leaf-reject", type=float, default=0, help="Probability of rejecting an unscheduled leaf checkpoint (0..1)")
    p.add_argument("--build-collector", action="store_true", help="Build and push the custom collector base image first")
    p.add_argument("--collector-src", type=Path, default=REPO.parent / "opentelemetry-collector-contrib")
    p.add_argument("--collector-toolchain", default="go1.24.13", help="Go toolchain for the collector's older dependencies")
    p.add_argument("--skip-build", action="store_true", help="Generate Compose and Kubernetes manifests without building/pushing images")
    p.add_argument("--frontend-deploy", action="store_true", help="Use one frontend Deployment instead of a DaemonSet")
    placement = p.add_mutually_exclusive_group()
    placement.add_argument("--node-pinning", type=Path, help="Node-pinning YAML, in the same format as social network")
    placement.add_argument("--one-per-node", action="store_true", help="Place 8 application services on distinct available nodes; implies --frontend-deploy")
    p.add_argument("--no-pin-requests", action="store_true", help="Ignore resources in the node-pinning file")
    p.add_argument("--namespace", default="dsb-hotel")
    p.add_argument("--nodeport", type=int, nargs="?", const=0, help="Expose frontend via NodePort; omit value to allocate a port")
    p.add_argument("--apply", action="store_true", help="Apply this build's manifests and wait for readiness")
    p.add_argument("--wait-timeout", default="5m", help="Rollout timeout for each workload")
    return p


def main():
    p = parser()
    args = p.parse_args()
    name = args.name or args.spec.removeprefix("docker_") + args.extra
    if not re.fullmatch(r"[a-z0-9][a-z0-9_]*", name):
        p.error("build name must contain only lowercase letters, digits and underscores")
    if not re.fullmatch(r"[a-z0-9_]{0,16}", args.extra):
        p.error("--extra must contain at most 16 lowercase letters, digits or underscores")
    if not re.fullmatch(r"[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?", args.namespace):
        p.error("--namespace must be a Kubernetes DNS label")
    if not re.fullmatch(r"[\w][\w.-]{0,127}", args.image_tag, flags=re.ASCII):
        p.error("--image-tag must be a Docker tag")
    try:
        checkpoint_distance.validate(args.cpd, args.cpd_min, args.cpd_max)
        reverse_policy.validate(args.reverse_policy, args.reverse_probability)
    except ValueError as error:
        p.error(str(error))
    if args.cpd_min is not None and not args.spec.startswith(("docker_pb", "docker_cgpb", "docker_sb")):
        p.error("random checkpoint distance requires PB, CGPB, or SB")
    if args.rt_depth <= 0:
        p.error("--rt-depth must be positive")
    if not 0 < args.soft < args.hard <= 100 or not math.isfinite(args.cp_safety) or args.cp_safety <= 0:
        p.error("require 0 < --soft < --hard <= 100 and positive --cp-safety")
    if not 0 <= args.rt_leaf_reject <= 1 or (args.sample_ratio is not None and not 0 <= args.sample_ratio <= 1):
        p.error("sampling and rejection probabilities must be between 0 and 1")
    if args.nodeport is not None and args.nodeport != 0 and not 30000 <= args.nodeport <= 32767:
        p.error("--nodeport must be 30000..32767 (or omit its value for automatic allocation)")
    if args.node_pinning and not args.frontend_deploy:
        p.error("--node-pinning requires --frontend-deploy so the frontend can be pinned")
    if args.reverse_truss and args.spec == "docker_rc_es":
        p.error("the random-checkpoint control does not implement reverse checkpoints")
    if args.reverse_policy is not None and not args.spec.startswith(("docker_pb", "docker_cgpb", "docker_sb")):
        p.error("reverse routing policies require PB, CGPB, or SB")
    for path in (args.collector_config, args.node_pinning):
        if path and not path.is_file():
            p.error(f"file does not exist: {path}")
    output = (args.output or APP / f"build_{name}").resolve()
    if output.exists():
        p.error(f"output already exists: {output}; use another --name or --output")
    required = ["go", "goimports", "protoc", "kompose"]
    if not args.skip_build or args.build_collector:
        required.append("docker")
    if args.apply or args.one_per_node:
        required.append("kubectl")
    for command in required:
        if shutil.which(command) is None:
            p.error(f"missing {command}; run utils/setup_environment.sh")
    import yaml  # Checked before generation; the shell wrapper activates the venv.
    if not args.skip_build or args.build_collector:
        run(["docker", "info"], stdout=subprocess.DEVNULL)
    if args.build_collector:
        collector_env = dict(os.environ, GOTOOLCHAIN=args.collector_toolchain)
        run([args.collector_src.resolve() / "build-and-push.sh", args.registry],
            cwd=args.collector_src, env=collector_env)
    suffix = "hotel_" + args.spec.removeprefix("docker_") + args.extra
    variant = suffix.replace("_", "-")
    build_env = dict(os.environ, IMAGE_TAG=args.image_tag,
                     BLUEPRINT_GOGC=GC_MODES[args.gc]["GOGC"],
                     BLUEPRINT_GC_INTERVAL_SEC=GC_MODES[args.gc]["GC_INTERVAL_SEC"])
    command = ["go", "run", "./examples/dsb_hotel/wiring", "-w", args.spec,
               "-extra", args.extra, "-rpc-timeout", args.rpc_timeout,
               "-collector-image", args.collector_image or args.registry + "/otelcontribcol:latest",
               "-o", output]
    if args.collector_config:
        command += ["-collector-config", args.collector_config.resolve()]
    run(command, cwd=REPO, env=build_env)
    docker = output / "docker"
    run(["goimports", "-w", docker])
    configure_collector(docker / f"otelcol_{suffix}_ctr/config.yaml", args)
    compose_path = docker / "docker-compose.yml"
    compose = configure_compose(compose_path, suffix, args)
    shutil.copyfile(output / ".local.env", docker / ".env")
    for line in (docker / ".env").read_text().splitlines():
        if line.strip() and not line.lstrip().startswith("#"):
            key, value = line.split("=", 1)
            build_env[key] = value
    daemon = f"otelcol-{variant}-ctr"
    no_local = []
    if not (args.frontend_deploy or args.one_per_node):
        frontend = f"frontend-service-{variant}-ctr"
        daemon += "," + frontend
        no_local = ["--no-local-policy", frontend]
    manifests = output / "k8s"
    # Use the shared social-network conversion; build images below with checked
    # subprocesses because d2k8s's legacy build loop continues after failures.
    run([sys.executable, REPO / "d2k8s/d2k8s.py", "--registry", args.registry,
         "--skip-build", "--daemon-services", daemon, *no_local, compose_path, manifests], env=build_env)
    prepare_manifests(manifests, suffix, args)
    pinning = one_per_node(manifests, suffix, args) if args.one_per_node else args.node_pinning
    if pinning:
        command = [sys.executable, REPO / "utils/pin_nodes.py", pinning, manifests]
        if args.no_pin_requests:
            command.append("--no-requests")
        run(command)
    if not args.skip_build:
        build_images(compose, docker, manifests)
    (output / "hotel-build.json").write_text(json.dumps({
        "spec": args.spec, "variant": variant, "namespace": args.namespace,
        "image_tag": args.image_tag, "images_built": not args.skip_build,
        "frontend_service": f"frontend-service-{variant}-ctr",
    }, indent=2) + "\n")
    print(f"Build ready: {output}", flush=True)
    if args.apply:
        apply(manifests, args)
    print(f"Frontend: kubectl -n {args.namespace} port-forward service/frontend-service-{variant}-ctr 9000:2000")
    print(f"Jaeger:   kubectl -n {args.namespace} port-forward service/jaeger-{variant}-ctr 16686:16686")
    if args.nodeport is not None and args.apply:
        run(["kubectl", "-n", args.namespace, "get", "service", f"frontend-service-{variant}-ctr"])


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(f"ERROR: {error}")
