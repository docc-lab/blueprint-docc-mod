# d2k8s

A tool for converting Docker Compose applications to Kubernetes manifests, with additional enhancements for handling Dockerfile configurations.

## Features

- Converts Docker Compose files to Kubernetes manifests using Kompose
- Handles Dockerfile configurations through build directives
- Converts underscores to dashes in Kubernetes resource names
- Preserves build context and arguments
- Writes individual YAML files to a specified output directory

## Requirements

- Python 3.6+
- Kompose installed and available in `PATH`
- Docker (for build/push/pull unless you pass `--skip-build`)
- PyYAML (`pip install -r requirements.txt`)

## Installation

1. Install dependencies (virtual environment recommended):

```bash
pip install -r requirements.txt
```

2. Optionally make the script executable:

```bash
chmod +x d2k8s.py
```

## Usage

```text
d2k8s.py <docker-compose-file> <output-dir> --registry <REGISTRY_URL> [options]
```

| Argument / option | Description |
|-------------------|-------------|
| `docker-compose-file` | Path to the Compose file |
| `output-dir` | Directory for generated manifests (created if needed) |
| `--registry` | Registry host (and optional port), e.g. `10.10.1.1:30000` |
| `--skip-build` | Only run Kompose and manifest post-processing; skip Docker build/push |
| `--daemon-services` | Comma-separated service names to turn into DaemonSets |
| `--services` | Comma-separated service names to build/push (default: all) |

Flow: Kompose convert → rewrite deployment images for `--registry` → post-process YAML (underscores, optional DaemonSets) → unless `--skip-build`, build/push images from the Compose project.

### Compose variables and `.env`

Kompose evaluates Compose the same way as Docker Compose: `${VAR?...}` and similar interpolations need those variables in the **environment** of the `kompose` process. A `.env` next to your project layout is **not** automatically loaded unless you export it.

Typical pattern (no `grep`): load the build `.env`, then run `d2k8s.py` in one line:

```bash
export $(cat /path/to/build_test/.env | xargs) && \
  /path/to/d2k8s/.venv/bin/python /path/to/d2k8s/d2k8s.py \
  /path/to/build_test/docker/docker-compose.yml \
  /path/to/build_test/k8s \
  --registry 10.10.1.1:30000 \
  --daemon-services otelcol-pb-ctr
```

Use the real path to the `.env` that defines bind addresses and other substituted values (for example `examples/dsb_sn/build_test/.env` in this repository).

`--services` must match **Compose `services:` keys** exactly. `--daemon-services` must match the **Kompose `*-deployment.yaml` basename** (the part before `-deployment.yaml`) at conversion time—usually the same spelling as the Compose service name; use underscores or dashes consistently with what Kompose emitted for your file.

## Example (dsb_sn build_test)

```bash
export $(cat /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_test/.env | xargs) && \
  /users/tomislav/blueprint-docc-mod/d2k8s/.venv/bin/python \
  /users/tomislav/blueprint-docc-mod/d2k8s/d2k8s.py \
  /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_test/docker/docker-compose.yml \
  /users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_test/k8s \
  --registry 10.10.1.1:30000 \
  --daemon-services otelcol-pb-ctr
```

Adjust the output directory and `--daemon-services` list to match your project.
