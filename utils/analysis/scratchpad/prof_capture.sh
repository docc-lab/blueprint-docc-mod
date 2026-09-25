#!/usr/bin/env bash
# Tomislav-RetCtx: once the profiling run is measuring, take 30 s CPU profiles of the gateway and the node-1 agent.
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
P=$(cat $S/prof_root.txt); OUT=/users/tomislav/deployments/dsb-hotel/ANALYSIS-2026-09-23/profiles
until python3 -c "import json,sys;d=json.load(open('$P/run-status.json'));sys.exit(0 if d.get('stage')=='measuring' else 1)" 2>/dev/null; do sleep 3; done
sleep 25
GW=$(kubectl -n dsb-hotel get pods -o wide --no-headers | awk '/^otelgw-/ {print $6}')
N1=$(kubectl -n dsb-hotel get pods -o wide --no-headers | awk '/^otelcol-/ && $7=="node-1" {print $6}')
echo "$(date -u +%H:%M:%S) profiling gw $GW node-1 $N1"
curl -s --noproxy '*' -o $OUT/sb12k-gateway.cpu.pb.gz "http://$GW:1777/debug/pprof/profile?seconds=30" &
curl -s --noproxy '*' -o $OUT/sb12k-node1.cpu.pb.gz "http://$N1:1777/debug/pprof/profile?seconds=30" &
wait
ls -la $OUT
echo "$(date -u +%H:%M:%S) PROFILES DONE"
