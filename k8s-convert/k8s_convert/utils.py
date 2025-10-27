import os
import yaml
import subprocess
from typing import Dict, List, Any
import re
from pathlib import Path
from .config import Config

def docker_to_k8s_name(name: str) -> str:
    """Convert Docker service name to Kubernetes-compatible name."""
    # Remove any non-alphanumeric characters and convert to lowercase
    name = re.sub(r'[^a-z0-9-]', '-', name.lower())
    # Remove consecutive hyphens
    name = re.sub(r'-+', '-', name)
    # Remove leading/trailing hyphens
    name = name.strip('-')
    return name

def load_env_file(env_file: str) -> Dict[str, str]:
    """Load environment variables from a .env file."""
    env_vars = {}
    if os.path.exists(env_file):
        with open(env_file, 'r') as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith('#'):
                    key, value = line.split('=', 1)
                    env_vars[key.strip()] = value.strip()
    return env_vars

def resolve_env_vars(value: str, env_vars: Dict[str, str]) -> str:
    """Resolve environment variables in a string using provided env vars."""
    def replace_var(match):
        var_name = match.group(1)
        if var_name in env_vars:
            return env_vars[var_name]
        return match.group(0)  # Return original if not found
    
    return re.sub(r'\${([^}]+)}', replace_var, value)

def parse_env(env_list: Any, env_file: str = None) -> Dict[str, str]:
    """Parse environment variables from Docker Compose format to dict and resolve variables."""
    env_dict = {}
    env_vars = load_env_file(env_file) if env_file else {}
    
    if isinstance(env_list, dict):
        for k, v in env_list.items():
            # Resolve any environment variables in the value
            resolved_value = resolve_env_vars(str(v), env_vars)
            # Convert Docker Compose service names to Kubernetes service names
            resolved_value = re.sub(r'([a-zA-Z0-9_]+)_ctr:', lambda m: f"{docker_to_k8s_name(m.group(1))}-ctr:", resolved_value)
            env_dict[k] = resolved_value
    elif isinstance(env_list, list):
        for env in env_list:
            if isinstance(env, str):
                if '=' in env:
                    key, value = env.split('=', 1)
                    # Resolve any environment variables in the value
                    resolved_value = resolve_env_vars(value, env_vars)
                    # Convert Docker Compose service names to Kubernetes service names
                    resolved_value = re.sub(r'([a-zA-Z0-9_]+)_ctr:', lambda m: f"{docker_to_k8s_name(m.group(1))}-ctr:", resolved_value)
                    env_dict[key] = resolved_value
            elif isinstance(env, dict):
                for k, v in env.items():
                    # Resolve any environment variables in the value
                    resolved_value = resolve_env_vars(str(v), env_vars)
                    # Convert Docker Compose service names to Kubernetes service names
                    resolved_value = re.sub(r'([a-zA-Z0-9_]+)_ctr:', lambda m: f"{docker_to_k8s_name(m.group(1))}-ctr:", resolved_value)
                    env_dict[k] = resolved_value
    return env_dict

def write_yaml(data: Dict[str, Any], filepath: str):
    """Write YAML data to file with proper formatting."""
    os.makedirs(os.path.dirname(filepath), exist_ok=True)
    with open(filepath, 'w') as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)

def validate_compose_file(compose_path: str) -> bool:
    """Validate Docker Compose file format and content."""
    try:
        with open(compose_path, 'r') as f:
            compose = yaml.safe_load(f)
        
        # Check required fields
        if not isinstance(compose, dict):
            return False
        if 'services' not in compose:
            return False
        
        # Validate each service
        for svc_name, svc in compose['services'].items():
            if not isinstance(svc, dict):
                return False
            if 'image' not in svc and 'build' not in svc:
                return False
        
        return True
    except Exception:
        return False

def validate_k8s_manifests(manifests_dir: str) -> bool:
    """Validate generated Kubernetes manifests using kubectl."""
    try:
        # Check if kubectl is available
        subprocess.run(['kubectl', 'version', '--client'], 
                      check=True, capture_output=True)
        
        # Validate each YAML file
        for root, _, files in os.walk(manifests_dir):
            for file in files:
                if file.endswith(('.yaml', '.yml')):
                    filepath = os.path.join(root, file)
                    result = subprocess.run(
                        ['kubectl', 'apply', '--dry-run=client', '-f', filepath],
                        capture_output=True,
                        text=True
                    )
                    if result.returncode != 0:
                        print(f"Validation failed for {filepath}:")
                        print(result.stderr)
                        return False
        return True
    except Exception as e:
        print(f"Validation error: {str(e)}")
        return False

def get_kubernetes_resource_limits(service_name: str, config: Config) -> Dict[str, str]:
    """Get resource limits for a service using the configuration system."""
    return config.get_resource_limits(service_name) 