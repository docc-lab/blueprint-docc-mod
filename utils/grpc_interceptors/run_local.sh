#!/usr/bin/env bash
# Run a compiled SN no-work build locally (no docker, no kubernetes) and record every span it exports.
#
# Usage:
#   run_local.sh <build dir> <results dir> [bridge kind] [seconds] [rate]
#
#   build dir    output of build_sn.sh, with binaries from build_binaries.sh in <build dir>/bin
#   results dir  created; gets spans.jsonl (every exported span), wrk.out, and one log per process
#   bridge kind  v | pb | cgpb | sb (default v).  Sets the run-time env exactly like the experiments
#                (utils/prepare_dsb_sn_nw.py service_environment): BRIDGE_KIND, REVERSE_TRUSS, ...
#   seconds      how long wrk2 sends requests (default 10), per rate
#   rate         requests per second (default 20).  A comma-separated list (e.g. 200,400,800) runs
#                wrk2 once per rate, in that order, against the same running services (a ramp),
#                with RATE_PAUSE seconds (default 5) in between; the output then goes to
#                wrk-<rate>.out / wrk-<rate>.err instead of wrk.out / wrk.err.
#
# Environment: RANDOM_SEED (default 1) seeds the request generator; GOMAXPROCS (default 2);
# OTEL_SAMPLE_RATIO (default 1) is the head-sampling ratio of the services;
# SETTLE_SECONDS (default 15) is how long to wait after all service ports listen before load starts;
# BRIDGE_CONFIG (default the SN policy {"cpd_min":2,"cpd_max":6,"reverse_policy":"inverse_depth"}) is
# what the fake collector hands the bridge processors.  For repeatable bridge decisions use
#   BRIDGE_CONFIG='{"cpd_min":3,"cpd_max":3,"reverse_policy":"ttl"}'
# (fixed checkpoint distance, deterministic reverse routing; RT_LEAF_REJECT=1 is a rate of 1.0).
#
# Load and placement (used by the latency measurements, see latency_ab.sh):
#   WRK_THREADS, WRK_CONNECTIONS (default 1 and 1: one connection, so two runs send the same
#     request sequence, which the trace comparison relies on)
#   WRK_CPUS, SERVICE_CPUS: CPU lists for `taskset` (e.g. 6-7 and 0-5); unset = no pinning
#   RECORD_SPANS=0: the fake collector only counts spans instead of writing them (spans.jsonl
#     stays empty), so it takes less CPU away from the services
# wrk2 always runs with -L, so every wrk output has the full latency percentile distribution.
#
# CPU accounting (always on): before and after every load step, each service's CPU time is read
# from /proc/<pid>/stat; cpu.txt (or cpu-<rate>.txt) lists CPU seconds used per process during
# the step.  Profiling (opt-in): PROFILE_SECONDS=30 gives every service its own pprof port
# (BRIDGES_PPROF, see runtime/plugins/otelcol/pprof.go) and, 15 s into a load step (after wrk2's
# calibration), saves a CPU profile of every service to pprof[-<rate>]/<proc>.pb.gz, and after the
# step an allocation profile to pprof[-<rate>]/<proc>.allocs.pb.gz.
#
# What it does:
#   1. Starts fakecollector on the otelcol address from <build dir>/.local.env (and on :4317, where
#      the bridge processors export), with the SN config
#      endpoint on :8080 (cpd 2..6, inverse_depth), writing spans to <results dir>/spans.jsonl.
#   2. Starts every service binary with the addresses from .local.env (the same command line the
#      container's run.sh uses).
#   3. Runs wrk2 with examples/dsb_sn/scripts/compose-post.lua, ONE connection and a fixed seed, so
#      two runs send the same sequence of requests.
#   4. Waits for the span processors to export, stops the services, then stops the collector.
#
# Example:
#   utils/grpc_interceptors/run_local.sh ~/grpc-interceptors-work/after_step1_v_nw \
#                                        ~/grpc-interceptors-work/runs/step1_v v 10 20
#
# Locking: takes a shared lock (MEASURE_LOCK, default /tmp/grpc_interceptors_measure.lock), so it will
# not start while a measurement script runs, and vice versa.
#
# Everything runs inside a private network namespace (`unshare -rn`, no root needed), so ports
# already used on the host (on CloudLab, an nginx sits on :8080, the port the bridges ask for their
# config) do not clash, and nothing started here is reachable from outside.
set -euo pipefail

