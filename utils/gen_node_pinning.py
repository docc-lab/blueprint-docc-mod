#!/usr/bin/env python3
"""
Generate a node-pinning YAML for a DSB-SN variant — the proven rev2 asymmetric-
pressure placement: composepost + hometimeline + hometimeline-cache co-located on
the HOT node (so that node's otelcol absorbs the pressure), everything else spread,
jaeger+ES isolated on their own node. The placement is variant-INDEPENDENT (verified
identical across pb/cgpb/rc rev2), so this just stamps <base>-<variant>-ctr names
onto the canonical layout. Output is consumed by pin_nodes.py.

Usage:
  gen_node_pinning.py <variant> [outfile] [--no-requests] [--anti-affinity | --one-per-node]
    variant      : kompose suffix in DASH form, e.g. pb-estest  (deployment names
                   are <base>-<variant>-ctr)
    outfile      : path to write (default: stdout)
    --no-requests: placement-only — emit nodeSelector placement with NO requests_cpu
                   (avoids over-reserving cores; the scheduler just co-locates)
    --anti-affinity: no two traced services with a call edge share a node (static
                   9-node layout; wrk2api pinned — build with --wrk2api-deploy)
    --one-per-node: every named (traced) service gets its OWN node. Nodes are
                   AUTO-DETECTED from `kubectl get nodes` (schedulable only, i.e.
                   tainted control-plane excluded) — no assumption about cluster
                   size. Needs >= 14 schedulable nodes (12 backends + wrk2api +
                   jaeger/ES). Caches/DBs ride with their owning service; the two
                   near-idle services (userid, tracepressure) share the untainted
                   control-plane node to keep real load off etcd.

CLUSTER NOTE: the hot node is node-1, which on the bridges-tb cluster is an
UNTAINTED control-plane node (node-0 is the tainted/off-limits one). That matches
the proven rev2 layout. If you want app load entirely off the control plane, edit
the emitted file to move node-1's services to a pure worker. DaemonSets (otelcol,
wrk2api) are intentionally NOT pinned.
"""
import json
import re
import subprocess
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
# Sizing (req == lim, emitted by render): every *-service = 8 cores; caches = 4;
# light DBs (post/user/urlshorten) = 8; the two heavy DBs (usertimeline-db,
# social-db) = 16 (2x a service). Tightest nodes are node-3 & node-4 (2 services
# + cache + heavy DB = 36 cores); with the otelcol DaemonSet capped at 1 core
# (~37 total) they fit the 40-core nodes. jaeger/ES isolated on node-9.
ANTI_AFFINITY_PLACEMENT = [
    ("node-1", [("composepost-service", 8000), ("userid-service", 8000)]),
    ("node-2", [("hometimeline-service", 8000), ("hometimeline-cache", 4000),
                ("urlshorten-service", 8000), ("urlshorten-db", 8000)]),
    ("node-3", [("usertimeline-service", 8000), ("usertimeline-cache", 4000),
                ("usertimeline-db", 16000), ("usermention-service", 8000)]),
    ("node-4", [("socialgraph-service", 8000), ("social-cache", 4000),
                ("social-db", 16000), ("text-service", 8000)]),
    ("node-5", [("post-storage-service", 8000), ("post-cache", 4000), ("post-db", 8000)]),
    ("node-6", [("uniqueid-service", 8000), ("media-service", 8000)]),
    ("node-7", [("wrk2api-service", 8000)]),
    ("node-8", [("user-service", 8000), ("user-cache", 4000), ("user-db", 8000)]),
    ("node-9", [("jaeger", 24000), ("elasticsearch", 8000)]),
]

# --- one-per-node mode (auto-detected cluster size) --------------------------
# Backends heaviest-first (measured cores @3600, cpd2 n=5), each with its
# untraced cache/db companions, which ride along on the same node.
ONE_PER_NODE_BACKENDS = [
    ("usertimeline-service", 8000, ["usertimeline-cache", "usertimeline-db"]),
    ("composepost-service", 12000, []),
    ("text-service", 8000, []),
    ("hometimeline-service", 5000, ["hometimeline-cache"]),
    ("post-storage-service", 5000, ["post-cache", "post-db"]),
    ("urlshorten-service", 5000, ["urlshorten-db"]),
    ("socialgraph-service", 6000, ["social-cache", "social-db"]),
    ("usermention-service", 4000, []),
    ("uniqueid-service", 3000, []),
    ("media-service", 2000, []),
    ("user-service", 3000, ["user-cache", "user-db"]),
]
# near-idle traced services parked on the untainted control-plane node so no
# real load contends with etcd there
ONE_PER_NODE_CP = [("userid-service", 1000), ("tracepressure-service", 500)]


