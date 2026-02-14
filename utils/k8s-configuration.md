# Docker Registry Setup Guide

This document provides instructions for setting up a Docker registry in a Kubernetes environment for container image storage and distribution.

## Overview

The setup includes:
- **Local Docker Registry**: Host-level registry for development
- **Kubernetes Docker Registry**: Cluster-managed registry with persistent storage
- **Storage Configuration**: Persistent volume setup for registry data

## Local Docker Registry Setup

### 1. Run Local Registry Container

Deploy a Docker registry on the host machine for local development:

```bash
docker run -d \
  --name local-registry \
  --restart=always \
  -p 5000:5000 \
  -v registry-data:/var/lib/registry \
  registry:2
```

**Configuration Details**:
- **Image**: `registry:2`
- **Port**: 5000 (exposed on host)
- **Storage**: Docker volume for persistent data
- **Restart Policy**: Always restart on failure

### 2. Configure Docker Daemon

Configure Docker to allow insecure connections to the local registry. **Recommended approach is using systemd configuration** rather than daemon.json to avoid Docker startup issues:

**✅ Recommended: Using systemd configuration**
```bash
# Edit the Docker systemd configuration
sudo nano /etc/systemd/system/docker.service.d/docker-options.conf

# Add insecure registry options to DOCKER_OPTS
[Service]
Environment="DOCKER_OPTS=--insecure-registry=localhost:5000 --insecure-registry=127.0.0.1:5000 --insecure-registry=10.10.1.1/24 --insecure-registry=192.168.128.0/17 [other existing options]"

# Reload systemd and restart Docker
sudo systemctl daemon-reload
sudo systemctl restart docker
```

**⚠️ Alternative: Using daemon.json (can cause Docker startup failures)**
```bash
# Create or edit /etc/docker/daemon.json
{
  "insecure-registries": [
    "localhost:5000",
    "127.0.0.1:5000",
    "10.10.1.1/24",
    "192.168.128.0/17"
  ]
}

# Restart Docker daemon
sudo systemctl restart docker
```

**Note**: The CIDR ranges (like `10.10.1.1/24` and `192.168.128.0/17`) allow insecure access to entire network ranges, which is useful for Kubernetes cluster networks. This means:

- **`10.10.1.1/24`**: Any IP in the `10.10.1.0/24` range (like `10.10.1.1:30000`, `10.10.1.2:30000`) is treated as an insecure registry
- **`192.168.128.0/17`**: Covers the Kubernetes service network for internal cluster communication
- **NodePort Access**: When the Kubernetes registry service exposes port 30000 via NodePort, Docker automatically treats it as insecure due to these CIDR ranges

### 3. Verify Network Configuration

Before configuring insecure registries, verify your network setup:

```bash
# Check local network interfaces
ip addr show

# Check Kubernetes service network
kubectl cluster-info dump | grep -E "service-cluster-ip-range|cluster-cidr"

# Verify Docker daemon options are loaded
sudo systemctl status docker --no-pager | grep insecure-registry
```

**Example Network Verification**:
```bash
# Local interface (example)
enp94s0f1: inet 10.10.1.1/24 brd 10.10.1.255 scope global

# Kubernetes service range
--service-cluster-ip-range=192.168.128.0/17

# Docker daemon with insecure registries loaded
/usr/bin/dockerd --insecure-registry=localhost:5000 --insecure-registry=127.0.0.1:5000 --insecure-registry=10.10.1.1/24 --insecure-registry=192.168.128.0/17
```

## Kubernetes Docker Registry Setup

### 1. Create Registry Namespace

```bash
kubectl create namespace registry
```

### 2. Persistent Volume Claim

Create a persistent volume claim for registry storage:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: registry-pvc
  namespace: registry
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: nfs-client
```

### 3. Registry Deployment

Deploy the registry as a Kubernetes deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: docker-registry
  namespace: registry
  labels:
    app: docker-registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: docker-registry
  template:
    metadata:
      labels:
        app: docker-registry
    spec:
      containers:
      - name: registry
        image: registry:2
        ports:
        - containerPort: 5000
        env:
        - name: REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY
          value: /var/lib/registry
        - name: REGISTRY_HTTP_ADDR
          value: :5000
        - name: REGISTRY_STORAGE_DELETE_ENABLED
          value: "true"
        - name: REGISTRY_HTTP_HEADERS
          value: 'X-Content-Type-Options: [nosniff]'
        volumeMounts:
        - mountPath: /var/lib/registry
          name: registry-storage
        livenessProbe:
          httpGet:
            path: /v2/
            port: 5000
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /v2/
            port: 5000
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: registry-storage
        persistentVolumeClaim:
          claimName: registry-pvc
```

### 4. Registry Service

Expose the registry through a Kubernetes service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: docker-registry
  namespace: registry
  labels:
    app: docker-registry
spec:
  type: NodePort
  ports:
  - port: 30000
    targetPort: 5000
    nodePort: 30000
    name: registry
  selector:
    app: docker-registry
```

## Deployment Commands

### Deploy Registry to Kubernetes

```bash
# Create namespace
kubectl create namespace registry

# Apply configurations
kubectl apply -f registry-pvc.yaml
kubectl apply -f registry-deployment.yaml
kubectl apply -f registry-service.yaml

# Verify deployment
kubectl get pods -n registry
kubectl get svc -n registry

# Check registry is accessible
curl http://10.10.1.1:30000/v2/
```

### Test Registry Access

```bash
# Test Kubernetes registry (replace with your node IP)
curl http://10.10.1.1:30000/v2/

