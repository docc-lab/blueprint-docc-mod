#!/usr/bin/env python3
"""Generate the dsb-sn social network OpenShift manifest from a Blueprint
docker-compose.yml.

Usage:
    python3 gen_app_manifest.py docker-compose.yml [-o dsb-sn-notracing.yaml]
                                [--tag notracing] [--user docker.io/maxlanliu]

Traced build (docker_tracing spec):
    python3 gen_app_manifest.py docker-compose.yml -o dsb-sn-tracing.yaml \
        --tag tracing --trace-endpoint otel-local:4317

With --trace-endpoint, the trace collector container from the compose file
(the jaeger/zipkin image) is NOT deployed; every dial address that pointed at
it points at --trace-endpoint instead (the otel-local collector from
otel-collectors.yaml).

Services with a `build:` section become your pushed images
(<user>/dsbsn-<name>:<tag>); services with `image: mongo` / `image: memcached`
use pinned official images. Ports come from each service's `expose:` entry,
environment variables are copied verbatim with hostnames converted to
Kubernetes names (underscores -> hyphens). The project setup and RAPL
collectors live in dsb-sn-rapl.yaml and are not touched by this file.
"""
import argparse
import yaml

NAMESPACE = "dsb-sn"
NODE = "dell66"
PART_OF = "dsb-sn"
FRONTEND = "wrk2api_container"
NODEPORT = 30853
# MongoDB indexes created automatically when each database pod starts empty.
# Keyed by docker-compose service name -> (database, collection, [fields]).
# Field names match the stored documents (all lowercase). Non-unique on purpose,
# so the application's own duplicate handling behaves exactly as before.
MONGO_INDEXES = {
    "user_db_ctr":         ("user",         "user",         ["userid", "username"]),
    "social_db_ctr":       ("social-graph", "social-graph", ["userid"]),
    "usertimeline_db_ctr": ("usertimeline", "usertimeline", ["userid"]),
    "post_db_ctr":         ("post",         "post",         ["postid"]),
}

# Extra environment variables for specific services (docker-compose name).
# GOGC=400: the home timeline service decodes and re-encodes a 100-entry JSON
# list ~35,000 times per second at 1,600 posts/s; collecting garbage 4x less
# often cut its CPU by roughly 15-30% and ComposePost latency by 10-20%.
EXTRA_ENV = {
    "hometimeline_container": [{"name": "GOGC", "value": "400"}],
}

BACKEND_IMAGES = {
    "mongo": "docker.io/library/mongo:7",
    "memcached": "docker.io/library/memcached:1.6",
}


def k8s(name):
    return name.replace("_", "-")


def fix_env(entry):
    key, val = entry.split("=", 1)
    if ":" in val:
        host, port = val.rsplit(":", 1)
        if host != "0.0.0.0":
            host = k8s(host)  # Kubernetes service names can't contain underscores
        val = f"{host}:{port}"
    return {"name": key, "value": val}


def labels(name):
    return {"app": name, "app.kubernetes.io/part-of": PART_OF}


def deployment(name, image, port, env, pull, volumes=None, mounts=None):
    container = {"name": name, "image": image, "imagePullPolicy": pull,
                 "ports": [{"containerPort": port}]}
    if env:
        container["env"] = env
    if mounts:
        container["volumeMounts"] = mounts
    pod_spec = {"nodeSelector": {"kubernetes.io/hostname": NODE}, "containers": [container]}
    if volumes:
        pod_spec["volumes"] = volumes
    return {
        "apiVersion": "apps/v1", "kind": "Deployment",
        "metadata": {"name": name, "namespace": NAMESPACE, "labels": labels(name)},
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {"app": name}},
            "template": {
                "metadata": {"labels": labels(name)},
                "spec": pod_spec,
            },
        },
    }


def index_configmap(name, dbname, coll, fields):
    lines = [
        "// Runs once, when this MongoDB pod starts with an empty data directory",
        "// (every fresh pod here). Created by gen_app_manifest.py.",
        f"const target = db.getSiblingDB({dbname!r});",
    ]
    for f in fields:
        lines.append(f"target.getCollection({coll!r}).createIndex({{ {f}: 1 }}, {{ name: {(f + '_1')!r} }});")
    lines.append(f"print('dsb-sn: indexes on {dbname}.{coll}: ' + JSON.stringify(target.getCollection({coll!r}).getIndexes().map(i => i.key)));")
    return {"apiVersion": "v1", "kind": "ConfigMap",
            "metadata": {"name": f"{name}-init", "namespace": NAMESPACE, "labels": labels(name)},
            "data": {"create-indexes.js": "\n".join(lines) + "\n"}}


