#!/usr/bin/env python3

import os
import sys
import yaml
import subprocess
import re
from pathlib import Path
import glob
import argparse

def parse_docker_compose(docker_compose_path):
    """Parse docker-compose file to get service definitions."""
    with open(docker_compose_path, 'r') as f:
        compose_data = yaml.safe_load(f)
    return compose_data.get('services', {})

def run_kompose(docker_compose_path, output_dir=None):
    """Run kompose convert on the docker-compose file."""
    original_dir = os.getcwd()
    try:
        abs_path = os.path.abspath(docker_compose_path)
        abs_output_dir = os.path.abspath(output_dir) if output_dir else None
        print(f"[DEBUG] CWD before Kompose: {os.getcwd()}")
        print(f"[DEBUG] Docker Compose path: {abs_path}")
        os.chdir(os.path.dirname(abs_path))
        print(f"[DEBUG] CWD after chdir: {os.getcwd()}")
        cmd = ['kompose', 'convert', '-f', os.path.basename(abs_path)]
        if output_dir:
            os.makedirs(abs_output_dir, exist_ok=True)
            cmd.extend(['-o', abs_output_dir])
        print(f"[DEBUG] Running Kompose command: {' '.join(cmd)}")
        result = subprocess.run(cmd, capture_output=True, text=True)
        print(f"[DEBUG] Kompose stdout: {result.stdout}")
        if result.stderr:
            print(f"[DEBUG] Kompose stderr: {result.stderr}", file=sys.stderr)
        if result.returncode != 0:
            print(f"Error running kompose: {result.stderr}", file=sys.stderr)
            sys.exit(1)
        return result.stdout
    except Exception as e:
        print(f"[DEBUG] Exception in run_kompose: {e}", file=sys.stderr)
        sys.exit(1)
    finally:
        os.chdir(original_dir)
        print(f"[DEBUG] CWD in finally: {os.getcwd()}")

def update_image_references(output_dir, registry_url):
    """Update image references in Kubernetes manifests to use the specified registry."""
    for yaml_file in Path(output_dir).glob('*.yaml'):
        if not yaml_file.name.endswith('-deployment.yaml'):
            continue
            
        print(f"[DEBUG] Updating image references in {yaml_file}")
        with open(yaml_file, 'r') as f:
            content = yaml.safe_load(f)
        
        # Update image in container spec
        if 'spec' in content and 'template' in content['spec']:
            template = content['spec']['template']
            if 'spec' in template and 'containers' in template['spec']:
                for container in template['spec']['containers']:
                    if 'image' in container:
                        # Get the original image name
                        original_image = container['image']
                        # Only update if not already using the registry
                        if not original_image.startswith(f"{registry_url}/"):
                            # Use the same logic as build_and_push_images
                            if ':' in original_image:
                                # Image has a tag, split on the last colon to separate name from tag
                                image_name_with_namespace, original_tag = original_image.rsplit(':', 1)
                                # Replace slashes with dashes and underscores with dashes in the image name
                                image_name = image_name_with_namespace.replace('/', '-').replace('_', '-')
                                # Use the original tag
                                new_image = f"{registry_url}/{image_name}:{original_tag}"
                            else:
                                # No tag specified, replace slashes and underscores with dashes, use latest
                                image_name = original_image.replace('/', '-').replace('_', '-')
                                new_image = f"{registry_url}/{image_name}:latest"
                            
                            print(f"[DEBUG] Updating image {original_image} -> {new_image}")
                            container['image'] = new_image
        
        # Write back the updated content
        with open(yaml_file, 'w') as f:
            yaml.dump(content, f, default_flow_style=False)

