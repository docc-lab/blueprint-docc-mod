#!/usr/bin/env bash
# Diff the generated code of two Blueprint builds (e.g. before and after a plugin change).
#
# Usage:
#   codegen_diff.sh <build dir A> <build dir B>
#
# Compares the generated Go and .proto files of every service process under <dir>/docker.  Files
# that are copied verbatim from the repo (the runtime module and the workflow module) are skipped,
# since they are not generated.  Prints a unified diff, then a summary of which files differ.
#
# Example (Step 1 regression check):
#   utils/grpc_interceptors/codegen_diff.sh ~/grpc-interceptors-work/baseline_v_nw \
#                                           ~/grpc-interceptors-work/after_step1_v_nw
set -euo pipefail

a=${1:?usage: codegen_diff.sh <build dir A> <build dir B>}
b=${2:?usage: codegen_diff.sh <build dir A> <build dir B>}

# Only the per-process generated code; runtime/ and workflow/ are copies of repo modules.
filter() {
    (cd "$1/docker" && find . -type f \( -name '*.go' -o -name '*.proto' \) \
        -not -path '*/runtime/*' -not -path '*/workflow/*' -not -name '*.pb.go' | sort)
}

diff <(filter "$a") <(filter "$b") > /dev/null || {
    echo "the two builds generate different sets of files:"
    diff <(filter "$a") <(filter "$b") || true
}

changed=0
while read -r f; do
    [ -f "$b/docker/$f" ] || continue
    if ! cmp -s "$a/docker/$f" "$b/docker/$f"; then
        diff -u "$a/docker/$f" "$b/docker/$f" --label "A/$f" --label "B/$f" || true
        changed=$((changed + 1))
    fi
done < <(filter "$a")

echo
echo "files that differ: $changed"
