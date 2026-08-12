#!/usr/bin/env bash
# gc_size_test.sh — paired forced-vs-natural GC comparison at two payload sizes,
# plus a capacity probe per (GC, size) that tests whether the ceiling is truly
# byte-rate bound (1000 B vs 2000 B is a 2x lever; a byte-rate ceiling must halve
# the request ceiling).
#
# Clean-region rates are matched on BYTE rate, not request rate, per the lesson
# that differing mean sizes at equal rps sit at different utilisations:
#   1000 B -> 10k, 30k rps      (= 10, 30 MB/s per direction)
#   2000 B ->  5k, 15k rps      (= 10, 30 MB/s per direction)
#
# GC mode is an env-only change (inject_perf_env.py --gc) so it needs a rollout
# but NOT an image rebuild. The service image must already be the standard
# config (no pool, retries=3).
set -uo pipefail
cd "$(dirname "$0")"
source ../../.venv/bin/activate
K8S=${K8S:-build_sweep_p0_r3/k8s}
E=http://10.10.1.3:11011
LUA=workload/payload.lua
ROUNDS=${ROUNDS:-3}
OUT=results/gc_size_$(date +%Y%m%d_%H%M%S); mkdir -p $OUT
echo "k8s=$K8S rounds=$ROUNDS out=$OUT" | tee $OUT/config.txt

measure() {  # gc size rate round
  local gc=$1 sz=$2 rate=$3 rd=$4
  REQ_DIST=fixed REQ_SIZE=$sz RES_DIST=fixed RES_SIZE=$sz \
    wrk -t 16 -c 1024 -d 30s -L -s $LUA $E -R $rate > $OUT/${gc}_sz${sz}_r${rate}_rd${rd}.txt 2>&1
}
capacity() {  # gc size
  local gc=$1 sz=$2
  REQ_DIST=fixed REQ_SIZE=$sz RES_DIST=fixed RES_SIZE=$sz \
    wrk -t 16 -c 1024 -d 20s -L -s $LUA $E -R 200000 > $OUT/${gc}_sz${sz}_capacity.txt 2>&1
  awk -v g=$gc -v s=$sz '/^Requests\/sec/{printf "CAP %s size=%s achieved=%s rps => %.2f MB/s\n", g, s, $2, $2*s/1e6}' \
    $OUT/${gc}_sz${sz}_capacity.txt
}

for GC in forced natural; do
  echo "########## GC=$GC ##########"
  python3 ../../utils/inject_perf_env.py $K8S nt-es --gc $GC >/dev/null
  python3 ../../utils/pin_nodes.py node-pinning-nt_es.yaml $K8S >/dev/null
  kubectl apply -f $K8S/ >/dev/null
  kubectl rollout restart deploy/edge-service-nt-es-ctr deploy/internal-service-nt-es-ctr >/dev/null
  kubectl rollout status deploy/edge-service-nt-es-ctr --timeout=180s >/dev/null
  kubectl rollout status deploy/internal-service-nt-es-ctr --timeout=180s >/dev/null
  # confirm the env actually took effect
  EP=$(kubectl get pods --no-headers -o custom-columns=:metadata.name,:metadata.deletionTimestamp \
        | awk '/^edge-service/ && $2=="<none>"{print $1}' | head -1)
  echo -n "  live env: "
  kubectl get pod $EP -o jsonpath='{range .spec.containers[0].env[*]}{.name}={.value} {end}' \
    | grep -oE '(GOGC|GC_INTERVAL_SEC)=[^ ]*' | tr '\n' ' '; echo
  # warm
  REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000 \
    wrk -t 8 -c 128 -d 20s -L -s $LUA $E -R 5000 >/dev/null 2>&1
  capacity $GC 1000
  sleep 5
  capacity $GC 2000
  sleep 5
  for rd in $(seq 1 $ROUNDS); do
    measure $GC 1000 10000 $rd; sleep 4
    measure $GC 1000 30000 $rd; sleep 4
    measure $GC 2000  5000 $rd; sleep 4
    measure $GC 2000 15000 $rd; sleep 4
  done
  echo "  round set complete for GC=$GC"
done
echo "### GC_SIZE_TEST COMPLETE -> $OUT ###"
