#!/bin/bash
set -euo pipefail

echo "[Step 1] Disabling swap"
sudo swapoff -a
sudo sed -i '/ swap / s/^/#/' /etc/fstab

echo "[Step 2] Loading kernel modules"
sudo modprobe overlay
sudo modprobe br_netfilter

cat <<EOF | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF

echo "[Step 3] Applying sysctl settings"
cat <<EOF | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-ip6tables = 1
net.bridge.bridge-nf-call-iptables = 1
net.ipv4.ip_forward = 1
EOF
sudo sysctl --system

echo "[Step 4] Installing containerd"
sudo apt update && sudo apt install -y containerd
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo systemctl restart containerd
sudo systemctl enable containerd

echo "[Step 5] Installing Kubernetes components"
sudo apt install -y curl apt-transport-https ca-certificates gnupg lsb-release

curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.30/deb/Release.key | sudo tee /etc/apt/keyrings/kubernetes-apt-keyring.asc
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.asc] https://pkgs.k8s.io/core:/stable:/v1.30/deb/ /" | sudo tee /etc/apt/sources.list.d/kubernetes.list

sudo apt update
sudo apt install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

echo "[Step 6] Enabling services"
sudo systemctl enable kubelet
