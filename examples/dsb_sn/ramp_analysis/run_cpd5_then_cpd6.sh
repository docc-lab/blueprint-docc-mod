#!/bin/bash
# 5 SB ramps cpd=5 then 5 SB ramps cpd=6, both with 35/50/70 thresholds.

run_one_cpd() {
  local CPD=$1
  echo
  echo "########## ENTERING cpd=$CPD PHASE [$(date +%H:%M:%S)] ##########"
  echo
  # Edit config to set cpd
  python3 -c "
import re
p='/users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/docker/otelcol_sb_esrev0_ctr/config.yaml'
s=open(p).read()
s=re.sub(r'cpd: \d+', f'cpd: $CPD', s)
open(p,'w').write(s)
print(f'set cpd=$CPD in config')
"
  echo "=== Rebuild otelcol with cpd=$CPD ==="
  cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/docker/otelcol_sb_esrev0_ctr
  sudo docker build --no-cache --pull -t 10.10.1.1:30000/otelcol-sb-esrev0-ctr:latest . 2>&1 | tail -5
  sudo docker push 10.10.1.1:30000/otelcol-sb-esrev0-ctr:latest 2>&1 | tail -3
  echo

  for N in 1 2 3 4 5; do
    echo "=== [$(date +%H:%M:%S)] cpd=$CPD 35/50/70 Round $N — teardown all ==="
    kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_vesrev0/k8s/ --ignore-not-found=true --wait=true &
    kubectl delete -f /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_sb_esrev0/k8s/ --ignore-not-found=true --wait=true &
    wait
    for i in {1..60}; do
      remaining=$(kubectl get pods 2>/dev/null | awk '/esrev0/' | wc -l)
      [ "$remaining" -eq 0 ] && echo "all esrev0 pods gone" && break
      sleep 5
    done

    echo "=== [$(date +%H:%M:%S)] cpd=$CPD 35/50/70 Round $N — ramp ==="
    OUT=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/runs/ramp_sb_3tier_cpd${CPD}_355070_2to4k_r${N}_$(date +%Y-%m-%d_%H%M%S)
    echo "OUT=$OUT"
    /users/tomislav/blueprint-docc-mod/utils/teardown_seed_ramp.sh sb_esrev0 sb-esrev0 "$OUT"
    echo "=== [$(date +%H:%M:%S)] cpd=$CPD 35/50/70 Round $N done → $OUT ==="
  done
  echo "########## ALL 5 cpd=$CPD ROUNDS DONE ##########"
}

run_one_cpd 5
run_one_cpd 6
echo "=== [$(date +%H:%M:%S)] ALL cpd=5 AND cpd=6 SWEEPS DONE ==="
