#!/usr/bin/env bash
# Tomislav-RetCtx: for each case of the in-app profiling run, 25 s into measuring take 30 s CPU profiles of frontend and search.
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
P=$(cat $S/appprof_root.txt); OUT=/users/tomislav/deployments/dsb-hotel/ANALYSIS-2026-09-23/app-profiles; mkdir -p $OUT
for kind in v pb cgpb sb; do
  until python3 -c "import json,sys;d=json.load(open('$P/run-status.json'));sys.exit(0 if d.get('case')=='$kind' and d.get('stage')=='measuring' else 1)" 2>/dev/null; do sleep 3; done
  sleep 25
  for svc in frontend search; do
    ip=$(kubectl -n dsb-hotel get pods -o wide --no-headers | awk -v s="^$svc-service-" '$1 ~ s {print $6}')
    curl -s --noproxy '*' -o $OUT/$kind-12k-$svc.cpu.pb.gz "http://$ip:6060/debug/pprof/profile?seconds=30" &
  done
  wait
  echo "$(date -u +%H:%M:%S) captured $kind: $(ls -la $OUT/$kind-12k-*.cpu.pb.gz | awk '{print $5}' | tr '\n' ' ')"
done
echo "$(date -u +%H:%M:%S) APP PROFILES DONE"
