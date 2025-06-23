#!/bin/bash
# set -e
set -x

# Default values
registry=""
tag="latest"

# Default kompose binary
KOMPOSE_BIN="kompose"

# Function to show usage
usage() {
  echo "Usage: $0 [OPTIONS]"
  echo "Options:"
  echo "  --registry=NAME     Docker registry organization/repository name (required)"
  echo "                      (e.g., 'docclabgroup' for images like docclabgroup/service-name)"
  echo "  --tag=TAG           Tag for all images (default: latest)"
  echo "  --use-tmp-kompose   Use /tmp/kompose for kompose convert"
  echo ""
  echo "Note: This script assumes you have already logged in to your Docker registry."
  echo "Please run 'docker login' before running this script if needed."
  echo ""
  echo "Example: $0 --registry=docclabgroup --tag=v1.0"
  exit 1
}

# Parse arguments
for arg in "$@"; do
  case $arg in
    --registry=*)
      registry="${arg#*=}"
      ;;
    --tag=*)
      tag="${arg#*=}"
      ;;
    --use-tmp-kompose)
      KOMPOSE_BIN="/tmp/kompose"
      shift
      ;;
    --help)
      usage
      ;;
    *)
      echo "Unknown option: $arg"
      usage
      ;;
  esac
done

# Validate required parameters
if [ -z "$registry" ]; then
  echo "Error: --registry parameter is required"
  usage
fi

# Remove trailing slash if present
registry=${registry%/}

echo "==== Building, tagging and pushing images ===="

# Dynamically find container directories (directories that contain a Dockerfile)
CONTAINERS=()
for dir in */; do
  dir=${dir%/}  # Remove trailing slash
  if [ -f "$dir/Dockerfile" ]; then
    CONTAINERS+=("$dir")
    echo "Found container directory: $dir"
  fi
done

if [ ${#CONTAINERS[@]} -eq 0 ]; then
  echo "ERROR: No container directories with Dockerfiles found! Exiting..."
  exit 1
fi

# Build and push each container
for container in "${CONTAINERS[@]}"; do
  echo "Processing $container..."
  
  # Convert underscores to hyphens for image naming
  image_name=$(echo $container | tr '_' '-')
  
  # Build the image
  echo "  Building $container..."
  docker build -t $image_name:$tag $container
  
  # Tag with registry
  full_image_name="$registry/$image_name:$tag"
  echo "  Tagging as $full_image_name..."
  docker tag $image_name:$tag $full_image_name
  
  # Push to registry
  echo "  Pushing to registry..."
  docker push $full_image_name
  
  echo "  Done with $container"
done

echo "==== Preparing environment variables and converting docker-compose to Kubernetes manifests ===="

# First check if need to copy environment variables from parent directory
if [ ! -f ".env" ] && [ -f "../.local.env" ]; then
  echo "Copying ../.local.env to ./.env..."
  cp "../.local.env" "./.env"
fi

# Export environment variables
if [ -f ".env" ]; then
  echo "Loading environment variables from .env file..."
  export $(cat .env | xargs)
else
  echo "Warning: No .env file found, kompose conversion might fail if environment variables are needed."
fi

# Create k8s directory if it doesn't exist
echo "Creating k8s directory..."
mkdir -p k8s

# Run kompose convert with output directory
echo "Running kompose convert..."
$KOMPOSE_BIN convert -f docker-compose.yml -o k8s

echo "==== Fixing naming conventions in Kubernetes manifests ===="

# Update all YAML files to replace underscores with hyphens
for yaml_file in k8s/*.yaml; do
  echo "Processing $yaml_file..."
  
  # Fix service names in metadata
  sudo sed -i 's/name: "\([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)"/name: "\1-\2"/g' "$yaml_file"
  sudo sed -i 's/name: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)/name: \1-\2/g' "$yaml_file"
  
  # Fix all hostname fields - multiple passes to catch nested underscores
  sudo sed -i 's/hostname: "\([^"]*\)_\([^"]*\)"/hostname: "\1-\2"/g' "$yaml_file"
  sudo sed -i 's/hostname: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_-]*\)/hostname: \1-\2/g' "$yaml_file"
  
  # Additional pass to catch any remaining cases with multiple underscores
  sudo sed -i 's/_/-/g' "$yaml_file"
  
  # Fix image references
  for container in "${CONTAINERS[@]}"; do
    hyphen_name=$(echo $container | tr '_' '-')
    # Replace the image reference with the registry version
    sudo sed -i "s|image: ${hyphen_name}|image: ${registry}/${hyphen_name}:${tag}|g" "$yaml_file"
  done
done

# Fix service file names (replace underscores with hyphens)
echo "==== Starting service file renaming phase ===="
for service_file in k8s/*_*-service.yaml; do
  if [ -f "$service_file" ]; then
    # New file name with hyphens instead of underscores
    new_name=$(echo "$service_file" | tr '_' '-')
    
    # Rename the file if needed
    if [ "$service_file" != "$new_name" ]; then
      sudo mv "$service_file" "$new_name"
      echo "Renamed $service_file to $new_name"
    fi
  fi
done

echo "==== Process completed successfully ===="
echo "Images built, tagged, and pushed to: $registry organization"
echo "Kubernetes manifests generated and updated in k8s/ directory"
echo ""
echo "You can now apply the Kubernetes manifests with:"
echo "kubectl apply -f k8s/"
echo ""
echo "Or create a namespace first:"
echo "kubectl create namespace <your-app-name>"
echo "kubectl apply -f k8s/ --namespace=<your-app-name>"
echo "==== END ===="