def build_and_push_images(services, registry_url, docker_compose_dir):
    """Build and push Docker images for all services, including pulling and retagging official images."""
    original_dir = os.getcwd()
    try:
        os.chdir(docker_compose_dir)
        for service_name, service_config in services.items():
            # If build context is specified, build and push as before
            if 'build' in service_config:
                build_context = service_config['build']
                if isinstance(build_context, dict):
                    context = build_context.get('context', '.')
                else:
                    context = build_context

                image_name = service_config.get('image', service_name)
                # Use only the last part after the last slash, replace underscores with dashes
                image_name = image_name.split('/')[-1].replace('_', '-')
                full_image_name = f"{registry_url}/{image_name}:latest"

                print(f"[INFO] Building image for {service_name}")
                print(f"[DEBUG] Build context: {context}")
                print(f"[DEBUG] Image name: {full_image_name}")

                build_cmd = ['docker', 'build', '-t', full_image_name, context]
                result = subprocess.run(build_cmd, capture_output=True, text=True)
                if result.returncode != 0:
                    print(f"[ERROR] Failed to build {service_name}: {result.stderr}")
                    continue
                print(f"[INFO] Successfully built {full_image_name}")

                print(f"[INFO] Pushing {full_image_name}")
                push_cmd = ['docker', 'push', full_image_name]
                result = subprocess.run(push_cmd, capture_output=True, text=True)
                if result.returncode != 0:
                    print(f"[ERROR] Failed to push {service_name}: {result.stderr}")
                    continue
                print(f"[INFO] Successfully pushed {full_image_name}")
            # If no build context but an image is specified, pull, tag, and push
            elif 'image' in service_config:
                original_image = service_config['image']
                # Only retag/push if not already using the registry
                if not original_image.startswith(f"{registry_url}/"):
                    # Extract the image name and tag properly
                    # Handle cases like "jaegertracing/all-in-one:latest"
                    
                    # Check if there's already a tag
                    if ':' in original_image:
                        # Image has a tag, split on the last colon to separate name from tag
                        image_name_with_namespace, original_tag = original_image.rsplit(':', 1)
                        # Replace slashes with dashes and underscores with dashes in the image name
                        image_name = image_name_with_namespace.replace('/', '-').replace('_', '-')
                        # Use the original tag
                        full_image_name = f"{registry_url}/{image_name}:{original_tag}"
                    else:
                        # No tag specified, replace slashes and underscores with dashes, use latest
                        image_name = original_image.replace('/', '-').replace('_', '-')
                        full_image_name = f"{registry_url}/{image_name}:latest"
                    
                    print(f"[INFO] Pulling official image for {service_name}: {original_image}")
                    pull_cmd = ['docker', 'pull', original_image]
                    result = subprocess.run(pull_cmd, capture_output=True, text=True)
                    if result.returncode != 0:
                        print(f"[ERROR] Failed to pull {original_image}: {result.stderr}")
                        continue
                    print(f"[INFO] Tagging {original_image} as {full_image_name}")
                    tag_cmd = ['docker', 'tag', original_image, full_image_name]
                    result = subprocess.run(tag_cmd, capture_output=True, text=True)
                    if result.returncode != 0:
                        print(f"[ERROR] Failed to tag {original_image}: {result.stderr}")
                        continue
                    print(f"[INFO] Pushing {full_image_name}")
                    push_cmd = ['docker', 'push', full_image_name]
                    result = subprocess.run(push_cmd, capture_output=True, text=True)
                    if result.returncode != 0:
                        print(f"[ERROR] Failed to push {full_image_name}: {result.stderr}")
                        continue
                    print(f"[INFO] Successfully pushed {full_image_name}")
                else:
                    print(f"[INFO] Skipping {service_name} - image already uses registry prefix")
            else:
                print(f"[INFO] Skipping {service_name} - no build context or image specified")
    finally:
        os.chdir(original_dir)

def convert_underscores_to_dashes(yaml_content):
    """Convert underscores to dashes in Kubernetes resource names."""
    # Load the YAML content
    docs = list(yaml.safe_load_all(yaml_content))
    
    for doc in docs:
        if 'metadata' in doc and 'name' in doc['metadata']:
            doc['metadata']['name'] = doc['metadata']['name'].replace('_', '-')
        
        # Handle service names in selectors
        if 'spec' in doc and 'selector' in doc['spec']:
            if 'matchLabels' in doc['spec']['selector']:
                for key in list(doc['spec']['selector']['matchLabels'].keys()):
                    new_key = key.replace('_', '-')
                    if new_key != key:
                        doc['spec']['selector']['matchLabels'][new_key] = doc['spec']['selector']['matchLabels'].pop(key)
        
        # Handle service names in the service files
        if 'kind' in doc and doc['kind'] == 'Service':
            if 'metadata' in doc and 'name' in doc['metadata']:
                doc['metadata']['name'] = doc['metadata']['name'].replace('_', '-')
        
        # Handle hostname in pod spec
        if 'spec' in doc and 'template' in doc['spec']:
            template = doc['spec']['template']
            if 'spec' in template:
                if 'hostname' in template['spec']:
                    template['spec']['hostname'] = template['spec']['hostname'].replace('_', '-')
                
                # Handle environment variables
                if 'containers' in template['spec']:
                    for container in template['spec']['containers']:
                        if 'env' in container:
                            for env in container['env']:
                                if 'value' in env:
                                    # Replace underscores with dashes in environment variable values
                                    env['value'] = env['value'].replace('_', '-')
    
    # Convert back to YAML
    return yaml.dump_all(docs)

def convert_to_daemonset(yaml_content):
    """Convert a Deployment to a DaemonSet."""
    # Load the YAML content
    doc = yaml.safe_load(yaml_content)
    
    if doc.get('kind') == 'Deployment':
        # Change kind to DaemonSet
        doc['kind'] = 'DaemonSet'
        
        # Remove deployment-specific fields
        if 'spec' in doc:
            if 'replicas' in doc['spec']:
                del doc['spec']['replicas']
            if 'strategy' in doc['spec']:
                del doc['spec']['strategy']
    
    return yaml.dump(doc)

