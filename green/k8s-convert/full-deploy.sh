#!/bin/bash

# Script to fully deploy sockshop application to Kubernetes
# This script orchestrates the complete deployment process

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Directory variables (relative to script location)
SOCKSHOP_DIR="$SCRIPT_DIR/../examples/sockshop"
D2K8S_SCRIPT="$SCRIPT_DIR/../d2k8s/d2k8s.py"

echo "==== Full Deployment: Sockshop to Kubernetes ===="
echo ""

# Check prerequisites
if ! command -v kubectl >/dev/null 2>&1; then
  echo "[ERROR] kubectl not found in PATH"
  exit 1
fi

if ! command -v kompose >/dev/null 2>&1; then
  echo "[ERROR] kompose not found in PATH"
  exit 1
fi

if [ ! -f "$D2K8S_SCRIPT" ]; then
  echo "[ERROR] d2k8s.py not found at $D2K8S_SCRIPT"
  exit 1
fi

# Check if docker-compose.yml exists, if not, compile the application
if [ ! -f "$SOCKSHOP_DIR/build/docker/docker-compose.yml" ]; then
  echo "[INFO] docker-compose.yml not found. Compiling application..."
  echo "[INFO] This will generate build/docker/docker-compose.yml"
  
  cd "$SOCKSHOP_DIR"
  
  if [ ! -f "wiring/main.go" ]; then
    echo "[ERROR] wiring/main.go not found. Cannot compile application."
    exit 1
  fi
  
  echo "[INFO] Running: go run wiring/main.go -o build -w docker"
  go run wiring/main.go -o build -w docker
  
  if [ $? -ne 0 ]; then
    echo "[ERROR] Failed to compile application"
    exit 1
  fi
  
  if [ ! -f "build/docker/docker-compose.yml" ]; then
    echo "[ERROR] Compilation succeeded but docker-compose.yml not found"
    exit 1
  fi
  
  echo "[SUCCESS] Application compiled successfully"
  echo ""
fi

###### Step 0: Ensure Local Docker Registry is Running ######
echo "==== Step 0: Ensuring Local Docker Registry is Available ===="

# Check for Docker container registry
DOCKER_REGISTRY=$(docker ps --format "{{.Names}}" | grep -i registry | grep -v k8s_POD | head -1 || echo "")

if [ -z "$DOCKER_REGISTRY" ]; then
  echo "[INFO] Docker container registry not found. Starting local registry..."
  
  # Check if registry image exists
  if ! docker images | grep -q "^registry[[:space:]]*2"; then
    echo "[INFO] Pulling registry:2 image..."
    docker pull registry:2
  fi
  
  # Start the registry container
  echo "[INFO] Starting Docker registry container on port 5000..."
  docker run -d \
    --name local-registry \
    --restart=unless-stopped \
    -p 5000:5000 \
    -v registry-data:/var/lib/registry \
    registry:2
  
  if [ $? -eq 0 ]; then
    echo "[SUCCESS] Docker registry container started"
    # Wait a moment for registry to be ready
    sleep 2
  else
    echo "[ERROR] Failed to start Docker registry container"
    exit 1
  fi
else
  echo "[INFO] Docker container registry already running: $DOCKER_REGISTRY"
fi

# Verify registry is accessible
echo "[INFO] Verifying registry is accessible..."
if curl -s -f http://10.10.1.1:5000/v2/ >/dev/null 2>&1; then
  echo "[SUCCESS] Registry is accessible at 10.10.1.1:5000"
else
  echo "[WARNING] Registry may not be accessible yet. Continuing anyway..."
fi

# Use local Docker registry (port 5000)
REGISTRY_URL=${REGISTRY_URL:-"10.10.1.1:5000"}

echo "[INFO] Using registry: $REGISTRY_URL"
echo ""

###### Step 1: Convert Docker Compose to Kubernetes Manifests ######
echo "==== Step 1: Converting Docker Compose to Kubernetes ===="
echo "[INFO] Running d2k8s.py to generate Kubernetes manifests..."

cd "$SOCKSHOP_DIR/build"

# Source environment variables if .env file exists
if [ -f "docker/.env" ]; then
  echo "[INFO] Loading environment variables from docker/.env"
  set -a
  . docker/.env
  set +a
fi

