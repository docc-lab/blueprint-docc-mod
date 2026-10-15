#!/usr/bin/env bash
# Controlled end-to-end latency comparison, wrappers vs interceptors, for one bridge kind.
#
# Usage:
#   latency_ab.sh <wrapper build> <interceptor build> <kind> <out dir>
#
#   wrapper build / interceptor build   build dirs (build_sn.sh + build_binaries.sh)
#   kind                                v | pb | cgpb | sb
#   out dir                             created; one sub-directory per run (see below)
#
# Methodology (also in README.md, "Latency methodology"):
#   * One run at a time; nothing else of ours runs on the machine meanwhile.
#   * Order wrapper, interceptor, interceptor, wrapper (ABBA): a slow drift of the machine over the
#     session affects both builds equally.
#   * Every run starts all services fresh (run_local.sh), with the experiments' run-time settings
#     for <kind> and the real SN bridge config.
#   * CPU split: wrk2 on CPUs $WRK_CPUS (default 6-7), services and fake collector on $SERVICE_CPUS
#     (default 0-5).  The fake collector only counts spans (RECORD_SPANS=0).
#   * Two measurements per repetition, each a separate run:
#       unloaded  1 connection at $UNLOADED_RATE req/s (default 100) for $UNLOADED_SECONDS s
#                 (default 60): per-request latency without queueing.
#       ramp      $RAMP_CONNECTIONS connections (default 32, 2 wrk2 threads) stepping through
#                 $RAMP_RATES req/s (default 300,600,900,1050,1200), $RAMP_SECONDS s per step
#                 (default 35), $RATE_PAUSE s pause between steps (default 5): queueing, tail
#                 latency and the saturation point.
#   * wrk2 is open-loop (it keeps sending at the set rate even when responses are late) and
#     corrects for coordinated omission; it does not record its first ~10 s per step
#     (calibration), so steps must be well over 10 s.
#
# Summarise with:  latency_summary.py <out dir>
#
# Example:
#   utils/grpc_interceptors/latency_ab.sh $W/v_wrapper $W/v_interceptor v $W/lat/v
set -euo pipefail

# One measurement at a time: hold an exclusive lock for the whole run, and refuse to start if any
# other measurement or local run holds the lock (see run_local.sh for the shared side).
exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
flock -x -n 9 || { echo "another measurement or local run is in progress; not starting" >&2; exit 1; }
export MEASURE_LOCK_HELD=1   # our own run_local.sh calls must not try to lock again

wrapper=${1:?usage: latency_ab.sh <wrapper build> <interceptor build> <kind> <out dir>}
interceptor=${2:?}
kind=${3:?}
out=$(realpath -m "${4:?}")
here=$(cd "$(dirname "$0")" && pwd)
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$out"

export WRK_CPUS=${WRK_CPUS:-6-7} SERVICE_CPUS=${SERVICE_CPUS:-0-5} RECORD_SPANS=0
unset BRIDGE_CONFIG   # the real SN config (cpd 2..6, inverse_depth)

i=0
for build in wrapper interceptor interceptor wrapper; do
    i=$((i + 1))
    dir=$wrapper; [ "$build" = interceptor ] && dir=$interceptor
    echo "$(date -u +%T) run $i/4: $build, unloaded"
    WRK_THREADS=1 WRK_CONNECTIONS=1 "$here/run_local.sh" "$dir" "$out/${i}_${build}_unloaded" "$kind" \
        "${UNLOADED_SECONDS:-60}" "${UNLOADED_RATE:-100}" 2>&1 | grep -E '^rate' | sed 's/^/    /'
    echo "$(date -u +%T) run $i/4: $build, ramp"
    WRK_THREADS=2 WRK_CONNECTIONS=${RAMP_CONNECTIONS:-32} "$here/run_local.sh" "$dir" "$out/${i}_${build}_ramp" "$kind" \
        "${RAMP_SECONDS:-35}" "${RAMP_RATES:-300,600,900,1050,1200}" 2>&1 | grep -E '^rate' | sed 's/^/    /'
done
echo "$(date -u +%T) done; summary:"
"$here/latency_summary.py" "$out"
