#!/bin/bash

# Script to add node selectors to all sockshop service deployments
# This will schedule jaeger to node-2, and all other services to node-3

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Directory variables (relative to script location - go up one level from k8s-convert)
SOCKSHOP_DIR="$SCRIPT_DIR/../examples/sockshop"
K8S_DIR="$SOCKSHOP_DIR/build/k8s"

# Check if k8s directory exists
if [ ! -d "$K8S_DIR" ]; then
    echo "Error: Kubernetes manifests directory not found: $K8S_DIR"
    echo "Please run full-deploy.sh first to generate the manifests."
    exit 1
fi

echo "==== Adding Node Selectors ===="
echo "[INFO] Labeling nodes for workload placement..."

# Execute: Label nodes for workload placement
# node-2 -> jaeger; node-3 -> all other sockshop services
kubectl label nodes node-2 workload=jaeger --overwrite || echo "[WARNING] Failed to label node-2"
kubectl label nodes node-3 workload=sockshop --overwrite || echo "[WARNING] Failed to label node-3"

echo "[INFO] Adding node selectors to Kubernetes manifests..."
echo "[INFO] Note: Files may be owned by root, using sudo to modify them..."

# Execute: Add node selectors to deployments using Python
# Use sudo since files may be owned by root (created by sudo in full-deploy.sh)
cd "$K8S_DIR"

sudo python3 << EOF
import yaml
import glob
import os
import sys

# Node selectors for different services
node_selectors = {
    'sockshop': {'workload': 'sockshop'},  # All sockshop services on node-3
    'jaeger': {'workload': 'jaeger'},      # Jaeger on node-2
}

deployment_files = glob.glob('*-deployment.yaml')
if not deployment_files:
    print("[WARNING] No deployment files found in $K8S_DIR")
    sys.exit(1)

for yaml_file in deployment_files:
    with open(yaml_file, 'r') as f:
        doc = yaml.safe_load(f)
    
    if doc and doc.get('kind') == 'Deployment':
        service_name = doc['metadata']['name'].lower()
        
        # Determine which node selector to use
        if 'jaeger' in service_name:
            selector = node_selectors['jaeger']
            print(f"[INFO] Adding node selector to {yaml_file} for node-2 (jaeger)")
        else:
            selector = node_selectors['sockshop']
            print(f"[INFO] Adding node selector to {yaml_file} for node-3 (sockshop)")
        
        # Add node selector to the deployment
        if 'spec' not in doc:
            doc['spec'] = {}
        if 'template' not in doc['spec']:
            doc['spec']['template'] = {}
        if 'spec' not in doc['spec']['template']:
            doc['spec']['template']['spec'] = {}
        
        doc['spec']['template']['spec']['nodeSelector'] = selector
        
        # Write the modified YAML back to the file
        # Use absolute path to ensure we can write even if owned by root
        abs_path = os.path.abspath(yaml_file)
        with open(abs_path, 'w') as f:
            yaml.dump(doc, f, default_flow_style=False, sort_keys=False)

print("[INFO] Node selectors added to all deployments")
EOF

echo ""
echo "[SUCCESS] Node selectors added successfully!"
echo ""
echo "Summary:"
echo "- Jaeger will be scheduled on node-2 (workload=jaeger)"
echo "- All other sockshop services will be scheduled on node-3 (workload=sockshop)"
echo ""
echo "To verify node labels: kubectl get nodes --show-labels"
echo "To verify pod placement: kubectl get pods -o wide"