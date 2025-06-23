# k8s-convert

A comprehensive tool suite for converting Blueprint Docker deployments into Kubernetes manifests.

## Features

- **Complete Pipeline**: End-to-end conversion from Docker Compose to Kubernetes
- **Smart Resource Generation**:
  - Deployments for stateless services
  - StatefulSets for databases
  - Services for networking
  - ConfigMaps for environment variables
  - PersistentVolumeClaims for storage
  - Ingress resources for HTTP traffic
- **Image Management**:
  - Build and push images to registries
  - Support for private registries
  - Automatic image tagging
- **Kubernetes Best Practices**:
  - Resource limits and requests
  - Health checks (liveness and readiness probes)
  - Proper namespace isolation
  - Service discovery
- **Validation**:
  - Input validation for Docker Compose files
  - Output validation for Kubernetes manifests
  - kubectl dry-run validation

## Installation

```bash
# Clone the repository
git clone https://github.com/your-org/blueprint-docc-mod.git
cd blueprint-docc-mod

# Install dependencies
pip install -r requirements.txt
```

## Usage

### Basic Conversion

```bash
# Convert a Blueprint Docker Compose deployment to Kubernetes manifests
python -m k8s_convert.main /path/to/compose/dir /path/to/output/dir
```

### With Image Registry

```bash
# Convert and push images to a registry
python -m k8s_convert.main /path/to/compose/dir /path/to/output/dir \
    --registry-url registry.example.com \
    --username myuser \
    --password mypass
```

### Advanced Options

```bash
# Use a specific namespace
python -m k8s_convert.main /path/to/compose/dir /path/to/output/dir \
    --namespace my-namespace

# Skip validation
python -m k8s_convert.main /path/to/compose/dir /path/to/output/dir \
    --no-validate

# Use insecure registry
python -m k8s_convert.main /path/to/compose/dir /path/to/output/dir \
    --registry-url registry.example.com \
    --insecure
```

## Output Structure

The tool generates the following Kubernetes manifests:

```
output_dir/
├── namespace.yaml
├── service1-deployment.yaml
├── service1-service.yaml
├── service1-configmap.yaml
├── service1-ingress.yaml
├── db-statefulset.yaml
├── db-service.yaml
└── db-pvc.yaml
```

## Configuration

The tool automatically detects service types and applies appropriate configurations:

- **Databases**: Uses StatefulSets with persistent storage
- **Web Services**: Creates Ingress resources for HTTP traffic
- **Stateful Services**: Configures appropriate volume mounts
- **Resource Limits**: Applies sensible defaults based on service type

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details. 