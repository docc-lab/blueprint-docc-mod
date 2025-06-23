#!/bin/bash

set -e

# Get the directory where this script is located, regardless of where it's called from
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

# Function to detect Kubernetes cluster type
detect_cluster_type() {
    if command -v minikube &> /dev/null && minikube status &> /dev/null; then
        echo "minikube"
    elif command -v docker &> /dev/null && docker info &> /dev/null; then
        echo "docker"
    elif command -v kind &> /dev/null && kind get clusters &> /dev/null; then
        echo "kind"
    else
        echo "unknown"
    fi
}

detect_os() {
    unameOut="$(uname -s)"
    case "${unameOut}" in
        Linux*)     os=Linux;;
        Darwin*)    os=Mac;;
        *)          os="UNKNOWN:${unameOut}"
    esac
    echo $os
}

OS=$(detect_os)
CLUSTER_TYPE=$(detect_cluster_type)
export K8S_CLUSTER_TYPE=$CLUSTER_TYPE

# Default kompose binary
KOMPOSE_BIN="kompose"

# Parse command-line arguments for registry host/port and --use-tmp
while [[ $# -gt 0 ]]; do
  case $1 in
    --registry-host=*)
      REGISTRY_HOST="${1#*=}"
      shift
      ;;
    --registry-port=*)
      REGISTRY_PORT="${1#*=}"
      shift
      ;;
    --use-tmp)
      KOMPOSE_BIN="/tmp/kompose"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

# Fall back to environment variables if not set
REGISTRY_HOST="${REGISTRY_HOST:-$REGISTRY_HOST}"
REGISTRY_PORT="${REGISTRY_PORT:-$REGISTRY_PORT}"

# If still not set, error out
if [ -z "$REGISTRY_HOST" ] || [ -z "$REGISTRY_PORT" ]; then
  echo "Error: REGISTRY_HOST and REGISTRY_PORT must be set via command-line or environment variables."
  echo "Usage: $0 --registry-host=localhost --registry-port=5000 [--use-tmp]"
  exit 1
fi

# If using Minikube defaults, automate registry setup
if [ "$REGISTRY_HOST" = "localhost" ] && [ "$REGISTRY_PORT" = "5000" ]; then
  echo "Enabling Minikube registry addon..."
  minikube addons enable registry
  # Detect the actual registry port used by Minikube
  REGISTRY_PORT=$(kubectl get svc registry -n kube-system -o jsonpath='{.spec.ports[0].nodePort}')
  if [ -z "$REGISTRY_PORT" ]; then
    echo "Error: Could not detect Minikube registry port. Please ensure the registry addon is enabled."
    exit 1
  else
    echo "Detected Minikube registry port: $REGISTRY_PORT"
  fi
  echo "Port-forwarding registry to localhost:$REGISTRY_PORT..."
  kubectl port-forward --namespace kube-system service/registry $REGISTRY_PORT:5000 &
  PF_PID=$!
  sleep 3  # Give port-forward time to start
fi

export REGISTRY_HOST
export REGISTRY_PORT
export KOMPOSE_BIN

# Purge all Kubernetes resources in the default namespace, except the registry service
# Delete StatefulSets first to release PVCs
kubectl delete statefulset --all --ignore-not-found=true || true
# Delete ConfigMaps and Secrets
kubectl delete configmap --all --ignore-not-found=true || true
kubectl delete secret --all --ignore-not-found=true || true
# Then delete all other resources except the registry service
kubectl delete all --all --ignore-not-found=true --field-selector=metadata.name!=registry || true
# Then delete PVCs
kubectl delete pvc --all || true

# Extract and pre-pull external images from docker-compose.yml
pre_pull_external_images() {
  echo "Extracting and pre-pulling external images from docker-compose.yml..."
  
  # Extract image names from docker-compose.yml using grep and sed
  # This looks for lines with 'image:' and extracts the image name
  local external_images=$(grep -E '^[[:space:]]*image:' docker-compose.yml | sed -E 's/^[[:space:]]*image:[[:space:]]*//')
  
  for img in $external_images; do
    echo "Pulling $img..."
    docker pull "$img"
    
    # Tag the image for the local registry
    local tagged_img="${REGISTRY_HOST}:${REGISTRY_PORT}/${img}"
    echo "Tagging $img as $tagged_img..."
    docker tag "$img" "$tagged_img"
    
    # Push the tagged image to the local registry
    echo "Pushing $tagged_img to the local registry..."
    docker push "$tagged_img"
  done
  echo "Pre-pull and pre-push complete."
}

# Call the pre-pull function before running the pipeline
pre_pull_external_images

# Run the k8s-convert tool as a Python script
echo "Running k8s-convert tool..."
set -a
source .env
set +a
PYTHONPATH="${SCRIPT_DIR}" KOMPOSE_BIN="$KOMPOSE_BIN" python3 -m k8s_convert.main . --output-dir ../k8s --registry "${REGISTRY_HOST}:${REGISTRY_PORT}" --push-images --use-kompose

# Deploy the generated manifests
echo "Deploying Kubernetes manifests..."
kubectl apply -f ../k8s/

# Post-process the generated manifests to replace underscores with hyphens
# for yaml_file in ../k8s/*.yaml; do
#   echo "Processing $yaml_file..."
#   sed -i '' 's/name: "\([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)"/name: "\1-\2"/g' "$yaml_file"
#   sed -i '' 's/name: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)/name: \1-\2/g' "$yaml_file"
#   sed -i '' 's/hostname: "\([^"]*\)_\([^"]*\)"/hostname: "\1-\2"/g' "$yaml_file"
#   sed -i '' 's/hostname: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_-]*\)/hostname: \1-\2/g' "$yaml_file"
#   sed -i '' 's/metadata:\n  name: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)/metadata:\n  name: \1-\2/g' "$yaml_file"
#   sed -i '' 's/_/-/g' "$yaml_file"
# done
# Update all YAML files to replace underscores with hyphens
for yaml_file in ../k8s/*.yaml; do
  echo "Processing $yaml_file..."
  
  # Fix service names in metadata
  sed -i '' 's/name: "\([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)"/name: "\1-\2"/g' "$yaml_file"
  sed -i '' 's/name: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)/name: \1-\2/g' "$yaml_file"
  
  # Fix all hostname fields - multiple passes to catch nested underscores
  sed -i '' 's/hostname: "\([^"]*\)_\([^"]*\)"/hostname: "\1-\2"/g' "$yaml_file"
  sed -i '' 's/hostname: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_-]*\)/hostname: \1-\2/g' "$yaml_file"
  
  # Additional pass to catch any remaining cases with multiple underscores
  sed -i '' 's/_/-/g' "$yaml_file"
  
  # # Fix image references
  # for container in "${CONTAINERS[@]}"; do
  #   hyphen_name=$(echo $container | tr '_' '-')
  #   # Replace the image reference with the registry version
  #   sed -i '' "s|image: ${hyphen_name}|image: ${registry}/${hyphen_name}:${tag}|g" "$yaml_file"
  # done
done

# Reapply the processed manifests
kubectl apply -f ../k8s/

# Clean up port-forward process if started
if [ ! -z "$PF_PID" ]; then
  echo "Cleaning up registry port-forward..."
  kill $PF_PID
fi

echo "Pipeline completed successfully!"
