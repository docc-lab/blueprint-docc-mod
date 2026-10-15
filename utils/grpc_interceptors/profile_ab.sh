#!/usr/bin/env bash
# Where does the extra CPU of the interceptor build go?  Runs the wrapper build and then the
# interceptor build at the same fixed rate, measures CPU per request of every service, and takes a
# CPU profile of every service during the run.
#
# Usage:
#   profile_ab.sh <wrapper build> <interceptor build> <kind> <out dir> [rate] [seconds]
#
#   rate, seconds   fixed load (default 800 req/s for 60 s); pick a rate below saturation, so both
#                   builds serve the same number of requests
#
# Load and placement as in latency_ab.sh: 32 connections, wrk2 on CPUs 6-7, services on 0-5,
# fake collector counting only.  Profiles: 30 s, starting 15 s into the run (after wrk2's
# calibration).
#
# Output:
#   <out>/wrapper, <out>/interceptor   the two runs (cpu.txt, pprof/<proc>.pb.gz, wrk.out, logs)
#   printed: CPU milliseconds per request for every service, both builds, and the difference;
#            then, for the services with the largest difference, the functions whose CPU changed
#            most (go tool pprof -top -diff_base=<wrapper profile> <interceptor profile>).
#
# Example:
#   utils/grpc_interceptors/profile_ab.sh $W/v_wrapper $W/v_interceptor v $W/prof/v
set -euo pipefail

# One measurement at a time: hold an exclusive lock for the whole run, and refuse to start if any
# other measurement or local run holds the lock (see run_local.sh for the shared side).
exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
flock -x -n 9 || { echo "another measurement or local run is in progress; not starting" >&2; exit 1; }
export MEASURE_LOCK_HELD=1   # our own run_local.sh calls must not try to lock again

wrapper=${1:?usage: profile_ab.sh <wrapper build> <interceptor build> <kind> <out dir> [rate] [seconds]}
interceptor=${2:?}
kind=${3:?}
out=$(realpath -m "${4:?}")
rate=${5:-800}
seconds=${6:-60}
here=$(cd "$(dirname "$0")" && pwd)
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$out"

export WRK_THREADS=2 WRK_CONNECTIONS=32 WRK_CPUS=${WRK_CPUS:-6-7} SERVICE_CPUS=${SERVICE_CPUS:-0-5}
export RECORD_SPANS=0 PROFILE_SECONDS=30
unset BRIDGE_CONFIG

for build in wrapper interceptor; do
    dir=$wrapper; [ "$build" = interceptor ] && dir=$interceptor
    "$here/run_local.sh" "$dir" "$out/$build" "$kind" "$seconds" "$rate" 2>&1 | grep -E '^rate' | sed "s/^/$build: /"
done

# CPU per request: CPU seconds of the step / requests served.
python3 - "$out" <<'EOF'
import re, sys
out = sys.argv[1]
def load(build):
    reqs = int(re.search(r'(\d+) requests in', open(f'{out}/{build}/wrk.out').read()).group(1))
    cpu = {l.split()[0]: float(l.split()[1]) for l in open(f'{out}/{build}/cpu.txt')}
    return reqs, cpu
rw, cw = load('wrapper')
ri, ci = load('interceptor')
print(f'\nCPU per request (ms), {rw} vs {ri} requests')
print(f'{"service":42s} {"wrapper":>8s} {"intercept":>9s} {"diff":>8s}')
rows = []
for p in sorted(cw):
    w, i = cw[p] / rw * 1e3, ci.get(p, 0) / ri * 1e3
    rows.append((i - w, p, w, i))
for d, p, w, i in sorted(rows, reverse=True):
    print(f'{p[:42]:42s} {w:8.3f} {i:9.3f} {d:+8.3f}')
tw, ti = sum(cw.values()) / rw * 1e3, sum(ci.values()) / ri * 1e3
print(f'{"TOTAL":42s} {tw:8.3f} {ti:9.3f} {ti - tw:+8.3f}  ({(ti - tw) / tw:+.1%})')
open(f'{out}/top_services.txt', 'w').write('\n'.join(p for _, p, _, _ in sorted(rows, reverse=True)[:3]))
EOF

# Function-level difference for the services whose CPU per request grew most.
export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local
for p in $(cat "$out/top_services.txt"); do
    echo
    echo "== $p: functions whose CPU changed most (interceptor minus wrapper, flat and cumulative)"
    go tool pprof -top -nodecount=25 -diff_base="$out/wrapper/pprof/$p.pb.gz" "$out/interceptor/pprof/$p.pb.gz" 2>/dev/null \
        | sed -n '1,32p' | tee "$out/diff_$p.txt"
done
