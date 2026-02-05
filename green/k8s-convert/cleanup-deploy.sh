#!/bin/bash

# Script to completely clean up a previous deployment
# This allows you to start fresh before running full-deploy.sh

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Directory variables (relative to script location - go up one level from k8s-convert)
SOCKSHOP_DIR="$SCRIPT_DIR/../examples/sockshop"

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
# Delete all sockshop services and deployments
for svc in frontend-ctr jaeger-ctr cart-ctr cart-db-ctr catalogue-ctr catalogue-db-ctr order-ctr order-db-ctr payment-ctr shipping-ctr shipping-db-ctr user-ctr user-db-ctr; do
    kubectl delete service,deployment $svc --ignore-not-found=true || true
done

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

###### Step 3: Clean Local Docker Registry ######
echo ""
echo "==== Step 3: Cleaning Local Docker Registry ===="
echo "[INFO] Cleaning registry images and container..."

# Check for Docker container registry
DOCKER_REGISTRY=$(docker ps --format "{{.Names}}" | grep -i registry | grep -v k8s_POD | head -1 || echo "")

if [ -n "$DOCKER_REGISTRY" ]; then
    echo "[INFO] Docker container registry found: $DOCKER_REGISTRY"
    echo "[INFO] Stopping and removing Docker registry container..."
    
    # Stop the container
    docker stop $DOCKER_REGISTRY 2>/dev/null || true
    
    # Remove the container
    docker rm $DOCKER_REGISTRY 2>/dev/null || true
    
    # Remove associated volumes (this clears all registry data)
    echo "[INFO] Removing registry volumes to clear all images..."
    docker volume ls | grep -i registry | awk '{print $2}' | xargs -r docker volume rm 2>/dev/null || true
    
    echo "[SUCCESS] Docker registry container and data cleaned"
else
    echo "[INFO] No Docker registry container found, skipping registry cleanup"
fi

# Also clean up any Kubernetes registry if it exists (optional cleanup)
REGISTRY_POD=$(kubectl get pods -n registry -l app=docker-registry -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$REGISTRY_POD" ]; then
    echo "[INFO] Kubernetes registry also found, cleaning it up..."
    kubectl delete service docker-registry -n registry --ignore-not-found=true || true
    kubectl delete deployment docker-registry -n registry --ignore-not-found=true || true
    kubectl delete pvc registry-pvc -n registry --ignore-not-found=true || true
    echo "[SUCCESS] Kubernetes registry cleaned"
fi

###### Step 4: Remove Node Labels ######
echo ""
echo "==== Step 4: Removing Node Labels ===="
echo "[INFO] Removing workload labels from nodes..."

# Remove labels from node-2 (jaeger) and node-3 (sockshop) - matching add-node-selectors.sh
kubectl label nodes node-2 workload- --ignore-not-found=true || true
kubectl label nodes node-3 workload- --ignore-not-found=true || true

echo "[SUCCESS] Node labels removed"

###### Step 5: Verify Cleanup ######
echo ""
echo "==== Step 5: Verifying Cleanup ===="
echo "[INFO] Checking for remaining sockshop resources..."

# Check for remaining pods (include all sockshop services)
REMAINING_PODS=$(kubectl get pods -o json | jq -r '.items[] | select(.metadata.name | contains("sockshop") or contains("frontend") or contains("cart") or contains("catalogue") or contains("order") or contains("payment") or contains("shipping") or contains("user") or contains("jaeger")) | .metadata.name' 2>/dev/null || echo "")

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
echo "- Registry: Cleaned (all images and resources removed)"
echo "- Node labels: Removed"
echo ""