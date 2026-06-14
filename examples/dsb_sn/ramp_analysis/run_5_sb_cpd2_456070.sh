#!/bin/bash
echo "=== Rebuild otelcol with cpd=2 + thresholds 45/60/70 ==="
cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/docker/otelcol_sb_esrev0_ctr
sudo docker build --no-cache --pull -t 10.10.1.1:30000/otelcol-sb-esrev0-ctr:latest . 2>&1 | tail -5
sudo docker push 10.10.1.1:30000/otelcol-sb-esrev0-ctr:latest 2>&1 | tail -3
echo

run_round() {
  local N=$1
  echo "=== [$(date +%H:%M:%S)] 45-60-70 cpd=2 Round $N — teardown all ==="
  kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true &
  kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/k8s/ --ignore-not-found=true --wait=true &
  wait
  for i in {1..60}; do
    remaining=$(kubectl get pods 2>/dev/null | awk '/esrev0/' | wc -l)
    [ "$remaining" -eq 0 ] && echo "all esrev0 pods gone" && break
    sleep 5
  done

  echo "=== [$(date +%H:%M:%S)] 45-60-70 cpd=2 Round $N — ramp ==="
  OUT=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_sb_3tier_cpd2_456070_2to4k_r${N}_$(date +%Y-%m-%d_%H%M%S)
  echo "OUT=$OUT"
  /users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh sb_esrev0 sb-esrev0 "$OUT"
  echo "=== [$(date +%H:%M:%S)] 45-60-70 Round $N done → $OUT ==="
}

for n in 1 2 3 4 5; do
  run_round $n
done
echo "=== [$(date +%H:%M:%S)] ALL 5 45-60-70 cpd=2 ROUNDS DONE ==="
