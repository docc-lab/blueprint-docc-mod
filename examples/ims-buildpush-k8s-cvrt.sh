#!/bin/bash
set -e

# Default values
registry=""
tag="latest"

# Function to show usage
usage() {
  echo "Usage: $0 [OPTIONS]"
  echo "Options:"
  echo "  --registry=NAME     Docker registry organization/repository name (required)"
  echo "                      (e.g., 'docclabgroup' for images like docclabgroup/service-name)"
  echo "  --tag=TAG           Tag for all images (default: latest)"
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

# Step 1: Build, tag and push images
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
  echo "No container directories with Dockerfiles found! Exiting..."
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

# Step 2: Prepare environment variables and convert docker-compose to K8s manifests
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

# Run kompose convert
echo "Running kompose convert..."
kompose convert

# Step 3: Fix all references and naming conventions in Kubernetes manifests
echo "==== Fixing naming conventions in Kubernetes manifests ===="

# Create a list of patterns to replace (underscores with hyphens)
# Store both original name with underscore and hyphenated version
declare -A name_map
for container in "${CONTAINERS[@]}"; do
  hyphen_name=$(echo $container | tr '_' '-')
  name_map["$container"]="$hyphen_name"
done

# Update all YAML files to replace underscores with hyphens
for yaml_file in *.yaml; do
  echo "Processing $yaml_file..."
  
  # Fix service names in metadata
  sed -i 's/name: "\([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)"/name: "\1-\2"/g' "$yaml_file"
  sed -i 's/name: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_]*\)/name: \1-\2/g' "$yaml_file"
  
  # Fix all hostname fields - multiple passes to catch nested underscores
  sed -i 's/hostname: "\([^"]*\)_\([^"]*\)"/hostname: "\1-\2"/g' "$yaml_file"
  sed -i 's/hostname: \([a-zA-Z0-9]*\)_\([a-zA-Z0-9_-]*\)/hostname: \1-\2/g' "$yaml_file"
  
  # Additional pass to catch any remaining cases with multiple underscores
  sed -i 's/_/-/g' "$yaml_file"
  
  # Fix image references
  for original in "${!name_map[@]}"; do
    hyphen_version=${name_map[$original]}
    # Replace the image reference with the registry version
    sed -i "s|image: ${hyphen_version}|image: ${registry}/${hyphen_version}:${tag}|g" "$yaml_file"
  done
done

# Fix service file names (replace underscores with hyphens)
echo "==== Fixing service file names ===="
for service_file in *_*-service.yaml; do
  if [ -f "$service_file" ]; then
    # New file name with hyphens instead of underscores
    new_name=$(echo "$service_file" | tr '_' '-')
    
    # Rename the file if needed
    if [ "$service_file" != "$new_name" ]; then
      mv "$service_file" "$new_name"
      echo "Renamed $service_file to $new_name"
    fi
  fi
done

echo "==== Process completed successfully ===="
echo "Images built, tagged, and pushed to: $registry organization"
echo "Kubernetes manifests generated and updated"
echo ""
echo "You can now apply the Kubernetes manifests with:"
echo "kubectl apply -f ."
echo ""
echo "Or create a namespace first:"
echo "kubectl create namespace <your-app-name>"
echo "kubectl apply -f . --namespace=<your-app-name> --exclude=docker-compose.yml"
echo "==== END ===="
