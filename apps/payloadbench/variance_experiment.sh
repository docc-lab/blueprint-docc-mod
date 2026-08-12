#!/usr/bin/env bash
# variance_experiment.sh — does highly-variable payload size inflate latency variance?
#
# THE HYPOTHESIS IS A QUEUEING CLAIM. For M/G/1,
#     E[W] = rho/(1-rho) * E[S]*(1 + CV_S^2)/2
# so waiting time (and its spread) depends on the VARIANCE of service time, and the
# effect is amplified as rho -> 1. The causal chain being tested is:
#     payload size variance -> SERVICE TIME variance -> response time variance
#
# Earlier attempts (uniform +-20% around 1 KB) failed because they never moved the
# middle link: payload is ~1.5% of the ~122 us per-request cost at 1 KB, so CV_S
# stayed ~0. A null there says nothing about the hypothesis.
#
# DESIGN
#  * Two arms with IDENTICAL MEAN payload, differing only in size variance:
#      fixed    : constant  4250 B          -> CV_size = 0
#      bimodal  : 95% x 1 KiB + 5% x 64 KiB -> CV_size = 3.3, mean 4250 B
#    Mean 4.25 KB is realistic RPC traffic; the tail requests cost ~3x the service
#    time of the small ones, so CV_S is genuinely large while the mean is not.
#    (Optional 3rd arm: exponential, CV_size = 1, for a dose-response in CV_S.)
#  * Load is set as a FRACTION OF EACH ARM'S OWN CAPACITY (rho), not as raw rps.
#    Matching raw rps across arms with different service times would put them at
#    different rho -- the exact confound that produced a spurious result earlier.
#  * CV_S is MEASURED, not assumed: at rho ~ 0.1 there is no queueing, so observed
#    latency IS service time and its CV is CV_S for that arm.
#  * n rounds per (arm, rho) so error bars come from between-round variability.
#
# Usage: ./variance_experiment.sh [rounds]   (default 3)
set -uo pipefail
cd "$(dirname "$0")"
ROUNDS=${1:-3}
URL=${URL:-http://10.10.1.3:11011}
LUA=workload/payload.lua
OUT=results/variance_$(date +%Y%m%d_%H%M%S); mkdir -p $OUT

SMALL=1024; LARGE=65536; PLARGE=0.05
MEAN=$(python3 -c "print(round((1-$PLARGE)*$SMALL + $PLARGE*$LARGE))")
python3 - "$SMALL" "$LARGE" "$PLARGE" <<'PY' | tee $OUT/design.txt
import sys, math
s, l, p = int(sys.argv[1]), int(sys.argv[2]), float(sys.argv[3])
m = (1-p)*s + p*l
m2 = (1-p)*s*s + p*l*l
sd = math.sqrt(m2 - m*m)
print(f"design: bimodal {100*(1-p):.0f}% x {s}B + {100*p:.0f}% x {l}B")
print(f"        mean={m:.0f}B  sd={sd:.0f}B  CV_size={sd/m:.2f}")
print(f"        fixed arm uses {round(m)}B (matched mean, CV_size=0)")
PY

# ---- arm definitions: name + the env that payload.lua consumes ----
arm_env() {
  case $1 in
    fixed)   echo "REQ_DIST=fixed REQ_SIZE=$MEAN RES_DIST=fixed RES_SIZE=$MEAN";;
    bimodal) echo "REQ_DIST=bimodal REQ_P_LARGE=$PLARGE REQ_SMALL=$SMALL REQ_LARGE=$LARGE RES_DIST=bimodal RES_P_LARGE=$PLARGE RES_SMALL=$SMALL RES_LARGE=$LARGE";;
    exp)     echo "REQ_DIST=exp REQ_MEAN=$MEAN RES_DIST=exp RES_MEAN=$MEAN";;
  esac
}

run_wrk() {  # arm rate dur conns out
  local arm=$1 rate=$2 dur=$3 conns=$4 out=$5
  env $(arm_env $arm) wrk -t 16 -c $conns -d ${dur}s -L -s $LUA "$URL" -R $rate > "$out" 2>&1
}
stat_of() {  # file field -> value
  case $2 in
    ach) awk '/^Requests\/sec/{print $2}' "$1";;
    mean) sed -n 's/.*#\[Mean *= *\([0-9.]*\).*/\1/p' "$1";;
    sd)   sed -n 's/.*StdDeviation *= *\([0-9.]*\).*/\1/p' "$1";;
    p99)  awk '/^ 99.000%/{print $2}' "$1";;
    bad)  awk '/Non-2xx/{print $NF}' "$1";;
  esac
}

ARMS="fixed exp bimodal"
declare -A CAP
echo "=== STEP 1: capacity per arm (offer far above capacity; achieved == capacity) ==="
for arm in $ARMS; do
  run_wrk $arm 5000 12 128 /dev/null
  run_wrk $arm 400000 20 256 $OUT/cap_$arm.txt
  CAP[$arm]=$(stat_of $OUT/cap_$arm.txt ach)
  echo "  $arm capacity = ${CAP[$arm]} rps"
  sleep 4
done

echo "=== STEP 2: service time at rho~0.1 (no queueing => latency IS service time) ==="
for arm in $ARMS; do
  R=$(python3 -c "print(int(0.1*float('${CAP[$arm]}')))")
  run_wrk $arm $R 25 64 $OUT/svc_$arm.txt
  M=$(stat_of $OUT/svc_$arm.txt mean); S=$(stat_of $OUT/svc_$arm.txt sd)
  python3 -c "
m=float('$M'); s=float('$S')
print(f'  $arm: E[S]={m:.3f}ms  SD[S]={s:.3f}ms  CV_S={s/m:.3f}  (offered {$R} rps)')"
  sleep 4
done

echo "=== STEP 3: rho sweep, n=$ROUNDS (P-K predicts the gap GROWS as rho->1) ==="
printf "%-9s %-6s %-3s %-10s %-10s %-9s %-9s %s\n" arm rho rd offered achieved mean_ms sd_ms CV | tee $OUT/raw.txt
for rd in $(seq 1 $ROUNDS); do
  for rho in 0.50 0.70 0.85 0.95; do
    for arm in $ARMS; do
      R=$(python3 -c "print(int($rho*float('${CAP[$arm]}')))")
      f=$OUT/${arm}_rho${rho}_rd${rd}.txt
      run_wrk $arm $R 30 128 $f
      A=$(stat_of $f ach); M=$(stat_of $f mean); S=$(stat_of $f sd); B=$(stat_of $f bad)
      python3 -c "
m=float('$M'); s=float('$S')
print(f\"{'$arm':<9} {'$rho':<6} {'$rd':<3} {$R:<10} {float('$A'):<10.0f} {m:<9.3f} {s:<9.3f} {s/m:.3f}\")" | tee -a $OUT/raw.txt
      sleep 4
    done
  done
done
echo "=== DONE -> $OUT ==="
python3 analyze_variance.py $OUT | tee $OUT/analysis.txt