# Push test image to registry
docker pull hello-world
docker tag hello-world 10.10.1.1:30000/hello-world
docker push 10.10.1.1:30000/hello-world

# Verify image was stored
curl http://10.10.1.1:30000/v2/_catalog

# Test pulling image back
docker pull 10.10.1.1:30000/hello-world
```

### Verify Insecure Registry Configuration

Check if Docker daemon is configured with insecure registries:

```bash
# Check Docker daemon process for insecure registry flags
ps aux | grep dockerd | grep insecure-registry

# Check daemon.json file (if it exists)
cat /etc/docker/daemon.json

# Test registry connectivity
docker pull hello-world
docker tag hello-world localhost:5000/test-image
docker push localhost:5000/test-image
```

If the push fails with TLS errors, verify that the registry address is included in the insecure registry configuration.

## Registry Configuration Options

### Environment Variables

Key environment variables for registry configuration:

- `REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY`: Storage directory path
- `REGISTRY_HTTP_ADDR`: Registry listening address and port
- `REGISTRY_STORAGE_DELETE_ENABLED`: Enable image deletion (set to "true")
- `REGISTRY_HTTP_HEADERS`: Security headers configuration

### Storage Configuration

The registry uses persistent storage with:
- **Access Mode**: ReadWriteOnce (single node access)
- **Storage Size**: 10Gi (adjustable based on needs)
- **Storage Class**: Uses cluster's default storage class

## Registry Management

### List Images

```bash
# List all repositories
curl http://localhost:5000/v2/_catalog

# List tags for specific image
curl http://localhost:5000/v2/<image-name>/tags/list
```

### Delete Images

```bash
# Delete specific image tag
curl -X DELETE http://localhost:5000/v2/<image-name>/manifests/<tag>
```

### Registry Cleanup

```bash
# Run garbage collection (if supported)
docker exec local-registry registry garbage-collect /etc/docker/registry/config.yml
```

## Monitoring and Troubleshooting

### Health Checks

```bash
# Check registry status
kubectl get pods -n registry
kubectl logs -n registry deployment/docker-registry

# Check storage usage
kubectl describe pvc registry-pvc -n registry
```

### Common Issues

1. **Docker Daemon Won't Start After Configuration**:
   - **Problem**: Docker fails to start after adding daemon.json configuration
   - **Solution**: Use systemd configuration instead of daemon.json
   - **Commands**:
     ```bash
     # Remove problematic daemon.json
     sudo rm /etc/docker/daemon.json
     
     # Use systemd configuration instead
     sudo nano /etc/systemd/system/docker.service.d/docker-options.conf
     sudo systemctl daemon-reload
     sudo systemctl restart docker
     ```

2. **Registry Not Accessible**:
   - Verify pod is running: `kubectl get pods -n registry`
   - Check service configuration: `kubectl get svc -n registry`
   - Review pod logs: `kubectl logs -n registry deployment/docker-registry`

3. **Push/Pull Failures**:
   - Ensure insecure registry is configured in Docker daemon
   - Verify network connectivity to registry
   - Check if Docker daemon was restarted after configuration changes
   - Verify Docker daemon options are loaded: `sudo systemctl status docker --no-pager | grep insecure-registry`

4. **Storage Issues**:
   - Check PVC status: `kubectl get pvc -n registry`
   - Verify storage class availability: `kubectl get storageclass`

### Logs and Debugging

```bash
# Registry container logs
kubectl logs -n registry deployment/docker-registry

# Docker daemon logs
journalctl -u docker.service

# Kubernetes events
kubectl get events -n registry --sort-by='.lastTimestamp'
```

## Security Considerations

### Development Setup

The current configuration is designed for development environments:
- **HTTP Only**: No TLS encryption configured
- **No Authentication**: Open access for development
- **Insecure Registry**: Allows unencrypted connections

### Production Recommendations

For production environments, consider:
- **TLS Certificates**: Configure HTTPS for secure communication
- **Authentication**: Implement user authentication and authorization
- **Network Policies**: Restrict access using Kubernetes network policies
- **Image Scanning**: Integrate vulnerability scanning tools

## Maintenance

### Backup Strategy

```bash
# Backup registry data volume
docker run --rm -v registry-data:/data -v $(pwd):/backup alpine tar czf /backup/registry-backup.tar.gz -C /data .
```

### Updates

```bash
# Update registry image
kubectl set image deployment/docker-registry registry=registry:2 -n registry

# Rolling update
kubectl rollout status deployment/docker-registry -n registry
```

---

## ✅ Successful Configuration Summary

After following this guide, you should have:

- **Kubernetes Registry**: Running in `registry` namespace with persistent storage
- **Service**: Exposed on NodePort 30000 (accessible via `10.10.1.1:30000`)
- **Docker Configuration**: Insecure registries configured via systemd
- **Full Functionality**: Push/pull operations working correctly

### Registry Access Information:
- **Registry URL**: `10.10.1.1:30000`
- **Storage**: 10Gi persistent volume
- **Namespace**: `registry`
- **Service Type**: NodePort

### Usage Examples:
```bash
# Push image to registry
docker tag my-image 10.10.1.1:30000/my-image
docker push 10.10.1.1:30000/my-image

# Pull image from registry
docker pull 10.10.1.1:30000/my-image

# List registry contents
curl http://10.10.1.1:30000/v2/_catalog
```

---

*This setup provides a reliable Docker registry for container image storage and distribution in Kubernetes environments.*