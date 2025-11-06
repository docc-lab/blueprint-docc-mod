#!/bin/bash

# Script to completely clean up a previous deployment
# This allows you to start fresh before running full-deploy.sh

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Directory variables (relative to script location)
SOCKSHOP_DIR="$SCRIPT_DIR/blueprint-docc-mod/examples/sockshop"

echo "==== Cleanup: Starting Fresh Deployment ===="
echo "[INFO] This script will remove all Kubernetes resources and build artifacts"
echo ""

###### Step 1: Remove Sockshop Kubernetes Resources ######
echo "==== Step 1: Removing Sockshop Kubernetes Resources ===="
echo "[INFO] Deleting all Sockshop services, deployments, and pods..."

# Execute: Delete all sockshop resources
if [ -d "$SOCKSHOP_DIR/build/k8s" ]; then
    cd "$SOCKSHOP_DIR"
    kubectl delete -f build/k8s/ --ignore-not-found=true || echo "[WARNING] Some resources may not exist"
    echo "[INFO] Waiting 30 seconds for resources to be fully deleted..."
    sleep 30
else
    echo "[INFO] No k8s manifests found, skipping deletion"
fi

# Also try to delete common sockshop services individually (in case manifests don't exist)
echo "[INFO] Cleaning up individual sockshop services..."
kubectl delete service,deployment,statefulset -l app=sockshop --ignore-not-found=true || true
kubectl delete service,deployment frontend-ctr --ignore-not-found=true || true
kubectl delete service,deployment jaeger-ctr --ignore-not-found=true || true
kubectl delete service,deployment cart-ctr --ignore-not-found=true || true
kubectl delete service,deployment catalogue-ctr --ignore-not-found=true || true

echo "[SUCCESS] Sockshop resources removed"

###### Step 2: Remove Build Artifacts ######
echo ""
echo "==== Step 2: Removing Build Artifacts ===="
echo "[INFO] Removing build directory and generated artifacts..."

# Execute: Remove build directory
if [ -d "$SOCKSHOP_DIR/build" ]; then
    cd "$SOCKSHOP_DIR"
    sudo rm -rf build
    echo "[SUCCESS] Build directory removed"
else
    echo "[INFO] Build directory does not exist, skipping"
fi

###### Step 3: Clean Registry Images (Optional) ######
echo ""
echo "==== Step 3: Cleaning Registry Images (Optional) ===="
echo "[INFO] Note: This will remove all images from the registry"
read -p "Do you want to clean registry images? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "[INFO] Cleaning registry images..."
    
    # Get registry pod name
    REGISTRY_POD=$(kubectl get pods -n registry -l app=docker-registry -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$REGISTRY_POD" ]; then
        # Delete all images via registry API
        echo "[INFO] Deleting all images from registry..."
        # Note: This requires the registry to have delete enabled (which it does in registry-deployment.yaml)
        # We could curl the registry API to delete, but it's safer to just leave images
        # Or we can delete the PVC to force a fresh start
        read -p "Delete registry PVC to completely reset registry? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            kubectl delete pvc registry-pvc -n registry --ignore-not-found=true
            kubectl delete deployment docker-registry -n registry --ignore-not-found=true
            echo "[INFO] Registry PVC and deployment deleted. They will be recreated by full-deploy.sh"
        fi
    else
        echo "[INFO] Registry pod not found, skipping registry cleanup"
    fi
else
    echo "[INFO] Keeping registry images (they will be reused)"
fi

###### Step 4: Remove Node Labels (Optional) ######
echo ""
echo "==== Step 4: Removing Node Labels (Optional) ===="
read -p "Do you want to remove node labels? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "[INFO] Removing workload labels from nodes..."
    kubectl label nodes node-1 workload- --ignore-not-found=true || true
    kubectl label nodes node-2 workload- --ignore-not-found=true || true
    echo "[SUCCESS] Node labels removed"
else
    echo "[INFO] Keeping node labels (they will be overwritten by add-node-selectors.sh)"
fi

###### Step 5: Verify Cleanup ######
echo ""
echo "==== Step 5: Verifying Cleanup ===="
echo "[INFO] Checking for remaining sockshop resources..."

# Check for remaining pods
REMAINING_PODS=$(kubectl get pods -o json | jq -r '.items[] | select(.metadata.name | contains("sockshop") or contains("frontend") or contains("cart") or contains("catalogue") or contains("jaeger")) | .metadata.name' 2>/dev/null || echo "")

if [ -n "$REMAINING_PODS" ]; then
    echo "[WARNING] Some pods may still exist:"
    echo "$REMAINING_PODS"
    echo "[INFO] You may need to wait a bit longer or delete them manually"
else
    echo "[SUCCESS] No sockshop pods found"
fi

echo ""
echo "==== Cleanup Completed ===="
echo "[INFO] You can now run: ./full-deploy.sh"
echo ""
echo "Summary of cleanup:"
echo "- Sockshop Kubernetes resources: Deleted"
echo "- Build artifacts: Removed"
echo "- Registry images: $([ -n "$REGISTRY_POD" ] && echo "Kept (or deleted if you chose to)" || echo "N/A")"
echo "- Node labels: $([ "$REPLY" =~ ^[Yy]$ ] && echo "Removed" || echo "Kept")"
echo ""