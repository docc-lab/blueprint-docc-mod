import os
import yaml
from pathlib import Path
from typing import Dict, Any, Optional

class Config:
    def __init__(self, config_file: Optional[str] = None):
        self.config_file = config_file
        self.config = self._load_config()

    def _load_config(self) -> Dict[str, Any]:
        """Load configuration from file and environment variables."""
        config = {
            'registry': self._get_registry_config(),
            'kubernetes': self._get_kubernetes_config(),
            'resources': self._get_resource_config()
        }
        
        # Override with config file if provided
        if self.config_file and os.path.exists(self.config_file):
            with open(self.config_file, 'r') as f:
                file_config = yaml.safe_load(f)
                if file_config:
                    self._deep_update(config, file_config)
        
        return config

    def _get_registry_config(self) -> Dict[str, Any]:
        """Get registry configuration from environment variables."""
        return {
            'host': os.getenv('REGISTRY_HOST', 'localhost'),
            'port': int(os.getenv('REGISTRY_PORT', '5000')),
            'path': os.getenv('REGISTRY_PATH', ''),
            'insecure': os.getenv('REGISTRY_INSECURE', 'false').lower() == 'true',
            'username': os.getenv('REGISTRY_USERNAME'),
            'password': os.getenv('REGISTRY_PASSWORD')
        }

    def _get_kubernetes_config(self) -> Dict[str, Any]:
        """Get Kubernetes configuration from environment variables."""
        return {
            'namespace': os.getenv('K8S_NAMESPACE', 'default'),
            'context': os.getenv('K8S_CONTEXT'),
            'insecure_skip_tls_verify': os.getenv('K8S_INSECURE_SKIP_TLS_VERIFY', 'false').lower() == 'true'
        }

    def _get_resource_config(self) -> Dict[str, Any]:
        """Get resource configuration from environment variables."""
        return {
            'default': {
                'requests': {
                    'cpu': os.getenv('DEFAULT_CPU_REQUEST', '100m'),
                    'memory': os.getenv('DEFAULT_MEMORY_REQUEST', '128Mi')
                },
                'limits': {
                    'cpu': os.getenv('DEFAULT_CPU_LIMIT', '500m'),
                    'memory': os.getenv('DEFAULT_MEMORY_LIMIT', '512Mi')
                }
            },
            'database': {
                'requests': {
                    'cpu': os.getenv('DB_CPU_REQUEST', '200m'),
                    'memory': os.getenv('DB_MEMORY_REQUEST', '256Mi')
                },
                'limits': {
                    'cpu': os.getenv('DB_CPU_LIMIT', '1000m'),
                    'memory': os.getenv('DB_MEMORY_LIMIT', '1Gi')
                }
            }
        }

    def _deep_update(self, base: Dict[str, Any], update: Dict[str, Any]):
        """Recursively update a dictionary with another dictionary."""
        for key, value in update.items():
            if isinstance(value, dict) and key in base and isinstance(base[key], dict):
                self._deep_update(base[key], value)
            else:
                base[key] = value

    def get_registry_url(self, for_pull: bool = False) -> str:
        """Get the registry URL for pushing or pulling images."""
        registry = self.config['registry']
        host = registry['host']
        port = registry['port']
        path = registry['path']
        
        # For pull operations, use the appropriate host based on the environment
        if for_pull:
            if os.getenv('K8S_CLUSTER_TYPE') == 'minikube':
                host = 'host.minikube.internal'
            elif os.getenv('K8S_CLUSTER_TYPE') == 'docker':
                host = 'host.docker.internal'
            elif os.getenv('K8S_CLUSTER_TYPE') == 'kind':
                host = 'kind-registry'
            # Keep the same port for both push and pull operations
            port = registry['port']
        
        url = f"{host}:{port}"
        if path:
            url = f"{url}/{path}"
        return url

    def get_resource_limits(self, service_name: str) -> Dict[str, Any]:
        """Get resource limits for a service."""
        resources = self.config['resources']
        if any(db in service_name.lower() for db in os.getenv('DB_SERVICE_TYPES', 'mysql,mongo,postgres,redis').split(',')):
            return resources['database']
        return resources['default'] 