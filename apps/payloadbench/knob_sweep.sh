#!/usr/bin/env bash
# knob_sweep.sh — test config knobs that might lift the ~56k per-process ceiling.
# All are env-only (kubectl set env + rollout), no image rebuild.
# Every case is measured at -c 128 (the measured throughput optimum; -c 1024 sits
# ~14% below peak with 9x inflated latency).
set -uo pipefail
cd "$(dirname "$0")"
export REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000
D=deploy/edge-service-nt-es-ctr

measure() {  # label
  kubectl rollout status $D --timeout=180s >/dev/null 2>&1
  sleep 3
  wrk -t 8 -c 128 -d 12s -L -s workload/payload.lua http://10.10.1.3:11011 -R 5000 >/dev/null 2>&1
  local O=$(wrk -t 16 -c 128 -d 20s -L -s workload/payload.lua http://10.10.1.3:11011 -R 200000 2>&1)
  local A=$(awk '/^Requests\/sec/{print $2}' <<<"$O")
  local P=$(awk '/^ 50.000%/{print $2}' <<<"$O")
  local B=$(awk '/Non-2xx/{print $NF}' <<<"$O"); B=${B:-0}
  local C=$(kubectl top pods --no-headers 2>/dev/null | awk '/edge-service/{print $2}')
  printf "KNOB %-34s achieved=%-10s p50=%-9s edge_cpu=%-7s non2xx=%s\n" "$1" "$A" "$P" "$C" "$B"
  sleep 4
}

echo "### baseline: GOMAXPROCS=8, ClusterIP dial, natural GC, -c 128 ###"
kubectl set env $D GOMAXPROCS=8 GOGC=100 GC_INTERVAL_SEC=0 >/dev/null
measure "baseline_gomaxprocs8"

for G in 4 6 12; do
  kubectl set env $D GOMAXPROCS=$G >/dev/null
  measure "GOMAXPROCS=$G"
done
kubectl set env $D GOMAXPROCS=8 >/dev/null

for GG in 400 800; do
  kubectl set env $D GOGC=$GG >/dev/null
  measure "GOGC=$GG (fewer GC cycles)"
done
kubectl set env $D GOGC=100 >/dev/null

# Bypass the ClusterIP: dial the internal POD IP directly, skipping kube-proxy's
# service DNAT chains (~10% of CPU was nftables/conntrack rule evaluation).
IIP=$(kubectl get pods --no-headers -o wide | awk '/^internal-service/ && !/Terminating/{print $6; exit}')
IPORT=$(kubectl get svc internal-service-nt-es-ctr -o jsonpath='{.spec.ports[0].targetPort}')
[ -z "$IPORT" ] && IPORT=12345
echo "### dialing internal pod directly at $IIP:$IPORT (bypass ClusterIP) ###"
kubectl set env $D INTERNAL_SERVICE_NT_ES_GRPC_DIAL_ADDR="$IIP:$IPORT" >/dev/null
measure "direct_pod_ip_no_kubeproxy"
kubectl set env $D INTERNAL_SERVICE_NT_ES_GRPC_DIAL_ADDR="internal-service-nt-es-ctr:$IPORT" >/dev/null
measure "back_to_clusterip_control"
echo "### KNOB SWEEP COMPLETE ###"