if [ -z "${IN_PRIVATE_NETNS:-}" ]; then
    # Shared lock: several local runs may run side by side (ab_trio.sh does), but not while a
    # measurement (latency_ab.sh, cpu_ab.sh, profile_ab.sh) holds the lock exclusively, and a
    # measurement cannot start while any run holds it.  The lock lives as long as file descriptor 9,
    # which the private namespace process below inherits.
    if [ -z "${MEASURE_LOCK_HELD:-}" ]; then
        exec 9>"${MEASURE_LOCK:-/tmp/grpc_interceptors_measure.lock}"
        flock -s -n 9 || { echo "a measurement is in progress; not starting" >&2; exit 1; }
    fi
    exec unshare -rn env IN_PRIVATE_NETNS=1 bash "$0" "$@"
fi
ip link set lo up

build=$(realpath "${1:?usage: run_local.sh <build dir> <results dir> [kind] [seconds] [rate]}")
results=$(realpath -m "${2:?usage: run_local.sh <build dir> <results dir> [kind] [seconds] [rate]}")
kind=${3:-v}
seconds=${4:-10}
rate=${5:-20}

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
collector_bin=${FAKECOLLECTOR:-$build/../fakecollector}
[ -e "$results" ] && { echo "refusing to overwrite existing $results" >&2; exit 1; }
mkdir -p "$results"

# Build the fake collector if needed (it is a small standalone Go module next to this script).
if [ ! -x "$collector_bin" ]; then
    (cd "$here/fakecollector" && GOWORK=off GOTOOLCHAIN=local /usr/local/go/bin/go build -o "$collector_bin" .)
fi

# --- addresses ---------------------------------------------------------------------------------
set -a; . "$build/.local.env"; set +a
otel_var=$(grep -o '^OTELCOL_[A-Z0-9_]*_DIAL_ADDR' "$build/.local.env" | head -1)
entry_var=$(grep -o '^WRK2API_SERVICE_[A-Z0-9_]*_HTTP_DIAL_ADDR' "$build/.local.env" | head -1)
otel_port=${!otel_var##*:}
entry_addr=${!entry_var}

# --- run-time environment, as in the experiments ------------------------------------------------
export GOMAXPROCS=${GOMAXPROCS:-2} GOGC=100 GC_INTERVAL_SEC=0
export BRIDGE_KIND=$kind OTLP_RETRY=off OTEL_SAMPLE_RATIO=${OTEL_SAMPLE_RATIO:-1} RT_ROOT=off RT_SAMPLE=10000
case $kind in
    v) export REVERSE_TRUSS=off RT_LEAF_REJECT=0 ;;
    *) export REVERSE_TRUSS=on RT_LEAF_REJECT=1 ;;
esac

pids=()
procs=()
pprof_ports=()
cleanup() {
    # Stop by explicit PID only.
    for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
    [ -n "${collector_pid:-}" ] && kill "$collector_pid" 2>/dev/null || true
}
trap cleanup EXIT

# The checkpoint policy the fake collector hands the bridge processors (the SN default unless set).
sn_config='{"cpd_min":2,"cpd_max":6,"reverse_policy":"inverse_depth"}'
bridge_config=${BRIDGE_CONFIG:-$sn_config}

# --- 1. collector ----------------------------------------------------------------------------------
pin_services=(); [ -n "${SERVICE_CPUS:-}" ] && pin_services=(taskset -c "$SERVICE_CPUS")
pin_wrk=();      [ -n "${WRK_CPUS:-}" ] && pin_wrk=(taskset -c "$WRK_CPUS")
discard=();      [ "${RECORD_SPANS:-1}" = 0 ] && discard=(-discard)

"${pin_services[@]}" "$collector_bin" -otlp ":$otel_port,:4317" -config-addr ":8080" \
    -out "$results/spans.jsonl" "${discard[@]}" -config "$bridge_config" \
    > "$results/fakecollector.log" 2>&1 &
collector_pid=$!
sleep 1

