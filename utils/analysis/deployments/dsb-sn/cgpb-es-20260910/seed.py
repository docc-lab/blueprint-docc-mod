"""Seed Reed98, checking every response and avoiding concurrent cache updates."""
import asyncio
from collections import Counter
import importlib.util
import json
from pathlib import Path
import subprocess
import time

import aiohttp

ROOT = Path(__file__).resolve().parent
REPO = Path('/users/tomislav/blueprint-docc-mod')
DATA = REPO / 'examples/dsb_sn/scripts/datasets/social-graph/socfb-Reed98'
SPEC = importlib.util.spec_from_file_location('seed_api', REPO / 'examples/dsb_sn/scripts/init_social_graph.py')
API = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(API)
config = json.loads((ROOT / 'build.json').read_text())
service = json.loads(subprocess.check_output(['kubectl', '--request-timeout=10s', '-n',
    config['namespace'], 'get', 'service', f"wrk2api-service-{config['variant']}-ctr", '-o', 'json']))
port = next(p['nodePort'] for p in service['spec']['ports'] if p['port'] == 2000)
base = f'http://10.10.1.1:{port}'
nodes = int((DATA / 'socfb-Reed98.nodes').read_text())
edges = [line.split() for line in (DATA / 'socfb-Reed98.edges').read_text().splitlines() if line.strip()]
report = {'graph': 'socfb-Reed98', 'api_url': base, 'users': 0,
          'directed_follows': 0, 'completed': False, 'started_at_epoch': time.time()}


def check(results):
    counts = Counter(results)
    if set(counts) != {'Success'}:
        raise RuntimeError(dict(counts))


def save():
    (ROOT / 'seed.json').write_text(json.dumps(report, indent=2) + '\n')


def initialize_collections():
    pods = json.loads(subprocess.check_output(['kubectl', '--request-timeout=10s', '-n',
        config['namespace'], 'get', 'pods', '-o', 'json']))['items']
    for prefix, name in [('user-db-', 'user'), ('social-db-', 'social-graph'),
                         ('post-db-', 'post'), ('urlshorten-db-', 'url-shorten'),
                         ('usertimeline-db-', 'usertimeline')]:
        pod = next(p['metadata']['name'] for p in pods if p['metadata']['name'].startswith(prefix))
        script = ('var target=db.getSiblingDB(' + json.dumps(name) + ');'
            'var collection=' + json.dumps(name) + ';'
            'if(target.getCollectionNames().indexOf(collection)<0){'
            'var result=target.createCollection(collection);'
            'if(result.ok!==1)throw JSON.stringify(result);}'
            'print("Collection initialized: "+collection);')
        subprocess.run(['kubectl', '--request-timeout=30s', '-n', config['namespace'],
            'exec', pod, '--', 'mongo', '--quiet', '--eval', script], check=True, timeout=35)


async def seed():
    timeout = aiohttp.ClientTimeout(total=30)
    async with aiohttp.ClientSession(timeout=timeout) as session:
        for first in range(0, nodes, 8):
            ids = range(first, min(first + 8, nodes))
            check(await asyncio.gather(*(API.upload_register(session, base, str(i)) for i in ids)))
            report['users'] += len(ids)
        print(f"Registered {report['users']} users", flush=True)
        save()
        # Opposite edges update different cache keys. Finish each pair before
        # the next pair so the service's read/modify/write cache cannot lose an
        # update when several follow requests touch the same user.
        for index, (left, right) in enumerate(edges, 1):
            check(await asyncio.gather(API.upload_follow(session, base, left, right),
                                       API.upload_follow(session, base, right, left)))
            report['directed_follows'] += 2
            if index % 1000 == 0:
                print(f"Seeded {index}/{len(edges)} graph edges", flush=True)
                save()
    report['completed'] = True
    report['duration_s'] = round(time.time() - report['started_at_epoch'], 2)
    save()
    print(json.dumps(report), flush=True)


try:
    initialize_collections()
    asyncio.run(seed())
except Exception as error:
    report['error'] = str(error)
    save()
    raise
