# d2k8s

A tool for converting Docker Compose applications to Kubernetes manifests, with additional enhancements for handling Dockerfile configurations.

## Features

- Converts Docker Compose files to Kubernetes manifests using Kompose
- Handles Dockerfile configurations through build directives
- Converts underscores to dashes in Kubernetes resource names
- Preserves build context and arguments
- Supports output to individual YAML files in a specified directory

## Requirements

- Python 3.6+
- Kompose installed and available in PATH
- PyYAML package

## Installation

1. Install dependencies:
```bash
pip install -r requirements.txt
```

2. Make the script executable:
```bash
chmod +x d2k8s.py
```

## Usage

Basic usage (outputs to stdout):
```bash
./d2k8s.py <docker-compose-file>
```

Output to a directory (creates individual YAML files):
```bash
./d2k8s.py <docker-compose-file> <output-directory>
```

The tool will:
1. Use Kompose to convert the Docker Compose file to Kubernetes manifests
2. Process any build directives to include Dockerfile configurations
3. Convert underscores to dashes in resource names
4. Output the final Kubernetes manifests either to stdout or to individual files in the specified directory

## Examples

Output to stdout:
```bash
./d2k8s.py docker-compose.yml > k8s-manifests.yaml
```

Output to a directory:
```bash
./d2k8s.py docker-compose.yml k8s/
``` 