def service(name, port, node_port=None):
    p = {"name": "http" if node_port else f"port-{port}", "port": port, "targetPort": port}
    spec = {"selector": {"app": name}, "ports": [p]}
    if node_port:
        p["nodePort"] = node_port
        spec = {"type": "NodePort", **spec}
    return {"apiVersion": "v1", "kind": "Service",
            "metadata": {"name": name, "namespace": NAMESPACE, "labels": labels(name)},
            "spec": spec}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("compose")
    ap.add_argument("-o", "--output", default="dsb-sn-notracing.yaml")
    ap.add_argument("--tag", default="notracing")
    ap.add_argument("--user", default="docker.io/maxlanliu")
    ap.add_argument("--trace-endpoint", default=None,
                    help="host:port of the OpenTelemetry collector the services export spans to")
    args = ap.parse_args()

    services = yaml.safe_load(open(args.compose))["services"]
    # trace collector containers in the compose file (replaced by --trace-endpoint)
    collectors = {k8s(c) for c, svc in services.items()
                  if any(t in str(svc.get("image", "")) for t in ("jaeger", "zipkin", "opentelemetry"))}
    if collectors and not args.trace_endpoint:
        raise SystemExit(f"compose file has trace collector(s) {sorted(collectors)}: pass --trace-endpoint otel-local:4317")
    apps, backends = [], []
    for cname in sorted(services):
        svc = services[cname]
        if k8s(cname) in collectors:
            continue                      # not deployed: spans go to --trace-endpoint
        port = int(svc["expose"][0])
        env = [fix_env(e) for e in svc.get("environment", [])] + EXTRA_ENV.get(cname, [])
        for e in env:
            if e["value"].rsplit(":", 1)[0] in collectors:
                e["value"] = args.trace_endpoint
        name = k8s(cname)
        if "build" in svc:
            short = k8s(cname.removesuffix("_container"))
            image = f"{args.user}/dsbsn-{short}:{args.tag}"
            apps.append((name, image, port, env, "Always"))
        else:
            base = svc["image"].split(":")[0].split("/")[-1]
            backends.append((name, BACKEND_IMAGES.get(base, svc["image"]), port, env, "IfNotPresent", cname))

    docs = []
    for name, image, port, env, pull in apps:
        docs.append(deployment(name, image, port, env, pull))
        docs.append(service(name, port, NODEPORT if name == k8s(FRONTEND) else None))
    for name, image, port, env, pull, cname in backends:
        volumes = mounts = None
        if cname in MONGO_INDEXES:
            dbname, coll, fields = MONGO_INDEXES[cname]
            docs.append(index_configmap(name, dbname, coll, fields))
            volumes = [{"name": "init", "configMap": {"name": f"{name}-init"}}]
            mounts = [{"name": "init", "mountPath": "/docker-entrypoint-initdb.d", "readOnly": True}]
        docs.append(deployment(name, image, port, env, pull, volumes, mounts))
        docs.append(service(name, port))
    fe = k8s(FRONTEND)
    docs.append({"apiVersion": "route.openshift.io/v1", "kind": "Route",
                 "metadata": {"name": fe, "namespace": NAMESPACE, "labels": labels(fe)},
                 "spec": {"to": {"kind": "Service", "name": fe}, "port": {"targetPort": "http"}}})

    # sanity checks: every dial address must point at a Service in this file
    names = {d["metadata"]["name"] for d in docs if d["kind"] == "Service"}
    ports = {d["metadata"]["name"]: d["spec"]["ports"][0]["port"] for d in docs if d["kind"] == "Service"}
    for d in docs:
        if d["kind"] != "Deployment":
            continue
        for e in d["spec"]["template"]["spec"]["containers"][0].get("env", []):
            if e["name"].endswith("_DIAL_ADDR") and e["value"] != args.trace_endpoint:
                host, port = e["value"].rsplit(":", 1)
                assert host in names, f"{d['metadata']['name']}: {e} points at unknown service"
                assert int(port) == ports[host], f"{d['metadata']['name']}: {e} port mismatch"

    transports = sorted({e["name"].split("_")[-3] for d in docs if d["kind"] == "Deployment"
                         for e in d["spec"]["template"]["spec"]["containers"][0].get("env", [])
                         if e["name"].endswith("_DIAL_ADDR") and "_SERVICE_" in e["name"]})
    TRACE_NOTE = "" if not collectors else (
        f"\n#   - TRACING: every service is instrumented with OpenTelemetry (docker_tracing\n"
        f"#     spec) and exports spans to {args.trace_endpoint}; the compose file's\n"
        f"#     collector container ({', '.join(sorted(collectors))}) is not deployed. Requires\n"
        f"#     otel-collectors.yaml (otel-local on dell66, otel-central on dell65).\n"
        f"#     SDK settings can be set per Deployment with OTEL_* env vars (see trace.go).")
    header = f"""# =============================================================================
# DeathStarBench Social Network (Blueprint), {"/".join(transports)} between services,
# {"WITH tracing (spans to " + args.trace_endpoint + ")" if collectors else "WITHOUT tracing"}, all pods on {NODE}
#
# Generated by gen_app_manifest.py from Blueprint's build/docker/docker-compose.yml.
# Requires dsb-sn-rapl.yaml to be applied first (creates the namespace and the
# anyuid permission that mongo/memcached need).
#
#   - application images: {args.user}/dsbsn-<service>:{args.tag}, imagePullPolicy: Always
#     Built from patched Blueprint code (see the FIX comments in the source):
#       * gRPC between services instead of Thrift (Blueprint's Thrift client
#         is not safe for concurrent requests)
#       * UserTimelineService: cache stores []PostInfo, not []int64 (FIX 1-3)
#       * UniqueIdService: locked counter, custom epoch, wider counter
#       * UserTimelineService: per-user lock in WriteUserTimeline (FIX 4)
#       * HomeTimelineService: per-follower lock in WriteHomeTimeline (FIX 5)
#       * Home and user timelines capped at the newest 100 entries (FIX 6),
#         so the cost of a post does not grow with every earlier post
#       * HomeTimelineService: all followers' timelines updated in parallel
#         (FIX 7), so a post waits for the slowest update, not their sum
#     FIX 4 and FIX 5 assume ONE replica of those services (as deployed here).
#   - hometimeline-container runs with GOGC=400 (less garbage collection;
#     see EXTRA_ENV in gen_app_manifest.py)
#   - backends: 5x mongo:7, 5x memcached:1.6 (official images, no push needed)
#   - MongoDB indexes (not in stock Blueprint; the original DeathStarBench has
#     them): each database pod creates its own on first start, from the
#     *-init ConfigMap mounted at /docker-entrypoint-initdb.d:
#       user.user (userid, username), social-graph.social-graph (userid),
#       usertimeline.usertimeline (userid), post.post (postid)
#   - every pod has nodeSelector kubernetes.io/hostname: {NODE}
#   - ports, env vars and dial addresses copied from docker-compose.yml, with
#     hostnames converted to Kubernetes names (underscores -> hyphens)
#   - {fe} (the HTTP frontend) is a NodePort fixed at {NODEPORT},
#     plus a Route; all other Services are ClusterIP
#   - every object has the label app.kubernetes.io/part-of={PART_OF}{TRACE_NOTE}
#
# Deploy:   oc apply  -f {args.output}
# Remove:   oc delete -f {args.output}      (rapl keeps running)
# Frontend: http://<dell66-ip>:{NODEPORT}   or   oc get route -n {NAMESPACE}
# =============================================================================
"""
    class Dumper(yaml.SafeDumper):
        pass

    def str_repr(dumper, data):
        style = "|" if "\n" in data else None
        return dumper.represent_scalar("tag:yaml.org,2002:str", data, style=style)
    Dumper.add_representer(str, str_repr)

    with open(args.output, "w") as f:
        f.write(header)
        f.write(yaml.dump_all(docs, Dumper=Dumper, sort_keys=False, default_flow_style=False, width=1000))
    kinds = {}
    for d in docs:
        kinds[d["kind"]] = kinds.get(d["kind"], 0) + 1
    print(f"{args.output}: {kinds}, transport between services: {transports}")


if __name__ == "__main__":
    main()
