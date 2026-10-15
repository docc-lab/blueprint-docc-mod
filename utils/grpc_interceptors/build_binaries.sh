#!/usr/bin/env bash
# Compile every service process of a Blueprint build into plain binaries (no docker).
#
# Usage:
#   build_binaries.sh <build dir>
#
# <build dir> is an output directory of build_sn.sh.  For every container <dir>/docker/<ctr>/ that
# holds a Go process <proc>, this runs the same `go build` its Dockerfile runs and writes the
# binary to <build dir>/bin/<proc>.  Containers without Go code (otelcol, jaeger, ...) are skipped.
# Compilation errors are printed and the script exits non-zero.
#
# Example:
#   utils/grpc_interceptors/build_binaries.sh ~/grpc-interceptors-work/after_step1_v_nw
set -euo pipefail

build=$(realpath "${1:?usage: build_binaries.sh <build dir>}")
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH GOTOOLCHAIN=local
mkdir -p "$build/bin"

failed=0
for work in "$build"/docker/*/*/go.work; do
    procdir=$(dirname "$work")          # <ctr>/<proc>, the Go workspace
    proc=$(basename "$procdir")
    if (cd "$procdir" && go build -o "$build/bin/$proc" "./$proc") > "$build/bin/$proc.log" 2>&1; then
        echo "built $proc"
    else
        echo "FAILED $proc:"; sed 's/^/    /' "$build/bin/$proc.log" | head -20
        failed=$((failed + 1))
    fi
done
[ "$failed" -eq 0 ] || { echo "$failed process(es) failed to compile" >&2; exit 1; }
echo "all binaries in $build/bin"
