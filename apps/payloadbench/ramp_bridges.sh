#!/usr/bin/env bash
# ramp_bridges.sh — what does tracing-bridge metadata on the call path COST?
#
# A bridge carries extra bytes ON THE CALL PATH -- i.e. on the REQUEST only.
# Context/baggage propagates downstream with the call; the response does not
# carry it back. This measures that cost directly: identical workload, identical
# everything, with and without the bridge's bytes added to the request.
#
# BRIDGES (all cpd 6; request baggage only)
#   none  +0 B                     control
#   pb    24 B constant            p-bridge, simulated traces
#   cgpb  24 B / 33 B two-point    cg-bridge, Uber traces (mean 24.96, CV 0.11)
#   sb    bucketed histogram       s-bridge, Uber traces (mean 32.95, CV 2.86,
#                                  p99 117 B, p99.99 ~3 KB, max ~126 KB)
# Distributions live in payload.lua as named dists, stated in their own units;
# <P>_BASE adds the application's base payload on top. cgpb/sb CANNOT be reduced
# to a constant -- sb's whole point is that its size VARIES.
#
# BASE SIZES are the SOSP'23 Fig 6 points already characterised in
# results/baselines_*: p10 200/192 B, p50 1536/320 B, p90 12000/10000 B.
#
# WHY PAIRED, INTERLEAVED: the effect being measured is small -- 24 B on the
# request is +6.1% of a 392 B round trip but only +0.11% of a 22 KB one -- so it
# is comparable to between-round drift. Every (size, bridge) cell is therefore run inside the
# same round, back to back, and the ANALYSIS COMPARES WITHIN A ROUND. Comparing
# today's bridge run against a baseline measured on another machine last week
# would confound the bridge with the rebuild.
#
# The base arms here are NOT redundant with results/baselines_*: they are the
# same-session control. Cluster rebuilt since; only a within-session pair is safe.
#
# Usage: ./ramp_bridges.sh              (2 rounds, all sizes, none+pb)
#        BRIDGES="none pb" SIZES=p10 ROUNDS=3 ./ramp_bridges.sh
set -uo pipefail
cd "$(dirname "$0")"
ROUNDS=${ROUNDS:-2}; ROUND_START=${ROUND_START:-1}
START=${START:-2000}; STEP=${STEP:-2000}; MAXRPS=${MAXRPS:-72000}
DUR=${DUR:-25}; BREAK=${BREAK:-8}; SETTLE=${SETTLE:-90}
SAT_FRAC=${SAT_FRAC:-0.95}; TAIL_STEPS=${TAIL_STEPS:-2}; LAT_MULT=${LAT_MULT:-20}
URL=${URL:-http://10.10.1.3:11011}
LUA=workload/payload.lua
SIZES=${SIZES:-"p10 p50 p90"}
BRIDGES=${BRIDGES:-"none cgpb sb"}
OUT=${OUTDIR:-results/bridges_$(date +%Y%m%d_%H%M%S)}; mkdir -p $OUT

# Pre-flight: a campaign against an unreachable URL wastes hours producing empty
# step files (wrk hangs, every step yields no Requests/sec line). Fail in 5s instead.
curl -s -o /dev/null --max-time 5 "$URL" || { echo "FATAL: $URL unreachable -- is the edge NodePort published? (deploy.sh stage 5a)" >&2; exit 1; }

# base payload bytes per size (request response)
base_of() {
  case $1 in
    p10) echo "200 192";;
    p50) echo "1536 320";;
    p90) echo "12000 10000";;
    *) echo "FATAL: unknown size '$1'" >&2; return 1;;
  esac
}

# Request-side env for a bridge, given the base request size ($2).
# The bridge's bytes go on the REQUEST ONLY (the call path); the response keeps
# the unmodified base size.
bridge_req_env() {
  case $1 in
    none) echo "REQ_DIST=fixed REQ_SIZE=$2";;
    pb)   echo "REQ_DIST=pb   REQ_BASE=$2";;
    cgpb) echo "REQ_DIST=cgpb REQ_BASE=$2";;
    sb)   echo "REQ_DIST=sb   REQ_BASE=$2";;
    *) echo "FATAL: unknown bridge '$1'" >&2; return 1;;
  esac
}
# nominal mean overhead, for the config header only
bridge_mean() {
  case $1 in none) echo 0;; pb) echo 24;; cgpb) echo 24.96;; sb) echo 32.95;; esac
}

