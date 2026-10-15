#!/usr/bin/env bash
# Compile a DSB SocialNetwork wiring spec with Blueprint (code generation only, no docker images).
#
# Usage:
#   build_sn.sh <spec> <output dir> [bridge kind] [grpc mode]
#
#   spec        e.g. docker_v_es_nw (see examples/dsb_sn/wiring/main.go for the list)
#   output dir  where Blueprint writes the generated code; must not exist yet
#   bridge kind v | pb | cgpb | sb. Sets OT_BRIDGE, which picks the tracing code. Default: taken
#               from the spec name (docker_<kind>_es_nw), the same way utils/build_deploy_dsb.sh does it.
#   grpc mode   interceptor | wrapper. Sets OT_GRPC_MODE: how gRPC services are traced. Default: unset,
#               which means whatever the code's default is (interceptor once that exists).
#
# Example (the Step 1 baseline):
#   utils/grpc_interceptors/build_sn.sh docker_v_es_nw ~/grpc-interceptors-work/after_step1_v_nw
#
# Takes about 100 s.  The full Blueprint log is written to <output dir>.log.
#
# APP=dsb_hotel builds a HotelReservation spec instead (any app under examples/ works).  Note that the
# hotel specs set OT_BRIDGE themselves from the spec name.
set -euo pipefail

spec=${1:?usage: build_sn.sh <spec> <output dir> [bridge kind] [grpc mode]}
out=${2:?usage: build_sn.sh <spec> <output dir> [bridge kind] [grpc mode]}
kind=${3:-$(echo "$spec" | sed -E 's/^docker_//; s/_nw$//; s/_es$//')}
mode=${4:-}

repo=$(cd "$(dirname "$0")/../.." && pwd)
out=$(realpath -m "$out")
[ -e "$out" ] && { echo "refusing to overwrite existing $out" >&2; exit 1; }
mkdir -p "$(dirname "$out")"

export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH GOTOOLCHAIN=local
export OT_BRIDGE=$kind
if [ -n "$mode" ]; then export OT_GRPC_MODE=$mode; else unset OT_GRPC_MODE; fi

echo "building $spec (OT_BRIDGE=$OT_BRIDGE, OT_GRPC_MODE=${OT_GRPC_MODE:-<unset>}) -> $out"
cd "$repo/examples/${APP:-dsb_sn}"
if ! go run wiring/main.go -w "$spec" -o "$out" > "$out.log" 2>&1; then
    tail -20 "$out.log" >&2
    echo "Blueprint failed; full log in $out.log" >&2
    exit 1
fi
# The deploy script runs goimports on the output to drop unused imports; do the same so the
# generated code compiles.
goimports -w "$out/docker"
echo "ok: $out"
