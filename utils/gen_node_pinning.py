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

# Anti-affinity placement: NO two TRACED (named *-service) services that have a
# call edge between them share a node, so every traced service<->service hop
# crosses a real network link (un-masking the bridges' propagation/serialization
# overhead). Caches/DBs are UNTRACED -> they ride with their owning service.
# wrk2api (frontend) MUST be a Deployment (not the per-node DaemonSet) for this to
# hold for the gateway->backend hop too -> build with --wrk2api-deploy.
#
# Call graph (from the service constructors' client fields):
#   wrk2api      -> composepost, hometimeline, socialgraph, user, usertimeline
#   composepost  -> hometimeline, media, poststorage, text, uniqueid, user, usertimeline
#   hometimeline -> poststorage, socialgraph ;  text -> urlshorten, usermention
#   socialgraph  -> userid ;  user -> socialgraph ;  uniqueid -> usertimeline ;  usertimeline -> poststorage
# Every co-located *-service pair below is a verified NON-edge.
ANTI_AFFINITY_PLACEMENT = [
    ("node-1", [("composepost-service", 12000), ("userid-service", 6000)]),
    ("node-2", [("hometimeline-service", 5000), ("hometimeline-cache", 2000),
                ("urlshorten-service", 5000), ("urlshorten-db", 2000)]),
    ("node-3", [("usertimeline-service", 8000), ("usertimeline-cache", 2000),
                ("usertimeline-db", 11000), ("usermention-service", 4000)]),
    ("node-4", [("socialgraph-service", 6000), ("social-cache", 4000),
                ("social-db", 25000), ("text-service", 8000)]),
    ("node-5", [("post-storage-service", 5000), ("post-cache", 2000), ("post-db", 3000)]),
    ("node-6", [("uniqueid-service", 3000), ("media-service", 2000)]),
    ("node-7", [("wrk2api-service", 8000)]),
    ("node-8", [("user-service", 3000), ("user-cache", 2000), ("user-db", 2000)]),
    ("node-9", [("jaeger", 20000), ("elasticsearch", 4000)]),
]


def render(variant, no_requests=False, placement=None, header=None):
    placement = placement or PLACEMENT
    n_svc = sum(len(s) for _, s in placement)
    mode = "PLACEMENT-ONLY (no requests)" if no_requests else "with CPU requests"
    is_anti = placement is ANTI_AFFINITY_PLACEMENT
    note = ("# No two traced *-services with a call edge share a node; wrk2api pinned (build --wrk2api-deploy)."
            if is_anti else
            "# Hot node = node-1: composepost+hometimeline+hometimeline-cache co-located\n"
            "# for the asymmetric-pressure design. DaemonSets (otelcol, wrk2api) NOT pinned.")
    lines = [
        f"# Auto-generated node-pinning for variant '{variant}' ({header or 'canonical rev2 placement'}, {mode}).",
        note,
        f"# {n_svc} services across {len(placement)} nodes. Service names = <base>-{variant}-ctr.",
        "# Consumed by pin_nodes.py. Edit freely.",
        "",
    ]
    for node, svcs in placement:
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
    anti = "--anti-affinity" in argv               # traced services with a call edge never co-located
    pos = [a for a in argv if a not in ("--no-requests", "--anti-affinity")]
    if not pos:
        sys.exit("usage: gen_node_pinning.py <variant-dash-form> [outfile] [--no-requests] [--anti-affinity]")
    variant = pos[0]
    placement = ANTI_AFFINITY_PLACEMENT if anti else PLACEMENT
    header = "anti-affinity placement (no co-located traced call edges; wrk2api pinned as Deployment)" if anti else None
    out = render(variant, no_requests, placement=placement, header=header)
    if len(pos) >= 2:
        with open(pos[1], "w") as f:
            f.write(out)
        n_svc = sum(len(s) for _, s in placement)
        tag = " (placement-only, no requests)" if no_requests else ""
        tag += " [anti-affinity]" if anti else ""
        print(f"wrote {pos[1]} ({n_svc} services across {len(placement)} nodes){tag}")
    else:
        sys.stdout.write(out)


if __name__ == "__main__":
    main()
