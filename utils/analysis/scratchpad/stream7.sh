#!/usr/bin/env bash
# Tomislav-RetCtx: per-point stream for the SN ClickHouse+memlimit round and the hotel real-work sweep: every point with
# same-app no-tracing / vanilla comparisons, the earlier default-GC result where one exists, and LOSS (bridges: LP % and HP
# lost at agents; vanilla: spans lost before the agents, every one trace-breaking); every stage change; heartbeat.
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad; seen=$S/snnw_stream4.seen
roots() { for x in snnw_memch_v_root snnw_memch_br_root hotel_mem_nt_root hotel_mem_v_root hotel_mem_br_root; do cat $S/$x.txt 2>/dev/null; done; }
last=$(date +%s); rm -f $S/snnw_status.state
while true; do
  n0=$(wc -l < $seen)
  for R in $(roots); do
    for f in $(find $R/run/ -mindepth 3 -maxdepth 3 -name result.json 2>/dev/null | sort); do
      grep -qx "$f" $seen && continue
      python3 -c "import json,sys; sys.exit(0 if 'collector_deltas' in json.load(open('$f')) else 1)" 2>/dev/null || continue
      echo $f >> $seen
      python3 $S/stream7_point.py $f $S
    done
  done
  [ "$(wc -l < $seen)" != "$n0" ] && last=$(date +%s)
  hb=""; [ $(( $(date +%s) - last )) -ge 150 ] && hb="--heartbeat"
  out=$(python3 $S/snnw_status.py $S/snnw_status.state $hb $(roots) 2>/dev/null); [ -n "$out" ] && { echo "$out"; last=$(date +%s); }
  grep -q "HOTEL MEM CHAIN COMPLETE" $S/hotel_mem_chain.log 2>/dev/null && { echo "HOTEL SWEEP COMPLETE"; exit 0; }
  kill -0 $(cat $S/hotel_mem_chain.pid 2>/dev/null || echo 999999) 2>/dev/null || { echo "HOTEL CHAIN GONE: $(tail -3 $S/hotel_mem_chain.log | cut -c1-200)"; exit 1; }
  for R in $(roots); do [ -f $R/run-status.json ] && grep -q '"failed"' $R/run-status.json && { echo "RUN FAILED: $R $(head -c 300 $R/run-status.json)"; exit 1; }; done
  sleep 10
done
