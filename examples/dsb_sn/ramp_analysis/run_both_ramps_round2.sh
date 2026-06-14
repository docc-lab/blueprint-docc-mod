#!/bin/bash
# Round 2: both ramps, fresh state each.

echo "=== [$(date +%H:%M:%S)] Teardown everything (any lingering esrev0) ==="
kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true &
kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/k8s/ --ignore-not-found=true --wait=true &
wait

# Wait until all esrev0 pods are gone
for i in {1..60}; do
  remaining=$(kubectl get pods 2>/dev/null | awk '/esrev0/' | wc -l)
  [ "$remaining" -eq 0 ] && echo "all esrev0 pods gone" && break
  echo "[${i}/60] $remaining pods still terminating"
  sleep 5
done

echo "=== [$(date +%H:%M:%S)] Round 2 — Vanilla ramp ==="
OUT_V=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_vanilla_memlim_2to4k_r2_$(date +%Y-%m-%d_%H%M%S)
echo "OUT_V=$OUT_V"
/users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh vesrev0 v-esrev0 "$OUT_V"

echo
echo "=== [$(date +%H:%M:%S)] Teardown vanilla before SB ==="
kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true
for i in {1..60}; do
  remaining=$(kubectl get pods 2>/dev/null | awk '/v-esrev0/' | wc -l)
  [ "$remaining" -eq 0 ] && echo "all v-esrev0 pods gone" && break
  sleep 5
done

echo "=== [$(date +%H:%M:%S)] Round 2 — SB+priority(3tier) ramp ==="
OUT_S=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_sb_3tier_2to4k_r2_$(date +%Y-%m-%d_%H%M%S)
echo "OUT_S=$OUT_S"
/users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh sb_esrev0 sb-esrev0 "$OUT_S"

echo "=== [$(date +%H:%M:%S)] ROUND 2 ALL DONE ==="
echo "Vanilla:     $OUT_V"
echo "SB+priority: $OUT_S"
