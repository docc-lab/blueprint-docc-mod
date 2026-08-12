#!/usr/bin/env bash
# sweep_pool.sh N [N...] — measure edge->internal capacity vs client-pool size.
#
# For each N: compile (-internal-pool N; 0 = no pool / single shared ClientConn),
# goimports, build+push images, inject natural GC, node-pin, roll out, then probe
# capacity by offering ABOVE capacity so achieved == capacity.
#
# IMPORTANT: every config must be rebuilt+pushed immediately before measuring.
# All builds push to the SAME image tag (`...-ctr:latest`), so an earlier config's
# manifests will silently pull a later config's image if you skip the rebuild.
set -euo pipefail
cd "$(dirname "$0")"
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
source ../../.venv/bin/activate
GOIMPORTS="$(go env GOPATH)/bin/goimports"
OFFER=${OFFER:-90000}; DUR=${DUR:-20}; THREADS=${THREADS:-16}; CONNS=${CONNS:-1024}
export REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000
IIP_SVC=$(kubectl get svc internal-service-nt-es-ctr -o jsonpath='{.spec.clusterIP}')

for SPEC in "$@"; do
  N=${SPEC%%:*}; R=${SPEC#*:}; [ "$R" = "$SPEC" ] && R=3
  B=build_sweep_p${N}_r${R}
  echo "########## pool N=$N retries=$R ##########"
  rm -rf $B
  go run ./wiring -w docker_nt_es -internal-pool $N -retries $R -o $B -quiet >/dev/null 2>&1
  "$GOIMPORTS" -w $B/docker
  cp $B/.local.env $B/docker/.env
  ( cd $B && sg docker -c "export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin; \
      source /users/tomislav/blueprint-docc-mod/.venv/bin/activate; \
      set -a; . docker/.env; set +a; \
      python3 /users/tomislav/blueprint-docc-mod/d2k8s/d2k8s.py --registry 10.10.1.1:30000 \
        --daemon-services otelcol-nt-es-ctr docker/docker-compose.yml k8s" ) >/tmp/sweep_d2k8s_${N}_$R.log 2>&1
  grep -q "Successfully pushed 10.10.1.1:30000/edge-service" /tmp/sweep_d2k8s_${N}_$R.log \
    || { echo "  BUILD/PUSH FAILED (see /tmp/sweep_d2k8s_${N}_$R.log)"; continue; }
  python3 ../../utils/inject_perf_env.py $B/k8s nt-es --gc natural >/dev/null
  python3 ../../utils/pin_nodes.py node-pinning-nt_es.yaml $B/k8s >/dev/null
  kubectl apply -f $B/k8s/ >/dev/null
  kubectl rollout restart deploy/edge-service-nt-es-ctr deploy/internal-service-nt-es-ctr >/dev/null
  kubectl rollout status deploy/edge-service-nt-es-ctr --timeout=180s >/dev/null
  kubectl rollout status deploy/internal-service-nt-es-ctr --timeout=180s >/dev/null
  # warm
  wrk -t 8 -c 128 -d 15s -L -s workload/payload.lua http://10.10.1.3:11011 -R 5000 >/dev/null 2>&1
  # measure + count real TCP conns from inside the edge pod's netns (ClusterIP dest)
  wrk -t $THREADS -c $CONNS -d ${DUR}s -L -s workload/payload.lua http://10.10.1.3:11011 -R $OFFER > /tmp/sweep_cap_${N}_$R.txt 2>&1 &
  sleep $(( DUR / 2 ))
  CONNCOUNT=$(ssh -o StrictHostKeyChecking=no node-2 "
    cid=\$(sudo crictl ps --name edge-service -q 2>/dev/null | head -1)
    pid=\$(sudo crictl inspect \$cid 2>/dev/null | grep -m1 '\"pid\"' | grep -oE '[0-9]+')
    sudo nsenter -t \$pid -n ss -tn state established 2>/dev/null | tail -n +2 | grep -c '$IIP_SVC'" 2>/dev/null || echo "?")
  ECPU=$(kubectl top pod --no-headers 2>/dev/null | awk '/edge-service/{print $2}')
  ICPU=$(kubectl top pod --no-headers 2>/dev/null | awk '/internal-service/{print $2}')
  wait
  ACH=$(awk '/^Requests\/sec/{print $2}' /tmp/sweep_cap_${N}_$R.txt)
  BAD=$(awk '/Non-2xx/{print $NF}' /tmp/sweep_cap_${N}_$R.txt); BAD=${BAD:-0}
  printf "SWEEP N=%-4s retries=%-2s achieved=%-10s conns_to_internal=%-5s edge_cpu=%-7s internal_cpu=%-7s non2xx=%s\n" \
    "$N" "$R" "$ACH" "$CONNCOUNT" "$ECPU" "$ICPU" "$BAD"
  sleep 5
done
