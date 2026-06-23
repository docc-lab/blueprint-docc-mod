#!/usr/bin/env python3
"""
Inject the manual post-d2k8s edits into generated DSB-SN k8s manifests:
  - Jaeger/ES performance env vars (the ES_BULK_* + collector queue/worker knobs)
  - otelcol env vars (GOMEMLIMIT, GC_INTERVAL_SEC) + cpu/mem resources
  - (optional) app-pod GC mode (natural vs forced) on Go services

These are NOT produced by kompose/d2k8s and are wiped on every wiring+d2k8s
regeneration, so build_deploy_dsb.sh re-applies them automatically.

Usage:
  inject_perf_env.py <k8s_dir> <variant> [--gc natural|forced]
    e.g. inject_perf_env.py examples/dsb_sn/build_pb_esrev2/k8s pb-esrev2 --gc natural
"""
import sys, os, glob

try:
    import yaml
except ImportError:
    sys.exit("ERROR: PyYAML required -> pip install pyyaml")

# Values per utils/ES_BACKED_JAEGER_TUNING.md and the reference build.
JAEGER_ENV = {
    "COLLECTOR_OTLP_GRPC_MAX_MESSAGE_SIZE": "33554432",  # 32 MiB
    "ES_BULK_WORKERS":        "10",                       # dominant lever (jaeger default 1)
    "ES_BULK_SIZE":           "10000000",                 # 10 MB
    "ES_BULK_ACTIONS":        "5000",
    "ES_BULK_FLUSH_INTERVAL": "200ms",
    "COLLECTOR_QUEUE_SIZE":   "10000",
    "COLLECTOR_NUM_WORKERS":  "100",
}
OTELCOL_ENV = {
    "GOMEMLIMIT":      "3276MiB",
    "GC_INTERVAL_SEC": "0",
}
OTELCOL_RES = {
    "limits":   {"cpu": "500m", "memory": "4Gi"},
    "requests": {"cpu": "500m", "memory": "4Gi"},
}
GC_MODES = {
    # GOGC=off + interval=0 would mean NO GC at all (node OOM) -- never emit that combo.
    "natural": {"GOGC": "100", "GC_INTERVAL_SEC": "0"},
    "forced":  {"GOGC": "off", "GC_INTERVAL_SEC": "0.1"},
}


def load_all(p):
    with open(p) as f:
        return list(yaml.safe_load_all(f))


def dump_all(p, docs):
    with open(p, "w") as f:
        yaml.safe_dump_all(docs, f, default_flow_style=False, sort_keys=False)


def containers(doc):
    try:
        return doc["spec"]["template"]["spec"]["containers"]
    except (KeyError, TypeError):
        return []


def merge_env(c, env_map, only_if_present=False):
    """Merge env_map into container c. If only_if_present, skip containers that
    don't already declare at least one of the keys (used for GC: targets the Go
    services Blueprint set GOGC/GC_INTERVAL_SEC on, not mongo/redis)."""
    env = c.get("env") or []
    have = {e["name"]: e for e in env if isinstance(e, dict) and "name" in e}
    if only_if_present and not (set(env_map) & set(have)):
        return False
    for k, v in env_map.items():
        if k in have:
            have[k]["value"] = str(v)
            have[k].pop("valueFrom", None)
        else:
            env.append({"name": k, "value": str(v)})
    c["env"] = env
    return True


def patch(path, env_map, res=None, only_if_present=False):
    docs = load_all(path)
    touched = False
    for doc in docs:
        if not isinstance(doc, dict):
            continue
        for c in containers(doc):
            if merge_env(c, env_map, only_if_present):
                if res is not None:
                    c["resources"] = res
                touched = True
    if touched:
        dump_all(path, docs)
    return touched


def find_workload(k8s, name):
    for p in glob.glob(os.path.join(k8s, "*.yaml")):
        b = os.path.basename(p)
        if name in b and ("deployment" in b or "daemonset" in b):
            return p
    return None


def app_deployments(k8s):
    """Go app-service deployments (exclude jaeger/elasticsearch/otelcol/db/cache infra)."""
    out = []
    for p in glob.glob(os.path.join(k8s, "*-deployment.yaml")):
        b = os.path.basename(p)
        if any(x in b for x in ("jaeger", "elasticsearch", "otelcol", "-db-", "-cache-")):
            continue
        out.append(p)
    return out


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    k8s, variant = sys.argv[1], sys.argv[2]
    gc = None
    if "--gc" in sys.argv:
        gc = sys.argv[sys.argv.index("--gc") + 1]
        if gc not in GC_MODES:
            sys.exit(f"--gc must be one of {list(GC_MODES)}")

    j = find_workload(k8s, f"jaeger-{variant}-ctr")
    print(f"jaeger : {'patched' if (j and patch(j, JAEGER_ENV)) else 'NOT FOUND'} {j or ''}")

    o = find_workload(k8s, f"otelcol-{variant}-ctr")
    print(f"otelcol: {'patched' if (o and patch(o, OTELCOL_ENV, OTELCOL_RES)) else 'NOT FOUND'} {o or ''}")

    if gc:
        n = sum(1 for p in app_deployments(k8s) if patch(p, GC_MODES[gc], only_if_present=True))
        print(f"gc[{gc}]: patched {n} app-service deployments")


if __name__ == "__main__":
    main()
