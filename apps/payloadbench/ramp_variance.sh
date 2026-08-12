#!/usr/bin/env bash
# ramp_variance.sh — throughput/latency ramps for the three payload-size arms.
#
# Complements variance_experiment.sh (which compares at matched rho, isolating the
# queueing mechanism). This compares at matched OFFERED RPS, which is the
# operational question -- an operator receives whatever traffic arrives and cannot
# choose rho -- so the measured penalty here compounds BOTH effects:
#   (a) capacity loss   : bimodal ceiling 25.8k vs fixed 28.3k at identical mean size
#   (b) queueing amplification from service-time variance
# It also gives the knee comparison and the curve shape, which rho-points cannot.
#
# All three arms share mean payload 4250 B; only the distribution differs:
#   fixed   CV_size 0     constant 4250 B
#   exp     CV_size 1.0   exponential, mean 4250 B
#   bimodal CV_size 3.31  95% x 1 KiB + 5% x 64 KiB
#
# ROUND-INTERLEAVED (round-major, not arm-major) so cross-time drift affects every
# arm equally -- per examples/dsb_sn/notes/nt_ceiling_below_sampled_tracing.md, where
# running configs hours apart produced a spurious ordering.
# Client concurrency is -c 128: the measured throughput optimum. -c 1024 sits ~14%
# below peak with 9x inflated latency from self-inflicted queueing.
set -uo pipefail
cd "$(dirname "$0")"
ROUNDS=${ROUNDS:-3}
START=${START:-4000}; END=${END:-32000}; STEP=${STEP:-2000}
DUR=${DUR:-25}; BREAK=${BREAK:-8}
URL=${URL:-http://10.10.1.3:11011}
LUA=workload/payload.lua
SMALL=1024; LARGE=65536; PLARGE=0.05
MEAN=$(python3 -c "print(round((1-$PLARGE)*$SMALL + $PLARGE*$LARGE))")
# uniform +-10% around the same mean: CV_size = (0.2*mean/sqrt(12))/mean = 0.0577
U10LO=$(python3 -c "print(round(0.9*$MEAN))"); U10HI=$(python3 -c "print(round(1.1*$MEAN))")
ARMLIST=${ARMLIST:-"fixed exp bimodal"}
OUT=${OUTDIR:-results/rampvar_$(date +%Y%m%d_%H%M%S)}; mkdir -p $OUT
{
  echo "arms: fixed(${MEAN}B CV0) exp(mean ${MEAN}B CV1) bimodal(95%x${SMALL}+5%x${LARGE} CV3.31)"
  echo "ramp: $START..$END step $STEP, ${DUR}s/step, ${BREAK}s break, rounds=$ROUNDS, -t16 -c128"
  echo "url=$URL"
} | tee $OUT/config.txt

arm_env() {
  case $1 in
    fixed)   echo "REQ_DIST=fixed REQ_SIZE=$MEAN RES_DIST=fixed RES_SIZE=$MEAN";;
    exp)     echo "REQ_DIST=exp REQ_MEAN=$MEAN RES_DIST=exp RES_MEAN=$MEAN";;
    bimodal) echo "REQ_DIST=bimodal REQ_P_LARGE=$PLARGE REQ_SMALL=$SMALL REQ_LARGE=$LARGE RES_DIST=bimodal RES_P_LARGE=$PLARGE RES_SMALL=$SMALL RES_LARGE=$LARGE";;
    unif10)  echo "REQ_DIST=uniform REQ_MIN=$U10LO REQ_MAX=$U10HI RES_DIST=uniform RES_MIN=$U10LO RES_MAX=$U10HI";;
    # identical config to `fixed`; distinct name so a re-run can act as a
    # same-session reference without overwriting the original fixed data.
    fixed_ref) echo "REQ_DIST=fixed REQ_SIZE=$MEAN RES_DIST=fixed RES_SIZE=$MEAN";;
    # An unknown arm previously fell through to an EMPTY env, and payload.lua
    # then defaulted REQ_SIZE/RES_SIZE to 0 -- silently measuring ZERO-byte
    # payloads (~30% faster) for a whole campaign. Fail loudly instead.
    *) echo "FATAL: unknown arm '$1'" >&2; exit 1;;
  esac
}

for rd in $(seq 1 $ROUNDS); do
  for arm in $ARMLIST; do
    d=$OUT/$arm/round_$rd; mkdir -p $d
    echo "=== round $rd/$ROUNDS arm=$arm : warmup ==="
    env $(arm_env $arm) wrk -t 8 -c 128 -d 30s -L -s $LUA "$URL" -R 2000 >$d/warmup.txt 2>&1
    for (( rps=START; rps<=END; rps+=STEP )); do
      env $(arm_env $arm) wrk -t 16 -c 128 -d ${DUR}s -L -s $LUA "$URL" -R $rps \
        >$d/step_${rps}.txt 2>&1
      a=$(awk '/^Requests\/sec/{print $2}' $d/step_${rps}.txt)
      echo "  r$rd $arm offered=$rps achieved=${a:-ERR}"
      sleep $BREAK
    done
  done
done
echo "=== RAMPVAR DONE -> $OUT ==="
python3 analyze_rampvar.py $OUT | tee $OUT/analysis.txt
