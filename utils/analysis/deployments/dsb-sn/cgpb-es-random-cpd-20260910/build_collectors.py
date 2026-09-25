import copy
import json
from pathlib import Path
import subprocess
import urllib.request

import yaml

ROOT = Path(__file__).resolve().parent
OLD = Path('/users/tomislav/blueprint-docc-mod/examples/dsb_sn/build_cgpb_es_k8s_20260910')
CONFIG = json.loads((ROOT / 'experiment.json').read_text())
BASE_TAG = '10.10.1.1:30000/otelcontribcol:random-cpd-20260910'
subprocess.run(['docker', 'tag', '10.10.1.1:30000/otelcontribcol:latest', BASE_TAG], check=True)
subprocess.run(['docker', 'push', BASE_TAG], check=True)
request = urllib.request.Request('http://10.10.1.1:30000/v2/otelcontribcol/manifests/random-cpd-20260910',
    method='HEAD', headers={'Accept': 'application/vnd.docker.distribution.manifest.v2+json'})
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(request, timeout=15) as response:
    base = '10.10.1.1:30000/otelcontribcol@' + response.headers['Docker-Content-Digest']
original = yaml.safe_load((OLD / 'docker/otelcol_cgpb_es_sn20260910_ctr/config.yaml').read_text())
images = {}
for phase in CONFIG['phases']:
    directory = ROOT / ('collector-' + phase['name'])
    directory.mkdir(exist_ok=True)
    config = copy.deepcopy(original)
    config['receivers']['configdiscovery']['config_map'] = phase['config_map']
    (directory / 'config.yaml').write_text(yaml.safe_dump(config, sort_keys=False))
    (directory / 'Dockerfile').write_text(f'FROM {base}\nCOPY config.yaml /etc/otelcol/config.yaml\nCMD ["--config", "/etc/otelcol/config.yaml"]\n')
    image = f'10.10.1.1:30000/otelcol-{CONFIG["variant"]}-ctr:ttl-{phase["name"]}-20260910'
    subprocess.run(['docker', 'build', '--progress=plain', '-t', image, str(directory)], check=True)
    subprocess.run(['docker', 'push', image], check=True)
    images[phase['name']] = image
(ROOT / 'collector-images.json').write_text(json.dumps({'base': base, 'phases': images}, indent=2) + '\n')
print('All collector configurations built and pushed from the same immutable base.', flush=True)
