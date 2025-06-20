#!/usr/bin/env python3
"""
Script to fix environment variable names in Kubernetes deployment files.
Converts hyphens to underscores in environment variable names to match
what the applications expect.
"""

import os
import re
import sys
from pathlib import Path

def fix_env_vars_in_file(file_path):
    """Fix environment variable names in a single file."""
    print(f"Processing {file_path}")
    
    with open(file_path, 'r') as f:
        content = f.read()
    
    # Pattern to match environment variable names in Kubernetes YAML
    # Matches: - name: VARIABLE-NAME
    pattern = r'(\s+-\s+name:\s+)([A-Z0-9_-]+)'
    
    def replace_env_var(match):
        indent = match.group(1)
        var_name = match.group(2)
        # Convert hyphens to underscores
        fixed_name = var_name.replace('-', '_')
        if var_name != fixed_name:
            print(f"  Fixed: {var_name} -> {fixed_name}")
        return indent + fixed_name
    
    new_content = re.sub(pattern, replace_env_var, content)
    
    if new_content != content:
        with open(file_path, 'w') as f:
            f.write(new_content)
        print(f"  Updated {file_path}")
    else:
        print(f"  No changes needed in {file_path}")

def main():
    if len(sys.argv) != 2:
        print("Usage: python fix_env_vars.py <directory>")
        sys.exit(1)
    
    directory = Path(sys.argv[1])
    if not directory.exists():
        print(f"Directory {directory} does not exist")
        sys.exit(1)
    
    # Find all Kubernetes deployment files
    deployment_files = list(directory.glob("*-deployment.yaml"))
    
    if not deployment_files:
        print(f"No deployment files found in {directory}")
        sys.exit(1)
    
    print(f"Found {len(deployment_files)} deployment files")
    
    for file_path in deployment_files:
        fix_env_vars_in_file(file_path)
    
    print("Done!")

if __name__ == "__main__":
    main() 