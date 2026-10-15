#!/usr/bin/env bash
# Wrapper-vs-interceptor comparison for one bridge kind: run the wrapper build twice and the
# interceptor build once, at the same time (same machine load for all three), then compare.
#
# Usage:
#   ab_trio.sh <wrapper build> <interceptor build> <kind> <results prefix> [seconds] [rate]
#
#   wrapper build      build dir made with build_sn.sh ... <kind> wrapper (+ build_binaries.sh)
#   interceptor build  build dir made with build_sn.sh ... <kind> interceptor (+ build_binaries.sh)
#   kind               v | pb | cgpb | sb
#   results prefix     runs go to <prefix>_wrapper_a, <prefix>_wrapper_a2, <prefix>_interceptor
#   seconds, rate      load per run (default 100 s at 20 requests/s = 2000 requests)
#
# BRIDGE_CONFIG and GOMAXPROCS are passed through to run_local.sh.  The runs can share the machine
# because run_local.sh gives each its own network namespace.
#
# With INTERCEPTOR_RUNS=2 the interceptor build also runs twice (<prefix>_interceptor_b), to tell a
# systematic wrapper/interceptor difference (both interceptor runs agree, and differ from both
# wrapper runs the same way) from run-to-run noise.
#
# Two comparisons, each printed for wrapper-vs-wrapper (the noise level) and wrapper-vs-interceptor:
#   1. compare_traces.py, trace by trace, ignoring the attributes whose value depends on random
#      draws or on the order of concurrent calls (_br, _rc, _o; plus _d when BRIDGE_CONFIG is not
#      set, i.e. with the random SN config).  Must be identical.
#   2. attr_stats.py: distribution of every bridge attribute per span name.  The interceptor's
#      largest TVD should be about the same as the wrapper-vs-wrapper one.
#
# Example (SB, fixed checkpoint distance):
#   BRIDGE_CONFIG='{"cpd_min":3,"cpd_max":3,"reverse_policy":"ttl"}' \
#   utils/grpc_interceptors/ab_trio.sh $W/sb_wrapper $W/sb_interceptor sb $W/runs/sbD_big
set -euo pipefail

wrapper=${1:?usage: ab_trio.sh <wrapper build> <interceptor build> <kind> <results prefix> [seconds] [rate]}
interceptor=${2:?}
kind=${3:?}
prefix=${4:?}
seconds=${5:-100}
rate=${6:-20}
here=$(cd "$(dirname "$0")" && pwd)

"$here/run_local.sh" "$wrapper" "${prefix}_wrapper_a" "$kind" "$seconds" "$rate" > "${prefix}_wrapper_a.out" 2>&1 &
"$here/run_local.sh" "$wrapper" "${prefix}_wrapper_a2" "$kind" "$seconds" "$rate" > "${prefix}_wrapper_a2.out" 2>&1 &
"$here/run_local.sh" "$interceptor" "${prefix}_interceptor" "$kind" "$seconds" "$rate" > "${prefix}_interceptor.out" 2>&1 &
runs="wrapper_a wrapper_a2 interceptor"
if [ "${INTERCEPTOR_RUNS:-1}" = 2 ]; then
    "$here/run_local.sh" "$interceptor" "${prefix}_interceptor_b" "$kind" "$seconds" "$rate" > "${prefix}_interceptor_b.out" 2>&1 &
    runs="$runs interceptor_b"
fi
wait

for run in $runs; do
    echo "$run: $(grep -E 'requests in|Non-2xx' "${prefix}_$run.out" | tr -s ' ' | tr '\n' ' ')$(grep -o 'spans recorded: [0-9]*' "${prefix}_$run.out")"
done

# With the default (random) SN config the depth byte _d also depends on the random checkpoint draws,
# so it can only be compared trace by trace under a fixed BRIDGE_CONFIG.
ignore="--ignore-attr _br --ignore-attr _rc --ignore-attr _o"
[ -z "${BRIDGE_CONFIG:-}" ] && ignore="$ignore --ignore-attr _d"

echo
echo "== 1. trace by trace, ignoring: ${ignore//--ignore-attr /}"
for b in ${runs#wrapper_a }; do
    echo "wrapper_a vs $b: $("$here/compare_traces.py" "${prefix}_wrapper_a" "${prefix}_$b" $ignore \
        | grep -E 'identical|RESULT' | tr '\n' ' ')"
done

echo
echo "== 2. attribute distributions"
"$here/attr_stats.py" $(for r in $runs; do echo "${prefix}_$r"; done) --rows 4
