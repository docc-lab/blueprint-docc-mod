"""Exercise posts/timelines and verify the live deployment and its trace pipeline."""
import concurrent.futures
import datetime as dt
import json
from pathlib import Path
import subprocess
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent
CONFIG = json.loads((ROOT / 'build.json').read_text())
NS, VARIANT = CONFIG['namespace'], CONFIG['variant']
BASE = json.loads((ROOT / 'seed.json').read_text())['api_url']
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def kube(*args):
    return json.loads(subprocess.check_output(['kubectl', '--request-timeout=10s', '-n', NS,
                                              *args, '-o', 'json']))


def get(url):
    with OPENER.open(url, timeout=20) as response:
        return json.load(response)


def request(method, **params):
    start = time.perf_counter()
    response = get(BASE + '/' + method + '?' + urllib.parse.urlencode(params))
    error = response.get('Ret2') if method == 'ComposePost' else response.get('Ret1')
    assert not error, (method, response)
    return {'method': method, 'latency_ms': round((time.perf_counter() - start) * 1000, 2),
            'response': response}


started = time.time_ns() // 1000
requests = []
posts = []
for index in range(3):
    result = request('ComposePost', userId=1, username='username_1', post_type=0,
                     text=f'Kubernetes deployment smoke {index} @username_2 https://example.com/social-smoke/{index}',
                     media_types=json.dumps(['png']), media_ids=json.dumps([1001 + index]))
    assert result['response']['Ret0'] > 0, result
    assert 2 in result['response']['Ret1'], result
    posts.append(result['response']['Ret0'])
    requests.append(result)
assert len(set(posts)) == len(posts), ('Duplicate post IDs', posts)

with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    futures = [pool.submit(request, method, userId=user, start=0, stop=10)
               for _ in range(4) for method, user in
               [('ReadUserTimeline', 1), ('ReadHomeTimeline', 2)]]
    for future in futures:
        result = future.result()
        assert set(posts) <= set(result['response']['Ret0']), result
        requests.append(result)
ended = time.time_ns() // 1000
(ROOT / 'verification-responses.json').write_text(json.dumps(requests, indent=2) + '\n')
print(f'{len(requests)} API checks passed: three posts and eight concurrent timeline reads', flush=True)

pods_doc = kube('get', 'pods')
pods = pods_doc['items']
assert len(pods) == 35, len(pods)
assert all(any(c['type'] == 'Ready' and c['status'] == 'True'
               for c in p['status'].get('conditions', [])) for p in pods)
collectors = {p['spec']['nodeName'] for p in pods if p['metadata']['name'].startswith('otelcol-')}
assert len(collectors) == 9
apps = [p for p in pods if '-service-' in p['metadata']['name']]
assert {p['spec']['nodeName'] for p in apps} <= collectors
assert kube('get', 'service', f'otelcol-{VARIANT}-ctr')['spec']['internalTrafficPolicy'] == 'Local'

def digest(image):
    registry, name = image.split('/', 1)
    repository, tag = name.rsplit(':', 1)
    req = urllib.request.Request(f'http://{registry}/v2/{repository}/manifests/{tag}', method='HEAD',
        headers={'Accept': 'application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json'})
    with OPENER.open(req, timeout=15) as response:
        return response.headers['Docker-Content-Digest']


images = {c['image'] for p in pods for c in p['spec']['containers']}
digests = {image: digest(image) for image in sorted(images)}
for p in pods:
    actual = {c['name']: c for c in p['status']['containerStatuses']}
    for container in p['spec']['containers']:
        assert actual[container['name']]['imageID'].endswith('@' + digests[container['image']]), p['metadata']['name']
(ROOT / 'registry-images.json').write_text(json.dumps(digests, indent=2) + '\n')
(ROOT / 'verification-pods.json').write_text(json.dumps(pods_doc, indent=2) + '\n')

jaeger = next(p['status']['podIP'] for p in pods if p['metadata']['name'].startswith('jaeger-'))
service = f'unknown_service:wrk2api_service_{VARIANT.replace("-", "_")}_proc'
url = f'http://{jaeger}:16686/api/traces?' + urllib.parse.urlencode({
    'service': service, 'start': started, 'end': ended, 'limit': 100})
traces = []
deadline = time.monotonic() + 45
while time.monotonic() < deadline:
    data = get(url)
    assert not data.get('errors'), data.get('errors')
    traces = data['data'] or []
    if len(traces) >= len(requests):
        break
    time.sleep(2)
assert len(traces) == len(requests), (len(traces), len(requests))
names = get(f'http://{jaeger}:16686/api/services')['data']
names = [name for name in names if '_service_' in name and 'tracepressure' not in name]
assert len(names) == 13, names
assert any(len(t['processes']) >= 11 for t in traces), 'No composed-post trace spanning its service graph'

max_depth = 0
for trace in traces:
    spans = {s['spanID']: s for s in trace['spans']}
    memo = {}
    def depth(sid):
        if sid in memo:
            return memo[sid]
        parents = [r['spanID'] for r in spans[sid]['references'] if r['refType'] == 'CHILD_OF']
        assert all(parent in spans for parent in parents), 'Trace has a missing parent'
        memo[sid] = 1 + max((depth(parent) for parent in parents), default=0)
        return memo[sid]
    max_depth = max(max_depth, max(map(depth, spans)))

(ROOT / 'verification-traces.json').write_text(json.dumps(traces, indent=2) + '\n')
summary = {'verified_at': dt.datetime.now(dt.timezone.utc).isoformat(), 'namespace': NS,
    'frontend_url': BASE, 'ready_pods': len(pods), 'application_services': 13,
    'auxiliary_tracepressure_services': 1, 'collector_pods': len(collectors),
    'successful_api_requests': len(requests), 'created_post_ids': posts,
    'smoke_traces': len(traces), 'maximum_smoke_trace_depth_spans': max_depth,
    'jaeger_services': names, 'registry_images_verified': len(digests),
    'pods_match_registry_digests': True,
    'total_restarts': sum(c['restartCount'] for p in pods for c in p['status']['containerStatuses'])}
(ROOT / 'verification.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary, indent=2), flush=True)
