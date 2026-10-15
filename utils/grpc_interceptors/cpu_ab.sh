#!/usr/bin/env bash
# CPU per request, wrappers vs interceptors, with repetitions so the difference can be judged
# against the run-to-run noise.
#
# Usage:
#   cpu_ab.sh <wrapper build> <interceptor build> <kind> <out dir> [rate] [seconds] [rounds]
#
#   rate, seconds   fixed load per run (default 700 req/s for 60 s); below saturation, so both
#                   builds serve the same number of requests
#   rounds          how many times to run the ABBA block (default 2: 4 runs of each build)
#
# Why: a single run's total CPU per request varies by a few percent (e.g. the same PB wrapper build
# measured 5.111 and 4.949 ms per request in two runs), which is as large as the differences we want
# to see.  So: runs one at a time on an idle machine, in the order wrapper, interceptor, interceptor,
# wrapper (repeated), every run a fresh start of all services, no profiling (it costs CPU itself).
# Load and placement as in latency_ab.sh: 32 connections, wrk2 on CPUs 6-7, services on 0-5, fake
# collector counting only.  CPU per process comes from run_local.sh (cpu.txt, from /proc).
#
# Output: <out>/<n>_<build>/ per run, and a summary (also in <out>/summary.txt): total CPU per
# request of every run, mean and spread (max - min) per build, and the difference of the means.
#
# LABEL_A, LABEL_B (default "wrapper", "interceptor") name the two builds in the run directories
# and the summary, e.g. LABEL_A=old LABEL_B=new to compare two interceptor builds.
#
# Example:
#   utils/grpc_interceptors/cpu_ab.sh $W/sb_wrapper $W/sb_interceptor sb $W/cpu/sb 700 60 2
set -euo pipefail

# One measurement at a time: hold an exclusive lock for the whole run, and refuse to start if any
# other measurement or local run holds the lock (see run_local.sh for the shared side).
exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
flock -x -n 9 || { echo "another measurement or local run is in progress; not starting" >&2; exit 1; }
export MEASURE_LOCK_HELD=1   # our own run_local.sh calls must not try to lock again

wrapper=${1:?usage: cpu_ab.sh <wrapper build> <interceptor build> <kind> <out dir> [rate] [seconds] [rounds]}
interceptor=${2:?}
kind=${3:?}
out=$(realpath -m "${4:?}")
rate=${5:-700}
seconds=${6:-60}
rounds=${7:-2}
here=$(cd "$(dirname "$0")" && pwd)
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$out"

export WRK_THREADS=2 WRK_CONNECTIONS=32 WRK_CPUS=${WRK_CPUS:-6-7} SERVICE_CPUS=${SERVICE_CPUS:-0-5}
export RECORD_SPANS=0
unset BRIDGE_CONFIG PROFILE_SECONDS

A=${LABEL_A:-wrapper} B=${LABEL_B:-interceptor}
n=0
for _ in $(seq 1 "$rounds"); do
    for build in "$A" "$B" "$B" "$A"; do
        n=$((n + 1))
        dir=$wrapper; [ "$build" = "$B" ] && dir=$interceptor
        "$here/run_local.sh" "$dir" "$out/${n}_$build" "$kind" "$seconds" "$rate" 2>&1 \
            | grep -E '^rate' | sed "s/^/$(date -u +%T) run $n ($build): /"
    done
done

python3 - "$out" "$A" "$B" <<'EOF' | tee "$out/summary.txt"
import glob, os, re, statistics, sys
out, la, lb = sys.argv[1:4]
per = {la: [], lb: []}
for run in sorted(glob.glob(os.path.join(out, '*_*')), key=lambda p: int(os.path.basename(p).split('_')[0])):
    name = os.path.basename(run)
    build = name.split('_', 1)[1]
    if build not in per or not os.path.exists(os.path.join(run, 'cpu.txt')):
        continue
    reqs = int(re.search(r'(\d+) requests in', open(os.path.join(run, 'wrk.out')).read()).group(1))
    cpu = sum(float(l.split()[1]) for l in open(os.path.join(run, 'cpu.txt')))
    ms = cpu / reqs * 1e3
    per[build].append(ms)
    print(f'{name:16s} {reqs:7d} requests  {ms:.3f} ms CPU per request')
w, i = per[la], per[lb]
print()
for b, v in per.items():
    print(f'{b:12s} mean {statistics.mean(v):.3f} ms   spread (max-min) {max(v) - min(v):.3f} ms   runs {len(v)}')
d = statistics.mean(i) - statistics.mean(w)
print(f'difference ({lb} - {la}): {d:+.3f} ms per request ({d / statistics.mean(w):+.1%}); '
      f'noise (largest same-build spread): {max(max(w) - min(w), max(i) - min(i)):.3f} ms')
EOF
