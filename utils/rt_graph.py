#!/usr/bin/env python3
# Collect BRIDGES_RT counters from all pods, check invariant, plot per-node checkpoints
import re, subprocess, sys, os
NO_INV = "--no-invariant" in sys.argv or os.environ.get("RT_NO_INVARIANT") == "1"
import matplotlib; matplotlib.use("Agg")
import matplotlib.pyplot as plt

pods = subprocess.run(["kubectl","get","pods","-o","name"], capture_output=True, text=True).stdout.split()
per_node, recv, reject = {}, 0, 0
def val(line, key):
    m = re.search(key + r'[=" ]+(\d+)', line); return int(m.group(1)) if m else 0
for p in pods:
    logs = subprocess.run(["kubectl","logs",p,"--tail=8"], capture_output=True, text=True).stdout
    hits = [l for l in logs.splitlines() if "BRIDGES_RT" in l]
    if not hits: continue
    last = hits[-1]; name = p.split("/")[-1]
    per_node[name] = val(last,"checkpoints"); recv += val(last,"received"); reject += val(last,"leaf_rejects")

print(f"total leaf_rejects={reject}   total trusses_received={recv}")
if not NO_INV and reject != recv:
    print('Something horribly went wrong: leaf_rejects != trusses_received')
names=list(per_node); vals=[per_node[n] for n in names]
plt.figure(figsize=(12,5))
plt.bar(range(len(names)), vals)
plt.xticks(range(len(names)), names, rotation=90, fontsize=6)
plt.ylabel("reverse checkpoints")
plt.title("Reverse checkpoints per node")
plt.tight_layout()
plt.savefig("rt_checkpoints.png")
print("wrote rt_checkpoints.png")