{ echo "bridges: $BRIDGES   (baggage on the REQUEST only, cpd 6)"
  echo "sizes (SOSP'23 Fig 6): p10=200/192B  p50=1536/320B  p90=12000/10000B"
  echo "ramp: start=$START step=$STEP max=$MAXRPS ${DUR}s/step ${BREAK}s break rounds=$ROUNDS -t16 -c128"
  echo "saturation: achieved < ${SAT_FRAC} x offered OR mean > ${LAT_MULT}x first step, then $TAIL_STEPS more"
  echo "paired within round; url=$URL"
  for sz in $SIZES; do for br in $BRIDGES; do
    read q s <<<"$(base_of $sz)"; o=$(bridge_mean $br)
    printf "  %-4s %-5s req=%-8s res=%-6s (mean +%s B on request, %+.2f%% round trip)\n" \
      "$sz" "$br" "$(python3 -c "print(f'{$q+$o:g}')")" "$s" "$o" \
      "$(python3 -c "print(100*$o/($q+$s))")"
  done; done; } | tee $OUT/config.txt

for rd in $(seq $ROUND_START $ROUNDS); do
  # COUNTERBALANCED ORDER: odd rounds run control-first, even rounds bridge-first.
  # With a fixed order, arm position is confounded with time and within-round
  # drift lands entirely on the second arm (observed: the bridge arm reading 7.9%
  # FASTER on 10/11 steps, which +24 B cannot cause). Alternating makes drift
  # cancel in the pooled estimate instead of masquerading as a bridge effect.
  if [ $((rd % 2)) -eq 0 ]; then ORDER=$(echo $BRIDGES | tr ' ' '\n' | tac | tr '\n' ' ')
  else ORDER="$BRIDGES"; fi
  echo "--- round $rd arm order: $ORDER ---"
  for sz in $SIZES; do
    for br in $ORDER; do
      read q s <<<"$(base_of $sz)" || exit 1
      RENV=$(bridge_req_env $br $q) || exit 1
      case "$RENV" in FATAL*) echo "$RENV" >&2; exit 1;; esac
      ENVV="$RENV RES_DIST=fixed RES_SIZE=$s"
      d=$OUT/${sz}_${br}/round_$rd; mkdir -p $d
      echo "=== round $rd/$ROUNDS size=$sz bridge=$br ($ENVV) ==="
      # An arm that ended in deep saturation leaves a draining backlog that
      # contaminates the next arm's early steps (observed: 88ms at 14k).
      sleep $SETTLE
      env $ENVV wrk -t 8 -c 128 -d 30s -L -s $LUA "$URL" -R 2000 >$d/warmup.txt 2>&1
      past=0; base_lat=""
      for (( rps=START; rps<=MAXRPS; rps+=STEP )); do
        env $ENVV wrk -t 16 -c 128 -d ${DUR}s -L -s $LUA "$URL" -R $rps >$d/step_${rps}.txt 2>&1
        a=$(awk '/^Requests\/sec/{print $2}' $d/step_${rps}.txt)
        [ -z "$a" ] && { echo "  r$rd $sz/$br offered=$rps ERROR (no result)"; break; }
        m=$(sed -n 's/.*#\[Mean *= *\([0-9.]*\).*/\1/p' $d/step_${rps}.txt)
        [ -z "$base_lat" ] && base_lat="$m"
        sat=$(python3 -c "
a=float('$a'); m=float('${m:-0}'); b=float('${base_lat:-1}')
print(1 if (a < $SAT_FRAC*$rps or (b>0 and m > $LAT_MULT*b)) else 0)")
        printf "  r%s %-4s %-5s offered=%-6s achieved=%-9s mean=%-8s %s\n" \
          "$rd" "$sz" "$br" "$rps" "$a" "${m:-?}" "$([ "$sat" = 1 ] && echo '<-- saturating')"
        if [ "$sat" = 1 ]; then
          past=$((past+1))
          [ "$past" -gt "$TAIL_STEPS" ] && { echo "  r$rd $sz/$br: saturated, stopping"; break; }
        fi
        sleep $BREAK
      done
    done
  done
  echo "### ROUND $rd COMPLETE ###"
done
echo "=== BRIDGES DONE -> $OUT ==="
python3 analyze_bridges.py $OUT | tee $OUT/analysis.txt
