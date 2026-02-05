#!/bin/bash
set -euo pipefail

NODES=("node-0" "node-1" "node-2" "node-3")

for node in "${NODES[@]}"; do
  echo "==== Installing perf on $node ===="
  ssh "$node" 'set -e; sudo apt-get update && sudo apt-get install -y linux-tools-common linux-tools-$(uname -r)'
  ssh "$node" 'perf --version'
  echo
done

echo "==== Done. perf installed on all nodes. ===="