# --- 2. services -----------------------------------------------------------------------------------
for runsh in "$build"/docker/*/run.sh; do
    ctr=$(dirname "$runsh")
    # The launch line in run.sh looks like:  ./<proc> --a.dial_addr=$A --b.bind_addr=$B &
    line=$(grep -E '^\s*\./[a-z0-9_]+_proc ' "$runsh" | head -1 | sed -E 's/^\s*//; s/&\s*$//')
    [ -n "$line" ] || continue
    proc=$(echo "$line" | sed -E 's#^\./([a-z0-9_]+_proc).*#\1#')
    args=$(eval echo "${line#./$proc}")      # expand $ADDR variables from .local.env
    port=$((6100 + ${#pids[@]}))   # pprof port, only used with PROFILE_SECONDS
    if [ -n "${PROFILE_SECONDS:-}" ]; then
        BRIDGES_PPROF=":$port" "${pin_services[@]}" "$build/bin/$proc" $args > "$results/$proc.log" 2>&1 &
    else
        "${pin_services[@]}" "$build/bin/$proc" $args > "$results/$proc.log" 2>&1 &
    fi
    pids+=($!)
    procs+=("$proc")
    pprof_ports+=("$port")
done
echo "started ${#pids[@]} services; waiting until every service port listens"

# Every service (gRPC or HTTP) must be listening before load starts; otherwise early requests fail
# while a slow service is still starting.  The DB / cache / collector addresses in .local.env belong
# to containers we do not start, so only *_SERVICE_*_{GRPC,HTTP}_BIND_ADDR are checked.
ports=$(grep -E '^[A-Z0-9_]*_SERVICE_[A-Z0-9_]*_(GRPC|HTTP)_BIND_ADDR=' "$build/.local.env" | sed -E 's/.*://')
for port in $ports; do
    for _ in $(seq 1 120); do
        (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break
        sleep 0.5
    done
    (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null || { echo "port $port never opened" >&2; exit 1; }
done
# Settle time.  gRPC clients dial when their process starts; if the server was not up yet the dial
# is refused and gRPC backs off (1 s, 1.6 s, 2.6 s, ...) before retrying.  Calls made during that
# backoff fail at once with "connection refused".  When several runs start at the same time,
# services can come up seconds apart, so wait long enough for every backoff to run out.
sleep "${SETTLE_SECONDS:-15}"

# --- 3. load ---------------------------------------------------------------------------------------
# CPU time (user + system, in clock ticks) of every service process, one "proc ticks" line each.
cpu_ticks() {
    for k in "${!pids[@]}"; do
        echo "${procs[$k]} $(awk '{print $14 + $15}' "/proc/${pids[$k]}/stat" 2>/dev/null || echo 0)"
    done
}

IFS=, read -ra rates <<< "$rate"
for r in "${rates[@]}"; do
    # One rate keeps the old file names (wrk.out); a ramp writes wrk-<rate>.out per step.
    out=wrk; suffix=; [ "${#rates[@]}" -gt 1 ] && { out=wrk-$r; suffix=-$r; }
    if [ -n "${PROFILE_SECONDS:-}" ]; then
        mkdir -p "$results/pprof$suffix"
        ( sleep 15
          for k in "${!procs[@]}"; do
              curl -s -o "$results/pprof$suffix/${procs[$k]}.pb.gz" \
                  "http://127.0.0.1:${pprof_ports[$k]}/debug/pprof/profile?seconds=$PROFILE_SECONDS" &
          done
          wait ) &
        profiler=$!
    fi
    before=$(cpu_ticks)
    RANDOM_SEED=${RANDOM_SEED:-1} max_user_index=962 "${pin_wrk[@]}" /usr/local/bin/wrk \
        -t"${WRK_THREADS:-1}" -c"${WRK_CONNECTIONS:-1}" -d"${seconds}s" -R"$r" -L \
        -s "$repo/examples/dsb_sn/scripts/compose-post.lua" "http://$entry_addr" \
        > "$results/$out.out" 2> "$results/$out.err" || true
    after=$(cpu_ticks)
    # CPU seconds per process during this step (clock ticks / CLK_TCK).
    join <(echo "$before" | sort) <(echo "$after" | sort) \
        | awk -v hz="$(getconf CLK_TCK)" '{printf "%s %.3f\n", $1, ($3 - $2) / hz}' > "$results/cpu$suffix.txt"
    if [ -n "${PROFILE_SECONDS:-}" ]; then
        wait "$profiler"
        # Allocation profiles: everything each service allocated since it started (mostly this
        # load step).  Compare per call site with: go tool pprof -sample_index=alloc_objects ...
        for k in "${!procs[@]}"; do
            curl -s -o "$results/pprof$suffix/${procs[$k]}.allocs.pb.gz" \
                "http://127.0.0.1:${pprof_ports[$k]}/debug/pprof/allocs"
        done
    fi
    echo "rate $r: $(grep -E 'requests in|Non-2xx|Socket errors|^Requests/sec' "$results/$out.out" | tr -s ' ' | tr '\n' ' ')"
    [ "${#rates[@]}" -gt 1 ] && sleep "${RATE_PAUSE:-5}"
done

# --- 4. drain and stop -----------------------------------------------------------------------------
sleep 5   # the span processors export on a timer; give them time
for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
sleep 2
kill "$collector_pid"; wait "$collector_pid" 2>/dev/null || true
collector_pid=
if [ "${RECORD_SPANS:-1}" = 0 ]; then
    echo "spans received (not recorded): $(grep -o 'stopped after [0-9]* spans' "$results/fakecollector.log")"
else
    echo "spans recorded: $(wc -l < "$results/spans.jsonl")  -> $results/spans.jsonl"
fi