def process_output_files(output_dir, daemon_services=None):
    """Process all YAML files in the output directory."""
    if daemon_services is None:
        daemon_services = []
    
    # First, collect all files that need processing
    yaml_files = list(Path(output_dir).glob('*.yaml'))
    print(f"[DEBUG] Found {len(yaml_files)} YAML files to process")
    
    # Track files that need to be renamed and files to be removed
    files_to_rename = []
    files_to_remove = []
    
    # Process content of all files
    for yaml_file in yaml_files:
        print(f"[DEBUG] Processing file: {yaml_file}")
        with open(yaml_file, 'r') as f:
            content = f.read()
        
        # Convert underscores to dashes
        modified_content = convert_underscores_to_dashes(content)
        
        # If this is a deployment for a service that should be a daemonset, convert it
        if yaml_file.name.endswith('-deployment.yaml'):
            service_name = yaml_file.name.replace('-deployment.yaml', '')
            if service_name in daemon_services:
                print(f"[INFO] Converting {service_name} to DaemonSet")
                modified_content = convert_to_daemonset(modified_content)
                # Mark the original deployment file for removal
                files_to_remove.append(yaml_file)
                # Create the daemonset file with the converted content
                daemonset_name = yaml_file.name.replace('deployment', 'daemonset')
                daemonset_path = yaml_file.parent / daemonset_name
                with open(daemonset_path, 'w') as f:
                    f.write(modified_content)
                print(f"[INFO] Created DaemonSet file: {daemonset_path}")
                continue  # Skip writing back to the original deployment file
        
        # Write back to the same file (only for non-daemonset conversions)
        with open(yaml_file, 'w') as f:
            f.write(modified_content)
    
    # Remove the original deployment files that were converted to daemonsets
    for file_to_remove in files_to_remove:
        print(f"[INFO] Removing original deployment file: {file_to_remove}")
        try:
            file_to_remove.unlink()
        except Exception as e:
            print(f"[DEBUG] Error removing {file_to_remove}: {e}")
    
    # Now handle renaming of remaining files (convert underscores to dashes)
    yaml_files = list(Path(output_dir).glob('*.yaml'))  # Refresh the file list
    for yaml_file in yaml_files:
        if '_' in yaml_file.name:
            new_name = yaml_file.name.replace('_', '-')
            new_path = yaml_file.parent / new_name
            print(f"[DEBUG] Renaming {yaml_file} -> {new_path}")
            try:
                yaml_file.rename(new_path)
                print(f"[DEBUG] Successfully renamed {yaml_file} to {new_path}")
            except Exception as e:
                print(f"[DEBUG] Error renaming {yaml_file}: {e}")
    
    # Force a directory refresh
    print(f"[DEBUG] Final directory contents:")
    for f in Path(output_dir).glob('*.yaml'):
        print(f"  - {f}")

def main():
    parser = argparse.ArgumentParser(description='Convert Docker Compose to Kubernetes manifests and build/push Docker images')
    parser.add_argument('docker_compose_file', help='Path to the Docker Compose file')
    parser.add_argument('output_dir', help='Output directory for Kubernetes manifests')
    parser.add_argument('--registry', help='Docker registry URL (e.g., localhost:5000)', required=True)
    parser.add_argument('--skip-build', action='store_true', help='Skip building and pushing Docker images')
    parser.add_argument('--daemon-services', help='Comma-separated list of services to convert to DaemonSets')
    
    args = parser.parse_args()
    
    if not os.path.exists(args.docker_compose_file):
        print(f"Error: {args.docker_compose_file} does not exist", file=sys.stderr)
        sys.exit(1)
    
    # Parse daemon services if provided
    daemon_services = []
    if args.daemon_services:
        daemon_services = [s.strip() for s in args.daemon_services.split(',')]
        print(f"[INFO] Services to be converted to DaemonSets: {daemon_services}")
    
    # Step 1: Run kompose conversion
    print("[INFO] Step 1: Converting docker-compose to Kubernetes manifests")
    k8s_manifests = run_kompose(args.docker_compose_file, args.output_dir)
    
    # Step 2: Update image references in the generated manifests
    print("[INFO] Step 2: Updating image references in Kubernetes manifests")
    update_image_references(args.output_dir, args.registry)
    
    # Step 3: Process the manifests (convert underscores to dashes, etc.)
    print("[INFO] Step 3: Processing Kubernetes manifests")
    process_output_files(args.output_dir, daemon_services)
    
    # Step 4: Build and push Docker images if not skipped
    if not args.skip_build:
        print("[INFO] Step 4: Building and pushing Docker images")
        services = parse_docker_compose(args.docker_compose_file)
        docker_compose_dir = os.path.dirname(os.path.abspath(args.docker_compose_file))
        build_and_push_images(services, args.registry, docker_compose_dir)
    
    print(f"[INFO] Kubernetes manifests have been written to {args.output_dir}")

if __name__ == "__main__":
    main() 