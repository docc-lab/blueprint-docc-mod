#!/usr/bin/env bash
# Peak throughput of two builds, with repetitions so the difference can be judged against the
# run-to-run noise.
#
# Usage:
#   tput_ab.sh <build A> <build B> <kind> <out dir> [overload rate] [seconds] [rounds]
#
#   overload rate   offered load of the measured step (default 1600 req/s), well above what any
#                   build serves (vanilla ~1,190 req/s, PB ~1,050, SB ~960 on this machine), so the
#                   achieved rate is the build's capacity, not the offered rate
#   seconds         length of each step (default 45; wrk2 records latency only after ~10 s)
#   rounds          how many times to run the ABBA block (default 2: 4 runs of each build)
#
# Why: CPU per request at a fixed rate (cpu_ab.sh) varies by about 1% between runs of the same build,
# too much for sub-1% changes.  The throughput the latency runs recorded at saturation varied by only
# 0.1-0.6% between repetitions of the same build (lat4, lat5), and it is what a user of the system
# sees.  At saturation, 32 connections each have a request outstanding all the time (wrk2 is open
# loop, but cannot send more than one request per connection), so the achieved rate is how fast
# the services, as placed on CPUs 0-5, can complete ComposePost call trees.
#
# Each run: fresh start of all services (run_local.sh), a warm-up step at 700 req/s (below every
# build's saturation; its CPU per request is reported too), RATE_PAUSE (5 s), then the overload step.
# Order A, B, B, A per round.  Load and placement as in cpu_ab.sh / latency_ab.sh: 32 connections,
# 2 wrk2 threads on CPUs 6-7, services on 0-5, fake collector counting only.
#
# Output: <out>/<n>_<label>/ per run (wrk-<rate>.out, cpu-<rate>.txt, logs), and a summary (also in
# <out>/summary.txt): per run the achieved throughput under overload, socket errors, and the warm-up
# CPU per request; per build the mean and spread; the difference of the means; and the difference
# within each ABBA round (mean of its two B runs minus mean of its two A runs).  The machine drifts
# by up to ~1% over a session, which the per-build spread includes but the ABBA order cancels out of
# each round's difference, so the per-round differences are the ones to judge against each other.
# SUMMARY_ONLY=1 recomputes the summary of an existing <out> without running anything.
#
# LABEL_A, LABEL_B (default "old", "new") name the two builds in the run directories and summary.
#
# Example:
#   utils/grpc_interceptors/tput_ab.sh $W/pb_interceptor $W/pb_interceptor_raw pb $W/tput/pb
set -euo pipefail

# One measurement at a time (see cpu_ab.sh).
exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
flock -x -n 9 || { echo "another measurement or local run is in progress; not starting" >&2; exit 1; }
export MEASURE_LOCK_HELD=1

usage='usage: tput_ab.sh <build A> <build B> <kind> <out dir> [overload rate] [seconds] [rounds]'
build_a=${1:?$usage}
build_b=${2:?$usage}
kind=${3:?$usage}
out=$(realpath -m "${4:?$usage}")
overload=${5:-1600}
seconds=${6:-45}
rounds=${7:-2}
warmup=700
here=$(cd "$(dirname "$0")" && pwd)
A=${LABEL_A:-old} B=${LABEL_B:-new}
if [ "${SUMMARY_ONLY:-0}" != 1 ]; then
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$out"

export WRK_THREADS=2 WRK_CONNECTIONS=32 WRK_CPUS=${WRK_CPUS:-6-7} SERVICE_CPUS=${SERVICE_CPUS:-0-5}
export RECORD_SPANS=0
unset BRIDGE_CONFIG PROFILE_SECONDS

n=0
for _ in $(seq 1 "$rounds"); do
    for label in "$A" "$B" "$B" "$A"; do
        n=$((n + 1))
        dir=$build_a; [ "$label" = "$B" ] && dir=$build_b
        "$here/run_local.sh" "$dir" "$out/${n}_$label" "$kind" "$seconds" "$warmup,$overload" 2>&1 \
            | grep -E '^rate' | sed "s/^/$(date -u +%T) run $n ($label): /"
    done
done
fi

python3 - "$out" "$A" "$B" "$warmup" "$overload" <<'EOF' | tee "$out/summary.txt"
import glob, os, re, statistics, sys
out, la, lb, warmup, overload = sys.argv[1:6]
tput = {la: [], lb: []}
cpu = {la: [], lb: []}
rounds = {}   # round -> {label: [(throughput, cpu)]}
for run in sorted(glob.glob(os.path.join(out, '*_*')), key=lambda p: int(os.path.basename(p).split('_')[0])):
    name = os.path.basename(run)
    label = name.split('_', 1)[1]
    wo = os.path.join(run, f'wrk-{overload}.out')
    if label not in tput or not os.path.exists(wo):
        continue
    text = open(wo).read()
    rps = float(re.search(r'Requests/sec:\s*([\d.]+)', text).group(1))
    errors = re.search(r'Socket errors:.*', text)
    non2xx = re.search(r'Non-2xx or 3xx responses:\s*(\d+)', text)
    tput[label].append(rps)
    reqs = int(re.search(r'(\d+) requests in', open(os.path.join(run, f'wrk-{warmup}.out')).read()).group(1))
    secs = sum(float(l.split()[1]) for l in open(os.path.join(run, f'cpu-{warmup}.txt')))
    cpu[label].append(secs / reqs * 1e3)
    rounds.setdefault((int(name.split('_')[0]) - 1) // 4, {la: [], lb: []})[label].append((rps, cpu[label][-1]))
    note = '  ' + errors.group(0) if errors else ''
    note += f'  non-2xx: {non2xx.group(1)}' if non2xx else ''
    print(f'{name:12s} {rps:8.1f} req/s at {overload} offered   warm-up {cpu[label][-1]:.3f} ms CPU per request{note}')
print()
for what, per, unit, fmt in (('throughput', tput, 'req/s', '.1f'), ('warm-up CPU', cpu, 'ms/req', '.3f')):
    a, b = per[la], per[lb]
    if not a or not b:
        continue
    for label, v in per.items():
        print(f'{what:12s} {label:8s} mean {statistics.mean(v):{fmt}} {unit}   spread (max-min) {max(v) - min(v):{fmt}}   runs {len(v)}')
    d = statistics.mean(b) - statistics.mean(a)
    noise = max(max(a) - min(a), max(b) - min(b))
    print(f'{what:12s} difference ({lb} - {la}): {d:+{fmt}} {unit} ({d / statistics.mean(a):+.2%}); '
          f'noise (largest same-build spread): {noise:{fmt}} ({noise / statistics.mean(a):.2%})')
    k = 0 if what == 'throughput' else 1
    per_round = [statistics.mean(x[k] for x in r[lb]) - statistics.mean(x[k] for x in r[la])
                 for _, r in sorted(rounds.items()) if r[la] and r[lb]]
    print(f'{what:12s} per ABBA round ({lb} - {la}): ' +
          ', '.join(f'{x:+{fmt}} ({x / statistics.mean(a):+.2%})' for x in per_round))
    print()
EOF
