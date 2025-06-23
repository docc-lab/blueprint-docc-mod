import os
import yaml
import logging
import subprocess
from typing import Dict, Any, List, Optional
from .utils import (
    docker_to_k8s_name,
    parse_env,
    write_yaml,
    get_kubernetes_resource_limits,
    load_env_file,
    resolve_env_vars
)

logger = logging.getLogger(__name__)

class K8sConvert:
    def __init__(
        self,
        compose_dir: str,
        output_dir: str,
        namespace: str = "default",
        registry_url: Optional[str] = None,
        use_kompose: bool = False
    ):
        self.compose_dir = compose_dir
        self.output_dir = output_dir
        self.namespace = namespace
        self.registry_url = registry_url
        self.use_kompose = use_kompose
        self.compose_file = os.path.join(compose_dir, 'docker-compose.yml')
        self.env_file = os.path.join(compose_dir, '.env')
        self.services = {}
        self.env_vars = load_env_file(self.env_file)
        self.load_compose()

    def load_compose(self):
        with open(self.compose_file, 'r') as f:
            self.compose = yaml.safe_load(f)
        self.services = self.compose.get('services', {})

    def convert(self):
        """Convert Docker Compose services to Kubernetes manifests."""
        os.makedirs(self.output_dir, exist_ok=True)
        
        # Copy environment files if they exist
        if os.path.exists(self.env_file):
            import shutil
            shutil.copy(self.env_file, os.path.join(self.output_dir, '.env'))
            logger.info(f"Copied environment file to {self.output_dir}/.env")

        # Use kompose for conversion if enabled
        if self.use_kompose:
            self.convert_with_kompose()
        else:
            self.convert_native()

    def convert_with_kompose(self):
        """Convert using kompose."""
        try:
            # Use KOMPOSE_BIN env variable if set, else fallback to 'kompose'
            kompose_bin = os.environ.get('KOMPOSE_BIN', 'kompose')
            cmd = [kompose_bin, 'convert', '-f', self.compose_file, '-o', self.output_dir]
            subprocess.run(cmd, check=True)
            logger.info("Successfully converted using kompose")

            # Post-process the generated files
            self._post_process_kompose_output()

        except subprocess.CalledProcessError as e:
            logger.error(f"Kompose conversion failed: {e}")
            raise
        except Exception as e:
            logger.error(f"Error during kompose conversion: {e}")
            raise

    def _post_process_kompose_output(self):
        """Post-process the kompose-generated files."""
        for yaml_file in os.listdir(self.output_dir):
            if not yaml_file.endswith('.yaml'):
                continue

            file_path = os.path.join(self.output_dir, yaml_file)
            with open(file_path, 'r') as f:
                content = yaml.safe_load(f)

            # Update namespace
            if 'metadata' in content:
                content['metadata']['namespace'] = self.namespace

            # Update image references if registry_url is provided
            if self.registry_url:
                if content['kind'] in ['Deployment', 'StatefulSet', 'DaemonSet']:
                    for container in content['spec']['template']['spec']['containers']:
                        if 'image' in container:
                            # Extract image name without tag
                            image_parts = container['image'].split(':')
                            image_name = image_parts[0]
                            tag = image_parts[1] if len(image_parts) > 1 else 'latest'
                            # Update with registry
                            container['image'] = f"{self.registry_url}/{image_name}:{tag}"

            # Write back the modified content
            with open(file_path, 'w') as f:
                yaml.dump(content, f, default_flow_style=False)

    def convert_native(self):
        """Convert using native implementation."""
        # Generate namespace manifest
        self.generate_namespace()
        
        # Process each service
        for svc_name, svc in self.services.items():
            k8s_name = docker_to_k8s_name(svc_name)
            
            # Generate appropriate workload resource
            if self._is_database(svc):
                self.generate_statefulset(svc_name, svc, k8s_name)
                # Generate database initialization ConfigMap
                self.generate_db_configmap(svc_name, svc, k8s_name)
            else:
                self.generate_deployment(svc_name, svc, k8s_name)
            
            # Generate supporting resources
            self.generate_service(svc_name, svc, k8s_name)
            if 'environment' in svc:
                self.generate_configmap(svc_name, svc, k8s_name)
            if self._needs_ingress(svc):
                self.generate_ingress(svc_name, svc, k8s_name)
            if self._needs_pvc(svc):
                self.generate_pvc(svc_name, k8s_name)

    def _is_database(self, svc: Dict[str, Any]) -> bool:
        """Check if service is a database."""
        image = svc.get('image', '').lower()
        return any(db in image for db in ['mysql', 'mongo', 'postgres', 'redis'])

    def _get_db_type(self, svc: Dict[str, Any]) -> str:
        """Get the database type from the service image."""
        image = svc.get('image', '').lower()
        if 'mysql' in image:
            return 'mysql'
        elif 'mongo' in image:
            return 'mongo'
        elif 'postgres' in image:
            return 'postgres'
        elif 'redis' in image:
            return 'redis'
        return ''

    def generate_db_configmap(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate a ConfigMap for database initialization."""
        # Always parse environment to dict
        env_vars = parse_env(svc.get('environment', {}), self.env_file)
        
        # Get database type
        db_type = self._get_db_type(svc)
        
        if db_type == 'mysql':
            config_data = {
                'MYSQL_ROOT_HOST': '%',
                'MYSQL_ROOT_PASSWORD': env_vars.get('MYSQL_ROOT_PASSWORD', 'pass'),
                'MYSQL_DATABASE': env_vars.get('MYSQL_DATABASE', f'{k8s_name}_db'),
                'MYSQL_USER': env_vars.get('MYSQL_USER', 'root'),
                'MYSQL_PASSWORD': env_vars.get('MYSQL_PASSWORD', 'pass')
            }
        elif db_type == 'mongo':
            config_data = {
                'MONGO_INITDB_ROOT_USERNAME': env_vars.get('MONGO_INITDB_ROOT_USERNAME', 'root'),
                'MONGO_INITDB_ROOT_PASSWORD': env_vars.get('MONGO_INITDB_ROOT_PASSWORD', 'pass')
            }
        else:
            config_data = env_vars
        
        configmap = {
            'apiVersion': 'v1',
            'kind': 'ConfigMap',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'data': config_data
        }
        write_yaml(configmap, os.path.join(self.output_dir, f'{k8s_name}-configmap.yaml'))

    def generate_statefulset(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate Kubernetes StatefulSet manifest."""
        db_type = self._get_db_type(svc)
        if not db_type:
            return

        statefulset = {
            'apiVersion': 'apps/v1',
            'kind': 'StatefulSet',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'spec': {
                'serviceName': k8s_name,
                'replicas': 1,
                'selector': {
                    'matchLabels': {
                        'app': k8s_name
                    }
                },
                'template': {
                    'metadata': {
                        'labels': {
                            'app': k8s_name
                        }
                    },
                    'spec': {
                        'containers': [
                            {
                                'name': k8s_name,
                                'image': svc.get('image', ''),
                                'ports': [
                                    {
                                        'containerPort': 3306 if db_type == 'mysql' else 5432 if db_type == 'postgres' else 27017 if db_type == 'mongo' else 6379
                                    }
                                ],
                                'env': [
                                    {
                                        'name': 'MYSQL_ROOT_PASSWORD',
                                        'valueFrom': {
                                            'configMapKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_ROOT_PASSWORD'
                                            }
                                        }
                                    },
                                    {
                                        'name': 'MYSQL_DATABASE',
                                        'valueFrom': {
                                            'configMapKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_DATABASE'
                                            }
                                        }
                                    },
                                    {
                                        'name': 'MYSQL_USER',
                                        'valueFrom': {
                                            'configMapKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_USER'
                                            }
                                        }
                                    },
                                    {
                                        'name': 'MYSQL_PASSWORD',
                                        'valueFrom': {
                                            'configMapKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_PASSWORD'
                                            }
                                        }
                                    }
                                ],
                                'volumeMounts': [
                                    {
                                        'name': 'data',
                                        'mountPath': self._get_data_mount_path(svc)
                                    }
                                ],
                                'resources': get_kubernetes_resource_limits(svc_name, self.config),
                                'livenessProbe': self._generate_probe(svc, 'liveness'),
                                'readinessProbe': self._generate_probe(svc, 'readiness')
                            }
                        ]
                    }
                },
                'volumeClaimTemplates': [
                    {
                        'metadata': {
                            'name': 'data'
                        },
                        'spec': {
                            'accessModes': ['ReadWriteOnce'],
                            'resources': {
                                'requests': {
                                    'storage': '1Gi'
                                }
                            }
                        }
                    }
                ]
            }
        }
        write_yaml(statefulset, os.path.join(self.output_dir, f'{k8s_name}-statefulset.yaml'))

    def generate_deployment(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate Kubernetes Deployment manifest."""
        deployment = {
            'apiVersion': 'apps/v1',
            'kind': 'Deployment',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'spec': {
                'replicas': svc.get('deploy', {}).get('replicas', 1),
                'selector': {
                    'matchLabels': {
                        'app': k8s_name
                    }
                },
                'template': {
                    'metadata': {
                        'labels': {
                            'app': k8s_name
                        }
                    },
                    'spec': {
                        'containers': [
                            {
                                'name': k8s_name,
                                'image': svc.get('image', ''),
                                'ports': [
                                    {
                                        'containerPort': self._parse_port(port)['targetPort']
                                    }
                                    for port in svc.get('ports', [])
                                ] if isinstance(svc.get('ports', []), list) else [],
                                'env': [
                                    {
                                        'name': k,
                                        'value': v
                                    }
                                    for k, v in parse_env(svc.get('environment', {}), self.env_file).items()
                                ],
                                'resources': get_kubernetes_resource_limits(svc_name, self.config),
                                'livenessProbe': self._generate_probe(svc, 'liveness'),
                                'readinessProbe': self._generate_probe(svc, 'readiness')
                            }
                        ]
                    }
                }
            }
        }
        # Ensure the image field is set with the registry pull URL if provided
        if not deployment['spec']['template']['spec']['containers'][0]['image']:
            image_name = f"{svc_name}:latest"
            if self.registry_url:
                image_name = f"{self.registry_url}/{image_name}"
            deployment['spec']['template']['spec']['containers'][0]['image'] = image_name
        write_yaml(deployment, os.path.join(self.output_dir, f'{k8s_name}-deployment.yaml'))

    def generate_service(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate Kubernetes Service manifest."""
        # Get the correct port for the service
        if self._is_database(svc):
            if self._is_mysql(svc):
                port = 3306
            elif self._is_mongodb(svc):
                port = 27017
            else:
                port = 8080
        else:
            # For non-database services, use the first exposed port
            ports = svc.get('ports', [])
            if isinstance(ports, list) and ports:
                # Handle both string format ("8080:8080") and dict format
                port_str = ports[0]
                if isinstance(port_str, str):
                    _, container_port = port_str.split(':')
                    port = int(container_port)
                else:
                    port = self._parse_port(port_str)['targetPort']
            else:
                port = 8080  # Default port for application services
        
        service = {
            'apiVersion': 'v1',
            'kind': 'Service',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'spec': {
                'selector': {
                    'app': k8s_name
                },
                'ports': [{
                    'port': port,
                    'targetPort': port,
                    'protocol': 'TCP'
                }]
            }
        }
        write_yaml(service, os.path.join(self.output_dir, f'{k8s_name}-service.yaml'))

    def generate_configmap(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate Kubernetes ConfigMap manifest."""
        configmap = {
            'apiVersion': 'v1',
            'kind': 'ConfigMap',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'data': parse_env(svc.get('environment', {}), self.env_file)
        }
        write_yaml(configmap, os.path.join(self.output_dir, f'{k8s_name}-configmap.yaml'))

    def generate_ingress(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate Kubernetes Ingress manifest."""
        ingress = {
            'apiVersion': 'networking.k8s.io/v1',
            'kind': 'Ingress',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace,
                'annotations': {
                    'nginx.ingress.kubernetes.io/rewrite-target': '/'
                }
            },
            'spec': {
                'rules': [
                    {
                        'host': f'{k8s_name}.local',
                        'http': {
                            'paths': [
                                {
                                    'path': '/',
                                    'pathType': 'Prefix',
                                    'backend': {
                                        'service': {
                                            'name': k8s_name,
                                            'port': {
                                                'number': int(svc['ports'][0].split(':')[0])
                                            }
                                        }
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }
        write_yaml(ingress, os.path.join(self.output_dir, f'{k8s_name}-ingress.yaml'))

    def generate_pvc(self, svc_name: str, k8s_name: str):
        """Generate Kubernetes PersistentVolumeClaim manifest."""
        pvc = {
            'apiVersion': 'v1',
            'kind': 'PersistentVolumeClaim',
            'metadata': {
                'name': f'{k8s_name}-data',
                'namespace': self.namespace
            },
            'spec': {
                'accessModes': ['ReadWriteOnce'],
                'resources': {
                    'requests': {
                        'storage': '1Gi'
                    }
                }
            }
        }
        write_yaml(pvc, os.path.join(self.output_dir, f'{k8s_name}-pvc.yaml'))

    def _get_data_mount_path(self, svc: Dict[str, Any]) -> str:
        """Get the appropriate data mount path for a service."""
        image = svc.get('image', '').lower()
        if 'mysql' in image:
            return '/var/lib/mysql'
        elif 'mongo' in image:
            return '/data/db'
        elif 'postgres' in image:
            return '/var/lib/postgresql/data'
        elif 'redis' in image:
            return '/data'
        return '/data'

    def _generate_probe(self, svc: Dict[str, Any], probe_type: str) -> Dict[str, Any]:
        """Generate health check probe configuration."""
        if probe_type not in ['liveness', 'readiness']:
            return None

        # Customize probe based on service type
        image = svc.get('image', '').lower()
        if 'mysql' in image:
            return {
                'exec': {
                    'command': ['mysqladmin', 'ping', '-h', 'localhost']
                },
                'initialDelaySeconds': 30,
                'periodSeconds': 10,
                'timeoutSeconds': 5,
                'failureThreshold': 3
            }
        elif 'mongo' in image:
            return {
                'exec': {
                    'command': ['mongosh', '--eval', 'db.adminCommand("ping")']
                },
                'initialDelaySeconds': 30,
                'periodSeconds': 10,
                'timeoutSeconds': 5,
                'failureThreshold': 3
            }
        elif 'redis' in image:
            return {
                'exec': {
                    'command': ['redis-cli', 'ping']
                },
                'initialDelaySeconds': 30,
                'periodSeconds': 10,
                'timeoutSeconds': 5,
                'failureThreshold': 3
            }
        else:
            # Default TCP socket probe for non-database services
            port = int(svc['ports'][0].split(':')[1]) if 'ports' in svc and svc['ports'] else 80
            return {
                'tcpSocket': {
                    'port': port
                },
                'initialDelaySeconds': 30,
                'periodSeconds': 10,
                'timeoutSeconds': 5,
                'failureThreshold': 3
            }

    def generate_namespace(self):
        """Generate Kubernetes namespace manifest."""
        namespace = {
            'apiVersion': 'v1',
            'kind': 'Namespace',
            'metadata': {
                'name': self.namespace
            }
        }
        write_yaml(namespace, os.path.join(self.output_dir, 'namespace.yaml'))

    def _parse_port(self, port_str: Any) -> Dict[str, int]:
        """Parse port string and handle environment variables."""
        try:
            if isinstance(port_str, str):
                # Handle environment variable format: ${VAR_NAME}:port
                if '${' in port_str:
                    _, container_port = port_str.split(':')
                    return {
                        'port': int(container_port),
                        'targetPort': int(container_port)
                    }
                # Handle regular format: host:container
                host_port, container_port = port_str.split(':')
                # Try to resolve any environment variables in the ports
                host_port = resolve_env_vars(host_port, self.env_vars)
                container_port = resolve_env_vars(container_port, self.env_vars)
                return {
                    'port': int(host_port),
                    'targetPort': int(container_port)
                }
            elif isinstance(port_str, dict):
                host_port = port_str.get('published', '8080')
                container_port = port_str.get('target', '8080')
                # Try to resolve any environment variables in the ports
                host_port = resolve_env_vars(str(host_port), self.env_vars)
                container_port = resolve_env_vars(str(container_port), self.env_vars)
                return {
                    'port': int(host_port),
                    'targetPort': int(container_port)
                }
            else:
                # If we can't parse the port, use a default value
                return {
                    'port': 8080,
                    'targetPort': 8080
                }
        except (ValueError, TypeError):
            # If we can't parse the port, use a default value
            return {
                'port': 8080,
                'targetPort': 8080
            }

    def _needs_ingress(self, svc: Dict[str, Any]) -> bool:
        """Check if service needs an Ingress resource."""
        return 'ports' in svc and any(
            port.split(':')[0] in ['80', '443', '8080', '8443']
            for port in svc['ports']
        )

    def _needs_pvc(self, service: Dict[str, Any]) -> bool:
        """Check if a service needs a PersistentVolumeClaim."""
        # Check if the service has volumes defined
        if 'volumes' not in service:
            return False
            
        # Check if any volume is a named volume (not a bind mount)
        for volume in service['volumes']:
            if isinstance(volume, str) and not volume.startswith('./') and not volume.startswith('/'):
                return True
                
        return False

    def _get_db_port(self, service: Dict[str, Any]) -> int:
        """Get the correct port for the database service."""
        if self._is_mysql(service):
            return 3306
        elif self._is_mongodb(service):
            return 27017
        return 8080  # Default port

    def _is_mysql(self, svc: Dict[str, Any]) -> bool:
        """Check if service is a MySQL database."""
        image = svc.get('image', '').lower()
        return 'mysql' in image

    def _is_mongodb(self, svc: Dict[str, Any]) -> bool:
        """Check if service is a MongoDB database."""
        image = svc.get('image', '').lower()
        return 'mongo' in image

    def generate_mysql_deployment(self, svc_name: str, svc: Dict[str, Any], k8s_name: str):
        """Generate MySQL-specific Deployment manifest."""
        # Get environment variables
        env_vars = parse_env(svc.get('environment', {}), self.env_file)
        db_name = env_vars.get('MYSQL_DATABASE', f'{k8s_name}_db')
        db_user = env_vars.get('MYSQL_USER', 'root')
        db_password = env_vars.get('MYSQL_PASSWORD', 'pass')
        
        deployment = {
            'apiVersion': 'apps/v1',
            'kind': 'Deployment',
            'metadata': {
                'name': k8s_name,
                'namespace': self.namespace
            },
            'spec': {
                'replicas': 1,
                'selector': {
                    'matchLabels': {
                        'app': k8s_name
                    }
                },
                'template': {
                    'metadata': {
                        'labels': {
                            'app': k8s_name
                        }
                    },
                    'spec': {
                        'containers': [
                            {
                                'name': k8s_name,
                                'image': svc.get('image', ''),
                                'ports': [
                                    {
                                        'containerPort': 3306
                                    }
                                ],
                                'env': [
                                    {
                                        'name': 'MYSQL_DATABASE',
                                        'value': db_name
                                    },
                                    {
                                        'name': 'MYSQL_USER',
                                        'value': db_user
                                    },
                                    {
                                        'name': 'MYSQL_PASSWORD',
                                        'valueFrom': {
                                            'secretKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_PASSWORD'
                                            }
                                        }
                                    },
                                    {
                                        'name': 'MYSQL_ROOT_PASSWORD',
                                        'valueFrom': {
                                            'secretKeyRef': {
                                                'name': k8s_name,
                                                'key': 'MYSQL_ROOT_PASSWORD'
                                            }
                                        }
                                    }
                                ],
                                'resources': get_kubernetes_resource_limits(svc_name, self.config),
                                'volumeMounts': [
                                    {
                                        'name': 'mysql-data',
                                        'mountPath': '/var/lib/mysql'
                                    }
                                ]
                            }
                        ],
                        'volumes': [
                            {
                                'name': 'mysql-data',
                                'emptyDir': {}
                            }
                        ]
                    }
                }
            }
        }
        # Ensure the image field is set with the registry pull URL if provided
        if not deployment['spec']['template']['spec']['containers'][0]['image']:
            image_name = f"{svc_name}:latest"
            if self.registry_url:
                image_name = f"{self.registry_url}/{image_name}"
            deployment['spec']['template']['spec']['containers'][0]['image'] = image_name
        write_yaml(deployment, os.path.join(self.output_dir, f'{k8s_name}-deployment.yaml'))

def main():
    import argparse
    parser = argparse.ArgumentParser(description='Convert Blueprint Docker Compose to Kubernetes manifests')
    parser.add_argument('compose_dir', help='Path to Blueprint Docker Compose directory')
    parser.add_argument('output_dir', help='Path to output Kubernetes manifests')
    parser.add_argument('--registry-url', help='Registry URL')
    args = parser.parse_args()
    converter = K8sConvert(args.compose_dir, args.output_dir, args.registry_url)
    converter.convert()

if __name__ == '__main__':
    main() 