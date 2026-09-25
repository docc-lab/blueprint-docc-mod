#!/usr/bin/env bash
# Tomislav-RetCtx: GOMAXPROCS=40 (unset, Go 1.24 default) vs 1 for a 0.5-CPU collector, k8s-like placement.
set -euo pipefail
cd /users/tomislav/blueprint-docc-mod
CFG=/users/tomislav/deployments/collector-load/spanload-dense-ramps-20260914T185629Z/collector-realistic.yaml
for G in unset 1; do
  echo "$(date -u +%H:%M:%S) start GOMAXPROCS=$G"
  sg docker -c "/users/tomislav/blueprint-docc-mod/.venv/bin/python -B -u /tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad/run_spanload_ramp_k8s.py --out /users/tomislav/deployments/collector-load/gomaxprocs-test-20260923T1747Z/gomaxprocs-$G --collector-config $CFG --collector-gomemlimit 230MiB --profile semconv10-example --variants none,sb --rates 20000,40000,60000,80000,100000 --seconds 25 --collector-quota 0.5 --collector-gomaxprocs $G --no-cpuset --generator-cpus 4-7 --generator-cpus 14-17"
  echo "$(date -u +%H:%M:%S) done GOMAXPROCS=$G"
done
echo "$(date -u +%H:%M:%S) ALL DONE"
