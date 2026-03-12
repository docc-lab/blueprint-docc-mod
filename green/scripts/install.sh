#!/bin/bash
set -euo pipefail

########################################
# CONFIG
########################################

NODES=("node-0" "node-1" "node-2" "node-3")

########################################
# INSTALL PERF
########################################

for NODE in "${NODES[@]}"; do
echo "=============================="
echo "Installing perf on $NODE"
echo "=============================="

ssh $NODE << 'EOF'

set -e

echo "[INFO] Updating apt..."
sudo apt-get update -y

echo "[INFO] Installing perf (linux-tools)..."
sudo apt-get install -y \
    linux-tools-common \
    linux-tools-generic \
    linux-tools-$(uname -r)

echo "[INFO] Installing chrony (chronyc)..."
sudo apt-get install -y chrony

echo "[INFO] Enabling + starting chrony..."
sudo systemctl enable --now chrony || sudo systemctl enable --now chronyd

echo "[INFO] Verifying perf installation..."
perf --version || echo "perf installed but not in PATH"

echo "[INFO] Verifying chronyc installation..."
chronyc --version || echo "chronyc installed but not in PATH"

echo "[SUCCESS] perf ready on $(hostname)"

EOF

done

echo ""
echo "All nodes processed."