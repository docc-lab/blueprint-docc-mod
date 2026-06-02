#!/bin/bash
# DSB-SN ramp runner with arbitrary start/end/step/duration/break.
# Each iteration: teardown -> reapply -> NodePort patch -> seed -> warmup -> ramp.
#
# Prereqs: wrk on PATH, kubectl on PATH, and a python3 with aiohttp installed.
# Override the python interpreter with PYTHON3 (defaults to "python3" on PATH);
# point it at a venv python if the system python lacks aiohttp.

usage() {
    cat <<EOF
Usage: $0 --target TARGET [options]

Required:
  --target TARGET          Identifier suffix (e.g. vrev0). Used as both the
                           build directory name (build_TARGET/) and the
                           kompose-normalized service-name suffix
                           (wrk2api-service-TARGET-ctr, jaeger-TARGET-ctr).

Options:
  --loops N                Iterations to run (default 0 = a single iteration).
  --start RPS              Ramp start rate (default 500).
  --end RPS                Ramp end rate, inclusive (default 2000).
  --step RPS               Ramp step size (default 100).
  --duration SECS          Seconds per ramp step (default 60).
  --break SECS             Seconds between ramp steps (default 30).
  --warmup-rps RPS         Warmup rate (default = --start).
  --warmup-duration SECS   Warmup duration (default 100).
  -h, --help               Show this help and exit.

Connections (-c) and threads (-t) for wrk are derived per step:
  C = ceil(rps^2 / 20000),  T = ceil(C / 10).
EOF
}

# Defaults
TARGET=""
LOOPS=0
START=500
END=2000
STEP=100
DURATION=60
BREAK=30
WARMUP_RPS=""
WARMUP_DURATION=100

while [[ $# -gt 0 ]]; do
    case "$1" in
        --target)           TARGET="$2"; shift 2;;
        --loops)            LOOPS="$2"; shift 2;;
        --start)            START="$2"; shift 2;;
        --end)              END="$2"; shift 2;;
        --step)             STEP="$2"; shift 2;;
        --duration)         DURATION="$2"; shift 2;;
        --break)            BREAK="$2"; shift 2;;
        --warmup-rps)       WARMUP_RPS="$2"; shift 2;;
        --warmup-duration)  WARMUP_DURATION="$2"; shift 2;;
        -h|--help)          usage; exit 0;;
        *) echo "Unknown arg: $1" >&2; usage >&2; exit 1;;
    esac
done

if [[ -z "$TARGET" ]]; then
    echo "Error: --target is required" >&2
    usage >&2
    exit 1
fi
[[ -z "$WARMUP_RPS" ]] && WARMUP_RPS="$START"

THIS_DIR=$(pwd)
LUA=/users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua
PYTHON3=${PYTHON3:-python3}

echo "Plan: $((LOOPS+1)) iteration(s) of target=$TARGET"
echo "  Ramp:   $START -> $END rps, step $STEP, ${DURATION}s on / ${BREAK}s off"
echo "  Warmup: $WARMUP_RPS rps for ${WARMUP_DURATION}s"
echo
echo "=================================================="
echo "=================================================="
echo

for iternum in $(seq 0 $LOOPS); do
    echo "Loop $iternum"
    echo "--------------------------------------------------"
    echo "--------------------------------------------------"
    echo

    sleep 120
    cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn
    kubectl delete -f build_$TARGET/k8s/ --ignore-not-found=true
    sleep 60
    kubectl apply -f build_$TARGET/k8s/
    sleep 120
    kubectl patch service wrk2api-service-$TARGET-ctr -p '{"spec":{"type":"NodePort"}}'
    kubectl patch service jaeger-$TARGET-ctr -p '{"spec":{"type":"NodePort"}}' || true
    NODEPORT=$(kubectl get services wrk2api-service-$TARGET-ctr jaeger-$TARGET-ctr -o wide 2>/dev/null | sed -n 's/.*2000:\([0-9]*\)\/TCP.*/\1/p' | head -1)

    echo "Nodeport: $NODEPORT"
    URL="http://10.10.1.1:$NODEPORT"
    echo "--------------------------------------------------"
    echo

    cd /users/tomislav/DeathStarBench/socialNetwork
    $PYTHON3 /users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/init_social_graph.py --ip 10.10.1.1 --port $NODEPORT
    sleep 60

    # Warmup at the floor of the ramp.
    WARMUP_C=$(( (WARMUP_RPS*WARMUP_RPS + 19999) / 20000 ))
    WARMUP_T=$(( (WARMUP_C + 9) / 10 ))
    RANDOM_SEED=42
    echo "Warmup: $WARMUP_RPS rps for ${WARMUP_DURATION}s (c=$WARMUP_C, t=$WARMUP_T)"
    wrk -t $WARMUP_T -c $WARMUP_C -d ${WARMUP_DURATION}s -L -s $LUA $URL -R $WARMUP_RPS
    sleep 60

    # Ramp
    rps=$START
    while [[ $rps -le $END ]]; do
        C=$(( (rps*rps + 19999) / 20000 ))
        T=$(( (C + 9) / 10 ))
        printf "\n\n"; echo "rps=$rps (c=$C, t=$T):"
        RANDOM_SEED=$((42*iternum + T*rps/STEP))
        wrk -t $T -c $C -d ${DURATION}s -L -s $LUA $URL -R $rps 2>&1 | grep -A 2 -e "Thread Stats" -e "Mean"
        sleep $BREAK
        rps=$((rps + STEP))
    done

    echo
    echo
    echo "--------------------------------------------------"
    echo "--------------------------------------------------"
    echo
    echo
done

cd $THIS_DIR
