#!/usr/bin/env bash
# Tomislav-RetCtx: reconstruction performance on the loss the live collector
# actually caused, at every point of the ramp. pb0 on PB, cgp0 on CGPB, response
# path off and on. No synthetic dropping; truth is the workload's canonical tree.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
CPUS=$(cat $S/phys_cpus.txt)
OUT=$S/realdrops.csv
echo "kind,arm,rate,traces,feasible,empty,clean_pct,edge_exact_pct,spans_present,spans_total" > "$OUT"
for kind in pb cgpb; do
  case $kind in pb) mode=pb0;; cgpb) mode=cgp0;; esac
  for arm in off on; do
    pflag=""; [ "$arm" = "on" ] && pflag="--promote"
    for rate in $(seq -w 500 500 14000); do
      f=$S/perrate/$kind-$arm-$rate.json
      [ -f "$f" ] || continue
      r=$(taskset -c "$CPUS" $S/pbjr --traces "$f" --canon-from $S/perrate/canon-$kind-$arm.json \
            --payload ranged --mode $mode --real-drops $pflag --cpd 6 --workers 20 2>/dev/null)
      sp=$(echo "$r" | sed -n 's/.*spans present \([0-9]*\) of \([0-9]*\).*/\1,\2/p')
      row=$(echo "$r" | tail -1 | awk '{gsub("%","",$4); gsub("%","",$5); print $1","$2","$3","$4","$5}')
      echo "$kind,$arm,$((10#$rate)),$row,$sp" >> "$OUT"
      echo "$(date -u +%H:%M:%S) $kind $arm $rate -> $row" >> $S/realdrops.log
    done
  done
done
echo DONE >> $S/realdrops.log