def schedulable_nodes():
    """Node names from `kubectl get nodes`, excluding NoSchedule-tainted ones
    (the locked control-plane). Sorted numerically (node-2 < node-10). Returns
    None if kubectl is unavailable/unreachable."""
    try:
        out = subprocess.run(["kubectl", "get", "nodes", "-o", "json"],
                             capture_output=True, text=True, check=True, timeout=15).stdout
        items = json.loads(out)["items"]
    except Exception:
        return None
    nodes = []
    for it in items:
        taints = (it.get("spec") or {}).get("taints") or []
        if any(t.get("effect") == "NoSchedule" for t in taints):
            continue
        nodes.append(it["metadata"]["name"])

    def key(n):
        m = re.search(r"(\d+)$", n)
        return (0, int(m.group(1))) if m else (1, n)
    return sorted(nodes, key=key)


def build_one_per_node():
    """Placement: every named service on its own auto-detected node.
    Layout: first schedulable node (the untainted CP) gets only the near-idle
    pair; last node = jaeger+ES; second-to-last = wrk2api; the 11 in between
    (heaviest-first onto the earliest workers) get one backend each."""
    nodes = schedulable_nodes()
    if nodes is None:
        sys.exit("ERROR: --one-per-node needs a reachable kubectl (nodes are auto-detected)")
    need = 1 + len(ONE_PER_NODE_BACKENDS) + 2   # cp + 11 backends + wrk2api + jaeger
    if len(nodes) < need:
        sys.exit(f"ERROR: --one-per-node needs >= {need} schedulable nodes, found {len(nodes)}: {nodes}")
    cp, workers = nodes[0], nodes[1:]
    placement = [(cp, [(s, c) for s, c in ONE_PER_NODE_CP])]
    for i, (svc, cpu, stores) in enumerate(ONE_PER_NODE_BACKENDS):
        placement.append((workers[i], [(svc, cpu)] + [(st, 2000) for st in stores]))
    placement.append((workers[len(ONE_PER_NODE_BACKENDS)], [("wrk2api-service", 8000)]))
    placement.append((workers[len(ONE_PER_NODE_BACKENDS) + 1], [("jaeger", 20000), ("elasticsearch", 4000)]))
    return placement


def render(variant, no_requests=False, placement=None, header=None):
    placement = placement or PLACEMENT
    n_svc = sum(len(s) for _, s in placement)
    mode = "PLACEMENT-ONLY (no requests)" if no_requests else "with CPU requests"
    if placement is ANTI_AFFINITY_PLACEMENT:
        note = "# No two traced *-services with a call edge share a node; wrk2api pinned (build --wrk2api-deploy)."
    elif placement is PLACEMENT:
        note = ("# Hot node = node-1: composepost+hometimeline+hometimeline-cache co-located\n"
                "# for the asymmetric-pressure design. DaemonSets (otelcol, wrk2api) NOT pinned.")
    else:
        note = ("# ONE-PER-NODE: nodes auto-detected from kubectl (schedulable only); every named\n"
                "# service isolated; caches/DBs ride with their service; near-idle userid+tracepressure\n"
                "# share the untainted CP node; wrk2api pinned (build --wrk2api-deploy).")
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
                lines.append(f"      limits_cpu: {cpu}")   # req == lim (deterministic hard cap)
        lines.append("")
    return "\n".join(lines)


def main():
    argv = sys.argv[1:]
    no_requests = "--no-requests" in argv          # placement-only: nodeSelector, no CPU requests
    anti = "--anti-affinity" in argv               # traced services with a call edge never co-located
    one_per = "--one-per-node" in argv             # every named service on its own auto-detected node
    pos = [a for a in argv if a not in ("--no-requests", "--anti-affinity", "--one-per-node")]
    if not pos:
        sys.exit("usage: gen_node_pinning.py <variant-dash-form> [outfile] [--no-requests] [--anti-affinity | --one-per-node]")
    if anti and one_per:
        sys.exit("ERROR: --anti-affinity and --one-per-node are mutually exclusive")
    variant = pos[0]
    if one_per:
        placement = build_one_per_node()
        header = "one-per-node placement (auto-detected nodes; every named service isolated; wrk2api pinned as Deployment)"
    elif anti:
        placement = ANTI_AFFINITY_PLACEMENT
        header = "anti-affinity placement (no co-located traced call edges; wrk2api pinned as Deployment)"
    else:
        placement = PLACEMENT
        header = None
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
