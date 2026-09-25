#!/usr/bin/env python3
"""Tomislav-RetCtx: digest-preserving copy of registry images between the two clusters' local registries (no skopeo).
export: fetch each image's manifest (raw bytes) and blobs from a Registry v2 API into DIR (blobs stored once).
import: push the blobs and the raw manifests into another registry; the manifest bytes are unchanged, so every
image keeps its sha256 digest and the pinned image refs in plans / manifests resolve unchanged.
usage: registry_copy.py export DIR REF [REF ...]           (REF = host:port/repo@sha256:...)
       registry_copy.py import DIR [--registry host:port]   (default: the registry named in each ref)
       registry_copy.py check REF [REF ...]                  (exit 1 if any manifest is missing)
       registry_copy.py tag DIR REF TAG                      (also publish exported REF's manifest under TAG)"""
import hashlib, json, os, sys, urllib.request, urllib.error
ACCEPT = ', '.join(['application/vnd.docker.distribution.manifest.v2+json',
                    'application/vnd.docker.distribution.manifest.list.v2+json',
                    'application/vnd.oci.image.manifest.v1+json', 'application/vnd.oci.image.index.v1+json'])
LISTS = ('application/vnd.docker.distribution.manifest.list.v2+json', 'application/vnd.oci.image.index.v1+json')
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def req(url, method='GET', data=None, headers=None):
    return opener.open(urllib.request.Request(url, data=data, method=method, headers=headers or {}), timeout=600)

def split(ref):
    host, rest = ref.split('/', 1); repo, digest = rest.split('@'); return host, repo, digest

def export(out, refs):
    os.makedirs(f'{out}/blobs', exist_ok=True); os.makedirs(f'{out}/manifests', exist_ok=True)
    index = json.load(open(f'{out}/index.json')) if os.path.exists(f'{out}/index.json') else []
    def get_manifest(host, repo, digest):
        with req(f'http://{host}/v2/{repo}/manifests/{digest}', headers={'Accept': ACCEPT}) as r:
            body, ctype = r.read(), r.headers['Content-Type']
        assert 'sha256:' + hashlib.sha256(body).hexdigest() == digest, (repo, digest)
        open(f'{out}/manifests/{digest[7:]}', 'wb').write(body)
        m = json.loads(body); entries = [(repo, digest, ctype)]
        if ctype in LISTS:
            for child in m['manifests']:
                entries = get_manifest(host, repo, child['digest']) + entries  # children before the list
            return entries
        for blob in [m['config']] + m['layers']:
            path = f"{out}/blobs/{blob['digest'][7:]}"
            if os.path.exists(path) and os.path.getsize(path) == blob['size']: continue
            h = hashlib.sha256()
            with req(f"http://{host}/v2/{repo}/blobs/{blob['digest']}") as r, open(path + '.part', 'wb') as f:
                while True:
                    chunk = r.read(1 << 20)
                    if not chunk: break
                    h.update(chunk); f.write(chunk)
            assert 'sha256:' + h.hexdigest() == blob['digest'], blob
            os.rename(path + '.part', path)
        return entries
    for ref in refs:
        if any(e['ref'] == ref for e in index): continue
        host, repo, digest = split(ref)
        entries = get_manifest(host, repo, digest)
        index.append({'ref': ref, 'entries': [{'repo': r, 'digest': d, 'type': t} for r, d, t in entries]})
        json.dump(index, open(f'{out}/index.json', 'w'), indent=1)
        print('exported', ref, flush=True)

def blobs_of(out, digest):
    m = json.loads(open(f'{out}/manifests/{digest[7:]}', 'rb').read())
    return [] if 'manifests' in m else [b['digest'] for b in [m['config']] + m['layers']]

def exists(host, repo, kind, digest, accept=None):
    try:
        req(f'http://{host}/v2/{repo}/{kind}/{digest}', method='HEAD', headers={'Accept': accept} if accept else {}).close(); return True
    except urllib.error.HTTPError as err:
        if err.code == 404: return False
        raise

def import_(out, registry=None):
    for item in json.load(open(f'{out}/index.json')):
        host = registry or split(item['ref'])[0]
        for e in item['entries']:
            repo, digest = e['repo'], e['digest']
            for b in blobs_of(out, digest):
                if exists(host, repo, 'blobs', b): continue
                with req(f'http://{host}/v2/{repo}/blobs/uploads/', method='POST', data=b'') as r:
                    loc = r.headers['Location']
                loc = loc if loc.startswith('http') else f'http://{host}{loc}'
                data = open(f'{out}/blobs/{b[7:]}', 'rb').read()
                req(f"{loc}{'&' if '?' in loc else '?'}digest={b}", method='PUT', data=data,
                    headers={'Content-Type': 'application/octet-stream', 'Content-Length': str(len(data))}).close()
            body = open(f'{out}/manifests/{digest[7:]}', 'rb').read()
            with req(f'http://{host}/v2/{repo}/manifests/{digest}', method='PUT', data=body, headers={'Content-Type': e['type']}) as r:
                assert r.headers.get('Docker-Content-Digest', digest) == digest, (repo, dict(r.headers))
        print('imported', item['ref'], flush=True)

def check(refs):
    missing = [r for r in refs if not exists(*split(r)[:2], 'manifests', split(r)[2], ACCEPT)]
    for r in missing: print('MISSING', r)
    print(f'{len(refs) - len(missing)}/{len(refs)} present'); return not missing

def tag(out, ref, name):
    host, repo, digest = split(ref)
    entry = next(e for item in json.load(open(f'{out}/index.json')) for e in item['entries'] if e['digest'] == digest)
    body = open(f'{out}/manifests/{digest[7:]}', 'rb').read()
    with req(f'http://{host}/v2/{repo}/manifests/{name}', method='PUT', data=body, headers={'Content-Type': entry['type']}) as r:
        assert r.headers.get('Docker-Content-Digest', digest) == digest, (repo, dict(r.headers))
    print('tagged', f'{host}/{repo}:{name}', '->', digest)

if __name__ == '__main__':
    if sys.argv[1] == 'export': export(sys.argv[2], sys.argv[3:])
    elif sys.argv[1] == 'import': import_(sys.argv[2], sys.argv[4] if len(sys.argv) > 4 and sys.argv[3] == '--registry' else None)
    elif sys.argv[1] == 'check': sys.exit(0 if check(sys.argv[2:]) else 1)
    elif sys.argv[1] == 'tag': tag(sys.argv[2], sys.argv[3], sys.argv[4])
    else: sys.exit(__doc__)
