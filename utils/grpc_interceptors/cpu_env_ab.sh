#!/usr/bin/env bash
# CPU per request of ONE build under two run-time settings (environment variables), with
# repetitions: the same procedure as cpu_ab.sh, but both arms run the same binaries, so the
# difference is only the setting.
#
# Usage:
#   cpu_env_ab.sh <build> <kind> <out dir> <env A> <env B> [rate] [seconds] [rounds]
#
#   env A, env B    space-separated VAR=value assignments for the services of that arm, e.g.
#                   "GRPC_STREAM_WORKERS=0" and "GRPC_STREAM_WORKERS=8"; "" for no change
#   rate, seconds   fixed load per run (default 700 req/s for 60 s); below saturation
#   rounds          how many times to run the ABBA block (default 2: 4 runs of each arm)
#
# Runs in the order A, B, B, A (repeated), each a fresh start of all services, one at a time, under
# the same exclusive lock as the other measurement scripts.  Load and placement as in cpu_ab.sh.
# PROFILE_SECONDS, if set, also takes CPU profiles in every run (it costs some CPU itself, equally
# in both arms).
#
# Output: <out>/<n>_A, <out>/<n>_B per run, and <out>/summary.txt: total CPU per request of every
# run, mean and spread per arm, the difference of the means, and the CPU per request of every
# service (mean per arm), largest difference first.
#
# Example (gRPC stream workers off vs 8):
#   utils/grpc_interceptors/cpu_env_ab.sh $W/pb_interceptor pb $W/workers/pb_0_8 \
#       "GRPC_STREAM_WORKERS=0" "GRPC_STREAM_WORKERS=8"
set -euo pipefail

exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
flock -x -n 9 || { echo "another measurement or local run is in progress; not starting" >&2; exit 1; }
export MEASURE_LOCK_HELD=1   # our own run_local.sh calls must not try to lock again

usage="usage: cpu_env_ab.sh <build> <kind> <out dir> <env A> <env B> [rate] [seconds] [rounds]"
build=${1:?$usage}
kind=${2:?$usage}
out=$(realpath -m "${3:?$usage}")
envA=${4?$usage}
envB=${5?$usage}
rate=${6:-700}
seconds=${7:-60}
rounds=${8:-2}
here=$(cd "$(dirname "$0")" && pwd)
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$out"
printf 'A: %s\nB: %s\nbuild: %s\nkind: %s, %s req/s, %s s, %s rounds\n' "$envA" "$envB" "$build" "$kind" "$rate" "$seconds" "$rounds" > "$out/arms.txt"

export WRK_THREADS=2 WRK_CONNECTIONS=32 WRK_CPUS=${WRK_CPUS:-6-7} SERVICE_CPUS=${SERVICE_CPUS:-0-5}
export RECORD_SPANS=0
unset BRIDGE_CONFIG

n=0
for _ in $(seq 1 "$rounds"); do
    for arm in A B B A; do
        n=$((n + 1))
        settings=$envA; [ "$arm" = B ] && settings=$envB
        # env: the arm's settings apply to run_local.sh and everything it starts.
        # shellcheck disable=SC2086
        env $settings "$here/run_local.sh" "$build" "$out/${n}_$arm" "$kind" "$seconds" "$rate" 2>&1 \
            | grep -E '^rate' | sed "s/^/$(date -u +%T) run $n ($arm: $settings): /"
    done
done

python3 - "$out" <<'EOF' | tee "$out/summary.txt"
import glob, os, re, statistics, sys
out = sys.argv[1]
print(open(os.path.join(out, 'arms.txt')).read())
total = {'A': [], 'B': []}
per_proc = {'A': {}, 'B': {}}
for run in sorted(glob.glob(os.path.join(out, '*_[AB]')), key=lambda p: int(os.path.basename(p).split('_')[0])):
    name, arm = os.path.basename(run), os.path.basename(run).split('_')[1]
    if not os.path.exists(os.path.join(run, 'cpu.txt')):
        continue
    reqs = int(re.search(r'(\d+) requests in', open(os.path.join(run, 'wrk.out')).read()).group(1))
    cpu = {l.split()[0]: float(l.split()[1]) for l in open(os.path.join(run, 'cpu.txt'))}
    ms = sum(cpu.values()) / reqs * 1e3
    total[arm].append(ms)
    for p, c in cpu.items():
        per_proc[arm].setdefault(p, []).append(c / reqs * 1e3)
    print(f'{name:8s} {reqs:7d} requests  {ms:.3f} ms CPU per request')
a, b = total['A'], total['B']
print()
for arm, v in total.items():
    print(f'{arm}  mean {statistics.mean(v):.3f} ms   spread (max-min) {max(v) - min(v):.3f} ms   runs {len(v)}')
d = statistics.mean(b) - statistics.mean(a)
print(f'difference (B - A): {d:+.3f} ms per request ({d / statistics.mean(a):+.1%}); '
      f'noise (largest same-arm spread): {max(max(a) - min(a), max(b) - min(b)):.3f} ms')
print('\nper service, mean ms CPU per request (A, B, B - A):')
rows = []
for p in per_proc['A']:
    ma, mb = statistics.mean(per_proc['A'][p]), statistics.mean(per_proc['B'].get(p, [0]))
    rows.append((mb - ma, p, ma, mb))
for dd, p, ma, mb in sorted(rows):
    print(f'  {p[:44]:44s} {ma:7.3f} {mb:7.3f} {dd:+7.3f}')
EOF
