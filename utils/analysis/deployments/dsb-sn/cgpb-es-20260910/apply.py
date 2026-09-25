"""Check every registry image, apply this namespace's manifests, and await readiness."""
import json
from pathlib import Path
import subprocess
import urllib.request

import yaml

ROOT = Path(__file__).resolve().parent
CONFIG = json.loads((ROOT / 'build.json').read_text())
MANIFESTS = (ROOT / 'k8s').resolve()
docs = [yaml.safe_load(path.read_text()) for path in sorted(MANIFESTS.glob('*.yaml'))]
workloads = [doc for doc in docs if doc['kind'] in ('Deployment', 'DaemonSet')]
images = {container['image'] for doc in workloads for container in doc['spec']['template']['spec']['containers']}
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
digests = {}
for image in sorted(images):
    registry, name = image.split('/', 1)
    repository, tag = name.rsplit(':', 1)
    request = urllib.request.Request(f'http://{registry}/v2/{repository}/manifests/{tag}', method='HEAD',
        headers={'Accept': 'application/vnd.docker.distribution.manifest.v2+json'})
    with opener.open(request, timeout=10) as response:
        digests[image] = response.headers['Docker-Content-Digest']
(ROOT / 'registry-images-built.json').write_text(json.dumps(digests, indent=2) + '\n')
print(f'Verified {len(digests)} pushed images', flush=True)
subprocess.run(['kubectl', '--request-timeout=15s', 'apply', '-f', str(MANIFESTS)], check=True)
for doc in workloads:
    subprocess.run(['kubectl', '--request-timeout=10s', '-n', CONFIG['namespace'], 'rollout', 'status',
        f"{doc['kind'].lower()}/{doc['metadata']['name']}", '--timeout=300s'], check=True)
print('All social-network workloads rolled out successfully.', flush=True)
