#!/bin/bash
# Rounds 4 and 5: both ramps each, fresh state.

run_round() {
  local N=$1
  echo "=== [$(date +%H:%M:%S)] Teardown everything (pre-round $N) ==="
  kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true &
  kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/k8s/ --ignore-not-found=true --wait=true &
  wait
  for i in {1..60}; do
    remaining=$(kubectl get pods 2>/dev/null | awk '/esrev0/' | wc -l)
    [ "$remaining" -eq 0 ] && echo "all esrev0 pods gone" && break
    sleep 5
  done

  echo "=== [$(date +%H:%M:%S)] Round $N — Vanilla ramp ==="
  OUT_V=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_vanilla_memlim_2to4k_r${N}_$(date +%Y-%m-%d_%H%M%S)
  echo "OUT_V=$OUT_V"
  /users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh vesrev0 v-esrev0 "$OUT_V"

  echo
  echo "=== [$(date +%H:%M:%S)] Teardown vanilla before SB (round $N) ==="
  kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true
  for i in {1..60}; do
    remaining=$(kubectl get pods 2>/dev/null | awk '/v-esrev0/' | wc -l)
    [ "$remaining" -eq 0 ] && echo "all v-esrev0 pods gone" && break
    sleep 5
  done

  echo "=== [$(date +%H:%M:%S)] Round $N — SB+priority(3tier) ramp ==="
  OUT_S=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_sb_3tier_2to4k_r${N}_$(date +%Y-%m-%d_%H%M%S)
  echo "OUT_S=$OUT_S"
  /users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh sb_esrev0 sb-esrev0 "$OUT_S"

  echo "=== [$(date +%H:%M:%S)] ROUND $N DONE ==="
  echo "Vanilla:     $OUT_V"
  echo "SB+priority: $OUT_S"
}

run_round 4
run_round 5
echo "=== [$(date +%H:%M:%S)] ALL ROUNDS (4,5) DONE ==="
