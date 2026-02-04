# Utils

Utility scripts for blueprint-docc-mod.

## pin_nodes.py

Pins Kubernetes deployments to specific nodes and sets CPU resource requests/limits using a node-pinning YAML file.

**Requirements:** PyYAML (`pip install pyyaml`)

**Usage:**

```bash
python pin_nodes.py <pinning.yaml> <k8s-dir> [--dry-run]
```

- **pinning.yaml** – Path to a node-pinning config (e.g. `examples/dsb_sn/node-pinning-sb.yaml`). Format: top-level keys are node names (`kubernetes.io/hostname`); each value is a list of `service-name: { requests_cpu: N, limits_cpu?: N }` (CPU in millicores).
- **k8s-dir** – Directory containing `*-deployment.yaml` files (e.g. `examples/dsb_sn/build_sb/k8s`).
- **--dry-run** – Print planned changes without modifying files.

**Example:**

```bash
cd blueprint-docc-mod
python utils/pin_nodes.py examples/dsb_sn/node-pinning-sb.yaml examples/dsb_sn/build_sb/k8s
```

Then apply (or re-apply) the manifests:

```bash
kubectl apply -f examples/dsb_sn/build_sb/k8s/
```
