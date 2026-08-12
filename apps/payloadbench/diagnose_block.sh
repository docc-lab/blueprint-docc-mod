#!/usr/bin/env bash
# diagnose_block.sh — find WHERE the per-process bottleneck blocks.
#
# perf tells us where CPU is spent; this tells us where goroutines WAIT, which is
# what identifies a serialization point. Requires the image built with pprof.go
# and PPROF_ADDR set on the deployment.
set -uo pipefail
cd "$(dirname "$0")"
OUT=${OUT:-/tmp/pprof_diag}; mkdir -p $OUT
PORT=6060
EP=$(kubectl get pods --no-headers -o custom-columns=:metadata.name,:metadata.deletionTimestamp \
      | awk '/^edge-service/ && $2=="<none>"{print $1}' | head -1)
echo "pod=$EP"

kubectl port-forward "$EP" $PORT:$PORT >/tmp/pf.log 2>&1 &
PF=$!
trap 'kill $PF 2>/dev/null' EXIT
sleep 4
curl -sf "http://localhost:$PORT/debug/pprof/" >/dev/null || { echo "pprof endpoint unreachable"; cat /tmp/pf.log; exit 1; }
echo "pprof reachable"

export REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000
wrk -t 8 -c 128 -d 12s -L -s workload/payload.lua http://10.10.1.3:11011 -R 5000 >/dev/null 2>&1
wrk -t 16 -c 1024 -d 45s -L -s workload/payload.lua http://10.10.1.3:11011 -R 90000 >$OUT/load.txt 2>&1 &
sleep 15

echo "=== GOROUTINE SUMMARY (where are they parked?) ==="
curl -sf "http://localhost:$PORT/debug/pprof/goroutine?debug=1" -o $OUT/goroutine.txt
head -1 $OUT/goroutine.txt
grep -E "^[0-9]+ @" $OUT/goroutine.txt | sort -rn | head -12 | sed 's/^/  /'
echo
echo "=== top parked stacks (count + first meaningful frames) ==="
awk '/^[0-9]+ @/{cnt=$1; getline; f1=$1; getline; f2=$1; getline; f3=$1;
     printf "  %6d  %s | %s | %s\n", cnt, f1, f2, f3}' $OUT/goroutine.txt | sort -rn | head -12
echo
echo "=== BLOCK PROFILE (cumulative blocked time) ==="
curl -sf "http://localhost:$PORT/debug/pprof/block?debug=1" -o $OUT/block.txt
sed -n '1,4p' $OUT/block.txt | sed 's/^/  /'
grep -E "^[0-9]+ [0-9]+ @" $OUT/block.txt | head -8 | sed 's/^/  /'
echo
echo "=== MUTEX PROFILE (contention) ==="
curl -sf "http://localhost:$PORT/debug/pprof/mutex?debug=1" -o $OUT/mutex.txt
sed -n '1,4p' $OUT/mutex.txt | sed 's/^/  /'
grep -E "^[0-9]+ [0-9]+ @" $OUT/mutex.txt | head -8 | sed 's/^/  /'
wait
awk '/^Requests\/sec/{print "achieved during diagnosis: "$2}' $OUT/load.txt
echo "raw profiles in $OUT/"
