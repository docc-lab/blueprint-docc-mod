#!/usr/bin/env bash
# diagnose_block2.sh — symbolized version. debug=1 profiles give raw hex; use
# goroutine?debug=2 (readable stacks) and `go tool pprof` (symbolizes via the
# target's /debug/pprof/symbol endpoint) to name the actual functions.
set -uo pipefail
cd "$(dirname "$0")"
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
OUT=${OUT:-/tmp/pprof_diag2}; mkdir -p $OUT
PORT=6060
EP=$(kubectl get pods --no-headers -o custom-columns=:metadata.name,:metadata.deletionTimestamp \
      | awk '/^edge-service/ && $2=="<none>"{print $1}' | head -1)
echo "pod=$EP"
kubectl port-forward "$EP" $PORT:$PORT >/tmp/pf2.log 2>&1 &
PF=$!; trap 'kill $PF 2>/dev/null' EXIT
sleep 4
curl -sf "http://localhost:$PORT/debug/pprof/" >/dev/null || { echo "unreachable"; exit 1; }

export REQ_DIST=fixed REQ_SIZE=1000 RES_DIST=fixed RES_SIZE=1000
wrk -t 8 -c 128 -d 12s -L -s workload/payload.lua http://10.10.1.3:11011 -R 5000 >/dev/null 2>&1
wrk -t 16 -c 1024 -d 60s -L -s workload/payload.lua http://10.10.1.3:11011 -R 90000 >$OUT/load.txt 2>&1 &
sleep 18

echo "=== WHERE ARE THE GOROUTINES PARKED (readable, grouped) ==="
curl -sf "http://localhost:$PORT/debug/pprof/goroutine?debug=2" -o $OUT/g2.txt
echo "  total goroutines: $(grep -c '^goroutine ' $OUT/g2.txt)"
# group by the 3rd/4th frame (skip runtime park/select internals) and count
awk '
  /^goroutine /{inb=1; n=0; sig=""; next}
  inb && /^[a-zA-Z_(]/{
    n++
    if (n>=1 && n<=6) {
      fn=$0; sub(/\(.*/,"",fn)
      if (fn !~ /^runtime\./ && sig=="") sig=fn
      else if (fn !~ /^runtime\./) sig=sig" <- "fn
    }
  }
  inb && /^$/{ if (sig!="") { split(sig,a," <- "); print a[1]" <- "a[2] } inb=0 }
' $OUT/g2.txt | sort | uniq -c | sort -rn | head -12 | sed 's/^/  /'

echo
echo "=== BLOCK PROFILE (symbolized, cumulative blocked time) ==="
go tool pprof -top -nodecount=12 -unit=s "http://localhost:$PORT/debug/pprof/block" 2>/dev/null \
  | sed -n '1,20p' | sed 's/^/  /'
echo
echo "=== MUTEX PROFILE (symbolized) ==="
go tool pprof -top -nodecount=10 "http://localhost:$PORT/debug/pprof/mutex" 2>/dev/null \
  | sed -n '1,16p' | sed 's/^/  /'
wait
awk '/^Requests\/sec/{print "achieved: "$2}' $OUT/load.txt
