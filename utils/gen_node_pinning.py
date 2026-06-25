#!/usr/bin/env python3
"""
Generate a node-pinning YAML for a DSB-SN variant — the proven rev2 asymmetric-
pressure placement: composepost + hometimeline + hometimeline-cache co-located on
the HOT node (so that node's otelcol absorbs the pressure), everything else spread,
jaeger+ES isolated on their own node. The placement is variant-INDEPENDENT (verified
identical across pb/cgpb/rc rev2), so this just stamps <base>-<variant>-ctr names
onto the canonical layout. Output is consumed by pin_nodes.py.

Usage:
  gen_node_pinning.py <variant> [outfile] [--no-requests]
    variant      : kompose suffix in DASH form, e.g. pb-estest  (deployment names
                   are <base>-<variant>-ctr)
    outfile      : path to write (default: stdout)
    --no-requests: placement-only — emit nodeSelector placement with NO requests_cpu
                   (avoids over-reserving cores; the scheduler just co-locates)

CLUSTER NOTE: the hot node is node-1, which on the bridges-tb cluster is an
UNTAINTED control-plane node (node-0 is the tainted/off-limits one). That matches
the proven rev2 layout. If you want app load entirely off the control plane, edit
the emitted file to move node-1's services to a pure worker. DaemonSets (otelcol,
wrk2api) are intentionally NOT pinned.
"""
import sys

# Canonical rev2 placement: node -> [(service_base, requests_cpu_millicores), ...]
# (extracted from node-pinning-{pb,cgpb,rc}_esrev2.yaml, which are identical mod suffix)
PLACEMENT = [
    ("node-1", [("composepost-service", 12000), ("hometimeline-cache", 2000), ("hometimeline-service", 5000)]),
    ("node-2", [("social-cache", 4000), ("social-db", 25000), ("socialgraph-service", 6000)]),
    ("node-3", [("uniqueid-service", 3000), ("usermention-service", 4000)]),
    ("node-4", [("usertimeline-cache", 2000), ("usertimeline-db", 11000), ("usertimeline-service", 8000)]),
    ("node-5", [("media-service", 2000), ("text-service", 8000), ("userid-service", 6000)]),
    ("node-6", [("post-cache", 2000), ("post-db", 3000), ("post-storage-service", 5000)]),
    ("node-7", [("urlshorten-db", 2000), ("urlshorten-service", 5000)]),
    ("node-8", [("user-service", 3000), ("user-cache", 2000), ("user-db", 2000)]),
    ("node-9", [("jaeger", 20000), ("elasticsearch", 4000)]),
]


def render(variant, no_requests=False):
    n_svc = sum(len(s) for _, s in PLACEMENT)
    mode = "PLACEMENT-ONLY (no requests)" if no_requests else "with CPU requests"
    lines = [
        f"# Auto-generated node-pinning for variant '{variant}' (canonical rev2 placement, {mode}).",
        "# Hot node = node-1: composepost+hometimeline+hometimeline-cache co-located",
        "# for the asymmetric-pressure design. DaemonSets (otelcol, wrk2api) NOT pinned.",
        f"# {n_svc} services across {len(PLACEMENT)} nodes. Service names = <base>-{variant}-ctr.",
        "# Consumed by pin_nodes.py. Edit freely.",
        "",
    ]
    for node, svcs in PLACEMENT:
        lines.append(f"{node}:")
        for base, cpu in svcs:
            lines.append(f"  - {base}-{variant}-ctr:")
            if not no_requests:          # placement-only mode: nodeSelector only, no resource requests
                lines.append(f"      requests_cpu: {cpu}")
        lines.append("")
    return "\n".join(lines)


def main():
    argv = sys.argv[1:]
    no_requests = "--no-requests" in argv          # placement-only: nodeSelector, no CPU requests
    pos = [a for a in argv if a != "--no-requests"]
    if not pos:
        sys.exit("usage: gen_node_pinning.py <variant-dash-form> [outfile] [--no-requests]")
    variant = pos[0]
    out = render(variant, no_requests)
    if len(pos) >= 2:
        with open(pos[1], "w") as f:
            f.write(out)
        n_svc = sum(len(s) for _, s in PLACEMENT)
        tag = " (placement-only, no requests)" if no_requests else ""
        print(f"wrote {pos[1]} ({n_svc} services across {len(PLACEMENT)} nodes){tag}")
    else:
        sys.stdout.write(out)


if __name__ == "__main__":
    main()
