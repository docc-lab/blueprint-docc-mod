# NUMA and Topology Manager setup for consistent latency

To reduce NUMA and scheduling noise when comparing V vs NT (and get single-NUMA placement where possible), do both: **configure the kubelet on each node** and **set pod resources** as below.

---

## 1. Configure the nodes (kubelet)

You need **CPU manager policy `static`** and **Topology Manager** enabled on every worker node that runs your workloads. How you apply this depends on how the cluster is run.

### Option A: Kubelet flags (systemd / custom kubelet)

On each worker node, ensure the kubelet is started with:

```text
--cpu-manager-policy=static
--topology-manager-policy=single-numa-node
```

**systemd example** (often in `/etc/systemd/system/kubelet.service.d/10-kubeadm.conf` or similar):

```ini
[Service]
Environment="KUBELET_EXTRA_ARGS=--cpu-manager-policy=static --topology-manager-policy=single-numa-node"
```

Then:

```bash
sudo systemctl daemon-reload
sudo systemctl restart kubelet
```

**Important:** After changing CPU manager policy, you may need to **remove the state file and restart kubelet** so the static policy is applied from a clean state (otherwise existing allocations can stick):

```bash
# On each worker node (path can differ; check your distro)
sudo rm -f /var/lib/kubelet/cpu_manager_state
sudo systemctl restart kubelet
```

### Option B: KubeletConfiguration (kubeadm or config file)

If you use a KubeletConfiguration (e.g. kubeadm `ClusterConfiguration` or a config file passed to kubelet):

```yaml
cpuManagerPolicy: static
topologyManagerPolicy: single-numa-node
```

For kubeadm, that usually lives under `kubeadm join`-generated config or in a patch. After changing it, restart the kubelet (and clear `cpu_manager_state` if you’re switching to `static` for the first time).

### Option C: Check that it’s active

On a worker node:

```bash
# Policy in use
cat /var/lib/kubelet/cpu_manager_state
# Topology Manager: run a guaranteed pod and check cgroups or describe node
kubectl describe node <node-name> | grep -A5 Topology
```

---

## 2. Pod spec: what Topology Manager needs

For **Topology Manager** to align CPU and memory to a **single NUMA node**, pods should be **Guaranteed** and use **integer CPU**:

- **CPU:** `requests.cpu` == `limits.cpu` and **whole cores** (e.g. `1`, `2`, `1000m`, `2000m`). Fractional cores (e.g. `500m`) do not get exclusive CPUs from the static policy, so topology alignment is less useful.
- **Memory:** set both `requests.memory` and `limits.memory` (same value for Guaranteed). This lets the manager consider memory when choosing the NUMA node.

Example container resources:

```yaml
resources:
  requests:
    cpu: "2"           # or 2000m
    memory: "512Mi"
  limits:
    cpu: "2"
    memory: "512Mi"
```

Use the **same** CPU and memory requests/limits for **V and NT** variants of the same service so scheduling and NUMA treatment are comparable.

---

## 3. Add memory to your deployments

Right now only CPU requests are set. Add memory so scheduling is stable and Topology Manager can align memory to the same NUMA node as CPU.

Example for a service (e.g. composepost):

```yaml
resources:
  requests:
    cpu: 10000m
    memory: "512Mi"
  limits:
    cpu: 10000m
    memory: "512Mi"
```

You can tune `memory` per service (e.g. 256Mi for small services, 512Mi–1Gi for heavier ones). Keep **requests == limits** for the pods you care about for NUMA (so they stay Guaranteed).

---

## 4. Optional: numactl in the container

If you **cannot** change kubelet (e.g. shared cluster), you can still pin the process to one NUMA node inside the container:

1. Install `numactl` in the image (e.g. `apt-get install -y numactl` in Dockerfile).
2. Run the binary via numactl, e.g. in the container’s command/entrypoint:

   ```bash
   numactl --cpunodebind=0 --membind=0 /path/to/your/binary
   ```

Use the same node (e.g. `0`) for all containers on that host if you want them to share one NUMA node. This doesn’t require Topology Manager but gives you local memory and CPU on one node.

---

## 5. Summary checklist

- [ ] On every worker node: `--cpu-manager-policy=static` and `--topology-manager-policy=single-numa-node`, then restart kubelet (and clear `cpu_manager_state` if switching to static).
- [ ] In pod specs: CPU and memory **requests == limits**, integer CPU (whole cores); same values for V and NT for the same service.
- [ ] (Optional) Add numactl in the image and run the process with `numactl --cpunodebind=N --membind=N` if you can’t change kubelet.

After that, pods that fit on a single NUMA node will get CPU and memory from one node, which should reduce NUMA and scheduling variance when comparing V vs NT.
