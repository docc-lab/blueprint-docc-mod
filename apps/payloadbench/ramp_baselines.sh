#!/usr/bin/env bash
# ramp_baselines.sh — capacity/latency baselines at three PAPER-GROUNDED payload sizes.
#
# Sizes come from Seemakhupt et al., "A Cloud-Scale Characterization of Remote
# Procedure Calls", SOSP '23 (Google fleet, ~10K RPC methods), section 2.5 / Fig 6:
#   p10  200 B req / 192 B res   response P10 = 188 B. (The paper's request-P10
#                                sentence reads "under 2030 B", which contradicts its
#                                own median of 1530 B; it is almost certainly a
#                                typo for 203.0 B. 200 B is defensible either way.)
#   p50  1536 B req / 320 B res  paper: median request 1530 B, response 315 B
#   p90  12000 B req / 10000 B res  paper: P90 request 11.8 KB, response 10 KB
# Asymmetry is deliberate: the paper finds most methods are write-dominant
# (median response/request ratio < 1).
#
# Ramps from START by STEP "until saturation", detected PER SIZE because these
# sizes move very different byte volumes (p90 moves ~12x the bytes of p10, so it
# saturates at a far lower request rate). Saturation = achieved < SAT_FRAC x offered;
# after the first such step we take TAIL_STEPS more to capture the knee, then stop.
#
# Round-interleaved (round-major) so cross-time drift hits all sizes equally.
# -c 128 is the measured throughput optimum (-c 1024 sits ~14% below peak).
set -uo pipefail
cd "$(dirname "$0")"
ROUNDS=${ROUNDS:-2}; ROUND_START=${ROUND_START:-1}
START=${START:-2000}; STEP=${STEP:-2000}; MAXRPS=${MAXRPS:-72000}
DUR=${DUR:-25}; BREAK=${BREAK:-8}; SETTLE=${SETTLE:-90}
SAT_FRAC=${SAT_FRAC:-0.95}; TAIL_STEPS=${TAIL_STEPS:-2}; LAT_MULT=${LAT_MULT:-20}
URL=${URL:-http://10.10.1.3:11011}
LUA=workload/payload.lua
SIZES=${SIZES:-"p10 p50 p90"}
OUT=${OUTDIR:-results/baselines_$(date +%Y%m%d_%H%M%S)}; mkdir -p $OUT

size_env() {
  case $1 in
    p10) echo "REQ_DIST=fixed REQ_SIZE=200   RES_DIST=fixed RES_SIZE=192";;
    p50) echo "REQ_DIST=fixed REQ_SIZE=1536  RES_DIST=fixed RES_SIZE=320";;
    p90) echo "REQ_DIST=fixed REQ_SIZE=12000 RES_DIST=fixed RES_SIZE=10000";;
    # An unknown name previously fell through to an EMPTY env and payload.lua
    # silently defaulted to ZERO-byte payloads for a whole campaign. Fail loudly.
    *) echo "FATAL: unknown size '$1'" >&2; exit 1;;
  esac
}

{ echo "sizes (SOSP'23 Fig 6): p10=200/192B  p50=1536/320B  p90=12000/10000B"
  echo "ramp: start=$START step=$STEP max=$MAXRPS ${DUR}s/step ${BREAK}s break rounds=$ROUNDS -t16 -c128"
  echo "saturation: achieved < ${SAT_FRAC} x offered, then $TAIL_STEPS more steps"
  echo "url=$URL"; } | tee $OUT/config.txt

for rd in $(seq $ROUND_START $ROUNDS); do
  for sz in $SIZES; do
    d=$OUT/$sz/round_$rd; mkdir -p $d
    ENVV=$(size_env $sz) || exit 1
    echo "=== round $rd/$ROUNDS size=$sz ($ENVV) ==="
    # An arm that ended in deep saturation (multi-second latencies) leaves a
    # draining backlog; without a settle it contaminates the NEXT arm's early
    # steps (observed: p10 r2 read 88ms at 14k right after p90 r1 saturated).
    sleep $SETTLE
    env $ENVV wrk -t 8 -c 128 -d 30s -L -s $LUA "$URL" -R 2000 >$d/warmup.txt 2>&1
    past=0; base_lat=""
    for (( rps=START; rps<=MAXRPS; rps+=STEP )); do
      env $ENVV wrk -t 16 -c 128 -d ${DUR}s -L -s $LUA "$URL" -R $rps >$d/step_${rps}.txt 2>&1
      a=$(awk '/^Requests\/sec/{print $2}' $d/step_${rps}.txt)
      [ -z "$a" ] && { echo "  r$rd $sz offered=$rps ERROR (no result)"; break; }
      m=$(sed -n 's/.*#\[Mean *= *\([0-9.]*\).*/\1/p' $d/step_${rps}.txt)
      [ -z "$base_lat" ] && base_lat="$m"
      # saturated if throughput falls short OR latency has blown up vs the first step
      sat=$(python3 -c "
a=float('$a'); m=float('${m:-0}'); b=float('${base_lat:-1}')
print(1 if (a < $SAT_FRAC*$rps or (b>0 and m > $LAT_MULT*b)) else 0)")
      printf "  r%s %-4s offered=%-6s achieved=%-9s mean=%-8s %s\n" \
        "$rd" "$sz" "$rps" "$a" "${m:-?}" "$([ "$sat" = 1 ] && echo '<-- saturating')"
      if [ "$sat" = 1 ]; then
        past=$((past+1))
        [ "$past" -gt "$TAIL_STEPS" ] && { echo "  r$rd $sz: saturated, stopping"; break; }
      fi
      sleep $BREAK
    done
  done
done
echo "=== BASELINES DONE -> $OUT ==="
python3 analyze_baselines.py $OUT | tee $OUT/analysis.txt
