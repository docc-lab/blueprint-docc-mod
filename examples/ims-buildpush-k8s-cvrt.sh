#!/bin/bash
set -e

# Default values
registry=""
tag="latest"

# Function to show usage
usage() {
  echo "Usage: $0 [OPTIONS]"
  echo "Options:"
  echo "  --registry=URL      Remote Docker registry URL (required)"
  echo "  --tag=TAG           Tag for all images (default: latest)"
  echo ""
  echo "Note: This script assumes you have already logged in to your Docker registry."
  echo "Please run 'docker login <your-registry>' before running this script if needed."
  echo ""
  echo "Example: $0 --registry=myregistry.com/repo --tag=v1.0"
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

# Trim trailing slash from registry if present
registry=${registry%/}


# Step 1: Build, tag and push images
echo "==== Building, tagging and pushing images ===="

# Dynamically find container directories
CONTAINERS=()
for dir in */; do
  dir=${dir%/} 
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

# Step 2: Load environment variables and convert docker-compose to K8s manifests
echo "==== Converting docker-compose to Kubernetes manifests ===="

# Check if need to copy environment variables from parent directory
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

# Step 3: Update image references in all Kubernetes manifest files
echo "==== Updating image references in Kubernetes manifests ===="

# First handle the deployment files
for container in "${CONTAINERS[@]}"; do
  # Convert underscores to hyphens for file naming
  hyphen_name=$(echo $container | tr '_' '-')
  deployment_file="${hyphen_name}-deployment.yaml"
  
  if [ -f "$deployment_file" ]; then
    echo "Updating $deployment_file..."
    
    # Replace image reference
    sed -i "s|image: ${hyphen_name}|image: ${registry}/${hyphen_name}:${tag}|g" "$deployment_file"
  else
    echo "Warning: $deployment_file not found"
  fi
done

# Fix service file names (replace underscores with hyphens)
echo "==== Fixing service file names and contents ===="
for service_file in *_*-service.yaml; do
  if [ -f "$service_file" ]; then
    # New file name with hyphens instead of underscores
    new_name=$(echo "$service_file" | tr '_' '-')
    
    # First fix the content (replace underscores with hyphens in the file content)
    sed -i 's/_/-/g' "$service_file"
    
    # Then rename the file if needed
    if [ "$service_file" != "$new_name" ]; then
      mv "$service_file" "$new_name"
      echo "Renamed $service_file to $new_name"
    fi
  fi
done

echo "==== Process completed successfully ===="
echo "Images built, tagged, and pushed to: $registry"
echo "Kubernetes manifests generated and updated"
echo ""
echo "You can now apply the Kubernetes manifests with:"
echo "kubectl apply -f . --exclude=docker-compose.yml"
echo ""
echo "Or create a namespace first:"
echo "kubectl create namespace sockshop"
echo "kubectl apply -f . --namespace=<app-name> --exclude=docker-compose.yml"