# Check if registry has images (if empty, we need to build)
REGISTRY_IMAGES=$(curl -s http://10.10.1.1:5000/v2/_catalog 2>/dev/null | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data.get('repositories', [])))" 2>/dev/null || echo "0")

# Determine if we should skip build
# If registry is empty or SKIP_BUILD is not set, we should build
SKIP_BUILD_FLAG=""
if [ "$REGISTRY_IMAGES" -gt 0 ] && [ "${SKIP_BUILD:-}" != "false" ]; then
  echo "[INFO] Registry has $REGISTRY_IMAGES images. Skipping build (set SKIP_BUILD=false to rebuild)"
  SKIP_BUILD_FLAG="--skip-build"
else
  echo "[INFO] Registry is empty or SKIP_BUILD=false. Will build and push images..."
fi

# Run d2k8s.py to convert and build/push images
python3 "$D2K8S_SCRIPT" \
  docker/docker-compose.yml \
  k8s \
  --registry "$REGISTRY_URL" \
  $SKIP_BUILD_FLAG

if [ $? -ne 0 ]; then
  echo "[ERROR] Failed to convert docker-compose to Kubernetes manifests"
  exit 1
fi

echo "[SUCCESS] Kubernetes manifests generated in build/k8s/"
echo ""

###### Step 2: Add Node Selectors ######
echo "==== Step 2: Adding Node Selectors ===="
echo "[INFO] Adding node selectors to deployments..."

"$SCRIPT_DIR/add-node-selectors.sh"

if [ $? -ne 0 ]; then
  echo "[ERROR] Failed to add node selectors"
  exit 1
fi

echo "[SUCCESS] Node selectors added"
echo ""

###### Step 3: Deploy to Kubernetes ######
echo "==== Step 3: Deploying to Kubernetes ===="
echo "[INFO] Applying Kubernetes manifests..."

kubectl apply -f k8s/

if [ $? -ne 0 ]; then
  echo "[ERROR] Failed to deploy to Kubernetes"
  exit 1
fi

echo "[SUCCESS] Resources deployed to Kubernetes"
echo ""

###### Step 4: Convert Services to NodePort ######
echo "==== Step 4: Converting Services to NodePort ===="
echo "[INFO] Converting frontend and jaeger services to NodePort for external access..."

# Convert frontend to NodePort if not already
FRONTEND_TYPE=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.type}' 2>/dev/null || echo "")
if [ "$FRONTEND_TYPE" != "NodePort" ]; then
  echo "[INFO] Converting frontend-ctr service to NodePort..."
  kubectl patch service frontend-ctr -p '{"spec":{"type":"NodePort"}}'
  sleep 1
fi

# Convert jaeger to NodePort if it exists and not already NodePort
JAEGER_EXISTS=$(kubectl get svc jaeger-ctr >/dev/null 2>&1 && echo "yes" || echo "no")
if [ "$JAEGER_EXISTS" = "yes" ]; then
  JAEGER_TYPE=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.type}' 2>/dev/null || echo "")
  if [ "$JAEGER_TYPE" != "NodePort" ]; then
    echo "[INFO] Converting jaeger-ctr service to NodePort..."
    kubectl patch service jaeger-ctr -p '{"spec":{"type":"NodePort"}}'
    sleep 1
  fi
fi

echo "[SUCCESS] Services converted to NodePort"
echo ""

###### Step 5: Wait for Pods to be Ready ######
echo "==== Step 5: Waiting for Pods to be Ready ===="
echo "[INFO] Waiting up to 5 minutes for all pods to be ready..."

kubectl wait --for=condition=ready pod \
  -l 'io.kompose.service in (frontend-ctr,cart-ctr,catalogue-ctr,order-ctr,payment-ctr,shipping-ctr,user-ctr,jaeger-ctr)' \
  --timeout=300s \
  || echo "[WARNING] Some pods may not be ready yet. Check with: kubectl get pods"

echo ""

###### Step 6: Show Access URLs ######
echo "==== Step 6: Access Information ===="
echo ""

"$SCRIPT_DIR/access-frontend-laptop.sh"

echo ""
echo "==== Deployment Completed ===="
echo ""
echo "To check pod status:"
echo "  kubectl get pods -o wide"
echo ""
echo "To check services:"
echo "  kubectl get svc"
echo ""
echo "To view logs:"
echo "  kubectl logs -f <pod-name>"
echo ""

