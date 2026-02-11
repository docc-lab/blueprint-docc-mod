#!/usr/bin/env python3
"""
Pin Kubernetes deployments to nodes and set resource requests/limits
according to a node-pinning YAML config.

Usage:
  python pin_nodes.py <pinning.yaml> <k8s-dir> [--dry-run]

Requires: PyYAML
"""

import argparse
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("Error: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr)
    sys.exit(1)


def parse_pinning_yaml(path):
    """
    Parse node-pinning YAML into a map: service_name -> { node, requests_cpu?, limits_cpu?, requests_memory?, limits_memory? }.
    Node names are top-level keys; each value is a list of { service_name: { requests_cpu?, limits_cpu?, requests_memory?, limits_memory? } }.
    Resources may be omitted (or only comments); then the corresponding keys are None.
    Memory values are kept as strings (e.g. "111Mi"); if an int is given, converted to "<n>Mi".
    """
    with open(path, "r") as f:
        data = yaml.safe_load(f)
    if not data or not isinstance(data, dict):
        return {}
    result = {}
    for node_name, entries in data.items():
        if node_name.startswith("#") or not isinstance(entries, list):
            continue
        for item in entries:
            if not isinstance(item, dict) or len(item) != 1:
                continue
            service_name, resources = next(iter(item.items()))
            if resources is not None and not isinstance(resources, dict):
                continue
            resources = resources or {}
            requests_cpu = resources.get("requests_cpu")
            limits_cpu = resources.get("limits_cpu")
            requests_memory = resources.get("requests_memory")
            limits_memory = resources.get("limits_memory")
            if requests_memory is not None and isinstance(requests_memory, int):
                requests_memory = f"{requests_memory}Mi"
            if limits_memory is not None and isinstance(limits_memory, int):
                limits_memory = f"{limits_memory}Mi"
            result[service_name] = {
                "node": node_name,
                "requests_cpu": int(requests_cpu) if requests_cpu is not None else None,
                "limits_cpu": int(limits_cpu) if limits_cpu is not None else None,
                "requests_memory": requests_memory,
                "limits_memory": limits_memory,
            }
    return result


def cpu_to_k8s(millicores):
    """Convert integer millicores to Kubernetes CPU string (e.g. 7000 -> '7000m')."""
    if millicores is None:
        return None
    return f"{int(millicores)}m"


def apply_pinning_to_deployment(deployment_path, pin_map, dry_run=False):
    """
    If deployment metadata.name is in pin_map, set nodeSelector and container resources.
    When requests_cpu/limits_cpu/requests_memory/limits_memory are not in the pin config, strip them from the deployment.
    Returns True if the file was modified (or would be in dry_run).
    """
    with open(deployment_path, "r") as f:
        doc = yaml.safe_load(f)
    if doc.get("kind") != "Deployment":
        return False
    name = doc.get("metadata", {}).get("name")
    if not name or name not in pin_map:
        return False
    pin = pin_map[name]
    node = pin["node"]
    requests_cpu = cpu_to_k8s(pin.get("requests_cpu"))
    limits_cpu = cpu_to_k8s(pin.get("limits_cpu"))
    requests_memory = pin.get("requests_memory")
    limits_memory = pin.get("limits_memory")

    spec = doc.setdefault("spec", {})
    template = spec.setdefault("template", {})
    pod_spec = template.setdefault("spec", {})

    # Node selector: pin to this node
    pod_spec["nodeSelector"] = {"kubernetes.io/hostname": node}

    # Build requests/limits dicts from pin (set or strip per key)
    requests = {}
    if requests_cpu is not None:
        requests["cpu"] = requests_cpu
    if requests_memory is not None:
        requests["memory"] = requests_memory
    limits = {}
    if limits_cpu is not None:
        limits["cpu"] = limits_cpu
    if limits_memory is not None:
        limits["memory"] = limits_memory

    containers = pod_spec.get("containers", [])
    if not containers:
        return False
    for container in containers:
        resources = container.setdefault("resources", {})
        if requests:
            resources["requests"] = dict(requests)
        else:
            resources.pop("requests", None)
        if limits:
            resources["limits"] = dict(limits)
        else:
            resources.pop("limits", None)
        if not resources:
            container.pop("resources", None)

    res_msg = ", ".join(
        x for x in [
            f"requests_cpu={requests_cpu}" if requests_cpu else None,
            f"limits_cpu={limits_cpu}" if limits_cpu else None,
            f"requests_memory={requests_memory}" if requests_memory else None,
            f"limits_memory={limits_memory}" if limits_memory else None,
        ] if x
    ) or "no resources"
    if dry_run:
        print(f"[dry-run] Would update {deployment_path.name}: node={node}, {res_msg}")
        return True

    with open(deployment_path, "w") as f:
        yaml.dump(doc, f, default_flow_style=False, sort_keys=False)
    print(f"Updated {deployment_path.name}: node={node}, {res_msg}")
    return True


def main():
    parser = argparse.ArgumentParser(
        description="Pin Kubernetes deployments to nodes and set resources from a node-pinning YAML."
    )
    parser.add_argument(
        "pinning_yaml",
        help="Path to node-pinning YAML (node -> list of service: { requests_cpu?, limits_cpu?, requests_memory?, limits_memory? })",
    )
    parser.add_argument(
        "k8s_dir",
        help="Directory containing *-deployment.yaml files",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would be changed without writing files",
    )
    args = parser.parse_args()

    pin_path = Path(args.pinning_yaml)
    k8s_path = Path(args.k8s_dir)
    if not pin_path.exists():
        print(f"Error: Pinning file not found: {pin_path}", file=sys.stderr)
        sys.exit(1)
    if not k8s_path.is_dir():
        print(f"Error: K8s directory not found or not a directory: {k8s_path}", file=sys.stderr)
        sys.exit(1)

    pin_map = parse_pinning_yaml(pin_path)
    if not pin_map:
        print("Warning: No service entries found in pinning YAML.", file=sys.stderr)

    deployment_files = sorted(k8s_path.glob("*-deployment.yaml"))
    if not deployment_files:
        print(f"Warning: No *-deployment.yaml files in {k8s_path}", file=sys.stderr)

    updated = 0
    for dep_path in deployment_files:
        if apply_pinning_to_deployment(dep_path, pin_map, dry_run=args.dry_run):
            updated += 1

    if args.dry_run:
        print(f"[dry-run] Would update {updated} deployment(s).")
    else:
        print(f"Updated {updated} deployment(s).")


if __name__ == "__main__":
    main()
