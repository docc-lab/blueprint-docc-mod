#!/usr/bin/env bash
# test_hostnetwork.sh — attack the largest measured cost bucket in payloadbench:
# kernel/Kubernetes networking (31.7% of edge CPU, ~39 us/request) made up of
# Calico VXLAN encap/decap, kube-proxy nftables chains and conntrack.
#
# hostNetwork:true puts both pods in the host netns, so edge->internal traffic
# goes straight host-to-host over 10.10.1.x with no overlay and no service NAT,
# and the client can hit the edge's host port directly (no NodePort DNAT).
# dnsPolicy must become ClusterFirstWithHostNet or cluster DNS breaks.
#
# Measures before/after at the corrected concurrency (-c 128) and reports true
# CPU from cgroup cpu.stat (NOT kubectl top, which under-reports 2-8x).
set -uo pipefail
cd "$(dirname "$0")"
source ../../.venv/bin/activate
K8S=${K8S:-build_sweep_p0_r3/k8s}
export REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000

cpu_of() {  # svc-prefix node  -> cores used over 10s
  local pfx=$1 node=$2
  local uid=$(kubectl get pods --no-headers -o custom-columns=:metadata.name,:metadata.uid,:metadata.deletionTimestamp \
               | awk -v s="^$pfx" '$1~s && $3=="<none>"{print $2}' | head -1)
  ssh -o StrictHostKeyChecking=no "$node" "
    cg=\$(find /sys/fs/cgroup -maxdepth 4 -type d -name '*${uid//-/_}*' 2>/dev/null | head -1)
    a=\$(awk '/usage_usec/{print \$2}' \$cg/cpu.stat); sleep 10
    b=\$(awk '/usage_usec/{print \$2}' \$cg/cpu.stat)
    echo \"scale=2; (\$b-\$a)/10000000\" | bc" 2>/dev/null
}

measure() {  # label url
  local lbl=$1 url=$2
  wrk -t 8 -c 128 -d 12s -L -s workload/payload.lua "$url" -R 5000 >/dev/null 2>&1
  wrk -t 16 -c 128 -d 26s -L -s workload/payload.lua "$url" -R 250000 >/tmp/hn_$lbl.txt 2>&1 &
  sleep 6
  local ec=$(cpu_of edge-service node-2)
  wait
  local a=$(awk '/^Requests\/sec/{print $2}' /tmp/hn_$lbl.txt)
  local b=$(awk '/Non-2xx/{print $NF}' /tmp/hn_$lbl.txt); b=${b:-0}
  python3 -c "
a=float('$a'); c=float('${ec:-0}')
print(f'HN {'$lbl':<26} achieved={a:<10.0f} edge_cpu={c:<5.2f} cores  cpu_per_req={1e6*c/a if a else 0:.1f} us  non2xx=$b')"
  sleep 4
}

PORT=$(kubectl get svc edge-service-nt-es-ctr -o jsonpath='{.spec.ports[0].nodePort}')
echo "### BEFORE: pod network (Calico VXLAN) + NodePort ###"
measure "podnet_nodeport" "http://10.10.1.3:$PORT"

echo "### switching both services to hostNetwork ###"
for f in $K8S/edge-service-nt-es-ctr-deployment.yaml $K8S/internal-service-nt-es-ctr-deployment.yaml; do
  python3 - "$f" <<'PY'
import sys, yaml
p = sys.argv[1]
d = yaml.safe_load(open(p))
spec = d['spec']['template']['spec']
spec['hostNetwork'] = True
spec['dnsPolicy'] = 'ClusterFirstWithHostNet'   # else cluster DNS breaks in host netns
yaml.safe_dump(d, open(p, 'w'), default_flow_style=False, sort_keys=False)
print(f"  patched {p.split('/')[-1]}: hostNetwork=true dnsPolicy=ClusterFirstWithHostNet")
PY
done
kubectl apply -f $K8S/ >/dev/null
kubectl rollout restart deploy/edge-service-nt-es-ctr deploy/internal-service-nt-es-ctr >/dev/null
kubectl rollout status deploy/edge-service-nt-es-ctr --timeout=180s >/dev/null
kubectl rollout status deploy/internal-service-nt-es-ctr --timeout=180s >/dev/null
kubectl get pods --no-headers -o wide | awk '/edge-service|internal-service/ && !/Terminating/{print "  "$1" ip="$6" node="$7}'
# in host netns the edge listens on the node itself; no NodePort hop needed
echo "### AFTER: hostNetwork, client -> node-2:2000 directly ###"
measure "hostnet_direct" "http://10.10.1.3:2000"
echo "### done (revert with: git checkout $K8S or re-run pin_nodes/apply) ###"
