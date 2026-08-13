#!/usr/bin/env bash
# deploy.sh — build payloadbench (no-tracing variant) and roll it onto the cluster.
#
# Extracted from the inline sequence used by sweep_pool.sh so that a plain
# redeploy does not require running a whole parameter sweep. Every stage is
# checked: a silent build/push failure previously produced a full campaign of
# measurements against a STALE image, which is indistinguishable from a real
# result in the output files.
#
# Usage: ./deploy.sh [build_dir]        (default build_nt_es)
set -euo pipefail
cd "$(dirname "$0")"
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
source ../../.venv/bin/activate
B=${1:-build_nt_es}
REG=${REG:-10.10.1.1:30000}
NODEPORT=${NODEPORT:-11011}   # fixed port every measurement script targets
GOIMPORTS="$(go env GOPATH)/bin/goimports"
[ -x "$GOIMPORTS" ] || { go install golang.org/x/tools/cmd/goimports@latest; }

echo "=== [1] codegen -> $B ==="
rm -rf "$B"
go run ./wiring -w docker_nt_es -o "$B" -quiet
# Blueprint's generated code carries imports the templates cannot know are
# unused; goimports is a REQUIRED stage, not a tidy-up (build fails without it).
"$GOIMPORTS" -w "$B/docker"
cp "$B/.local.env" "$B/docker/.env"

echo "=== [2] build images + push to $REG, emit k8s manifests ==="
( cd "$B" && sg docker -c "export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin; \
    source $PWD/../../../.venv/bin/activate; \
    set -a; . docker/.env; set +a; \
    python3 $PWD/../../../d2k8s/d2k8s.py --registry $REG \
      --daemon-services otelcol-nt-es-ctr docker/docker-compose.yml k8s" ) >/tmp/deploy_d2k8s.log 2>&1
for svc in edge-service internal-service; do
  grep -q "Successfully pushed $REG/$svc" /tmp/deploy_d2k8s.log \
    || { echo "FATAL: $svc did not build/push — see /tmp/deploy_d2k8s.log"; tail -25 /tmp/deploy_d2k8s.log; exit 1; }
done

echo "=== [3] inject perf env (GOMAXPROCS from cpu limit, natural GC) + node pinning ==="
python3 ../../utils/inject_perf_env.py "$B/k8s" nt-es --gc natural
python3 ../../utils/pin_nodes.py node-pinning-nt_es.yaml "$B/k8s"

echo "=== [4] apply + rollout ==="
kubectl apply -f "$B/k8s/"
kubectl rollout restart deploy/edge-service-nt-es-ctr deploy/internal-service-nt-es-ctr
kubectl rollout status deploy/edge-service-nt-es-ctr    --timeout=300s
kubectl rollout status deploy/internal-service-nt-es-ctr --timeout=300s

echo "=== [5a] expose edge on the node IP as NodePort $NODEPORT ==="
# Blueprint/kompose emit a ClusterIP service, which the load generator (running
# on node-0, outside the pod network) cannot reach. Every measurement script
# targets http://10.10.1.3:$NODEPORT, so the edge service must be published on
# node-2's IP at that fixed port.
#
# This step was originally done by hand and was NOT in any script -- when the
# cluster was rebuilt it was silently lost, and wrk simply hung against a dead
# address. Scripted here so a rebuild cannot drop it again.
#
# 11011 is only usable because this cluster runs the apiserver with
# --service-node-port-range=2000-36767 (the default 30000-32767 would reject it).
kubectl patch svc edge-service-nt-es-ctr --type=json -p="[
 {\"op\":\"replace\",\"path\":\"/spec/type\",\"value\":\"NodePort\"},
 {\"op\":\"add\",\"path\":\"/spec/ports/1/nodePort\",\"value\":$NODEPORT}]" >/dev/null
kubectl get svc edge-service-nt-es-ctr -o jsonpath='{.spec.type}{" "}{.spec.ports[1].port}{":"}{.spec.ports[1].nodePort}{"\n"}'

echo "=== [5] verify placement + limits ==="
kubectl get pods -o custom-columns=NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase --no-headers \
  | grep -E "edge-service|internal-service"
for d in edge-service-nt-es-ctr internal-service-nt-es-ctr; do
  printf "  %-28s cpu_limit=%s GOMAXPROCS=%s\n" "$d" \
    "$(kubectl get deploy $d -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}')" \
    "$(kubectl get deploy $d -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="GOMAXPROCS")].value}')"
done

echo "=== [6] smoke test ==="
S=$(REQ_DIST=fixed REQ_SIZE=1024 RES_DIST=fixed RES_SIZE=1024 \
    wrk -t 4 -c 32 -d 10s -L -s workload/payload.lua http://10.10.1.3:11011 -R 2000 2>&1)
echo "$S" | grep -E "^Requests/sec|Non-2xx|Socket errors" || true
echo "$S" | grep -q "Requests/sec" || { echo "FATAL: smoke test produced no result"; exit 1; }
BAD=$(echo "$S" | awk '/Non-2xx/{print $NF}'); [ "${BAD:-0}" -gt 0 ] && { echo "FATAL: $BAD non-2xx responses"; exit 1; }
echo "=== DEPLOY OK ==="
