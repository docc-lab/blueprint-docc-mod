"""Build identical SDK/application images for all checkpoint-distance phases."""
import concurrent.futures
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
BUILD = REPO / 'examples/dsb_sn/build_cgpb_es_ttl_20260910'
CONFIG = json.loads((ROOT / 'experiment.json').read_text())
DEPLOYMENTS = json.loads((ROOT / 'before-deployments.json').read_text())['items']
APPS = [d for d in DEPLOYMENTS if '-service-' in d['metadata']['name']]
assert len(APPS) == 14
for relative, expected in json.loads((ROOT / 'workflow-source-hashes.json').read_text()).items():
    assert hashlib.sha256((REPO / relative).read_bytes()).hexdigest() == expected, relative

images = {}
contexts = {}
for deployment in APPS:
    name = deployment['metadata']['name']
    context = BUILD / 'docker' / name.replace('-', '_')
    assert context.is_dir(), context
    for filename in ['checkpoint_distance.go', 'cgpb_processor.go', 'pb_processor.go', 'sb_processor.go']:
        matches = list(context.glob(f'*/runtime/plugins/otelcol/{filename}'))
        assert len(matches) == 1, matches
        assert matches[0].read_bytes() == (REPO / 'runtime/plugins/otelcol' / filename).read_bytes(), matches[0]
    # Share Go's module/build caches across these generated process images.
    # Only generated build instructions change; application source stays intact.
    path = context / 'Dockerfile'
    dockerfile = path.read_text()
    if '--mount=type=cache' not in dockerfile:
        dockerfile = dockerfile.replace('RUN go mod download',
            'RUN --mount=type=cache,id=dsb-sn-gomod,target=/go/pkg/mod go mod download')
        dockerfile = dockerfile.replace('RUN go build ',
            'RUN --mount=type=cache,id=dsb-sn-gomod,target=/go/pkg/mod '
            '--mount=type=cache,id=dsb-sn-gobuild,target=/root/.cache/go-build go build ')
        path.write_text(dockerfile)
    images[name] = f'10.10.1.1:30000/{name}:{CONFIG["application_image_tag"]}'
    contexts[name] = context
(ROOT / 'application-images.json').write_text(json.dumps(images, indent=2) + '\n')
(ROOT / 'app-build-logs').mkdir(exist_ok=True)


def build(name):
    with (ROOT / 'app-build-logs' / f'{name}.log').open('w') as log:
        subprocess.run(['docker', 'build', '--progress=plain', '-t', images[name], str(contexts[name])],
                       stdout=log, stderr=subprocess.STDOUT, check=True)
        subprocess.run(['docker', 'push', images[name]], stdout=log, stderr=subprocess.STDOUT, check=True)
    return name


with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    futures = {pool.submit(build, name): name for name in images}
    for completed, future in enumerate(concurrent.futures.as_completed(futures), 1):
        print(f'{completed}/14 built and pushed: {future.result()}', flush=True)
print('All application images contain the current SDK; workflow source is unchanged.', flush=True)
