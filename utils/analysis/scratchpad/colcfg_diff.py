"""Tomislav-RetCtx: diff collector (agent + gateway) configs and container specs between two derived manifests, names normalized."""
import sys, yaml, re, json, difflib
def load(path):
    docs = [d for d in yaml.safe_load_all(open(path)) if d]
    out = {}
    for d in docs:
        kind, name = d.get('kind'), d.get('metadata', {}).get('name', '')
        base = re.sub(r'-(v|pb|cgpb|sb|nt)-es-[a-z0-9]+-(ctr|config)$', '', name)
        base = re.sub(r'-(hotel-)?(v|pb|cgpb|sb|nt)-esx?[a-z0-9]+(-ctr|-config)?', '', base)
        if not any(k in base for k in ('otelcol', 'otelgw', 'clickhouse')):
            continue
        if kind == 'ConfigMap':
            for k, v in (d.get('data') or {}).items():
                out[f'ConfigMap {base} {k}'] = v
        elif kind in ('DaemonSet', 'Deployment'):
            for c in d['spec']['template']['spec']['containers']:
                spec = {'image': c.get('image'), 'args': c.get('args'), 'resources': c.get('resources'),
                        'env': sorted((e['name'], str(e.get('value'))) for e in c.get('env', []) if 'value' in e)}
                out[f'{kind} {base} container {c["name"].split("-")[0]}'] = json.dumps(spec, indent=1, sort_keys=True)
    return out
def norm(s):
    s = re.sub(r'(hotel-)?(v|pb|cgpb|sb|nt)-esx?-?(nwx)?[a-z0-9]+', 'NAME', s)
    s = re.sub(r'hotel-(v|pb|cgpb|sb)-esx[a-z0-9]+', 'NAME', s)
    return s
a, b = load(sys.argv[1]), load(sys.argv[2])
print('keys only in A:', sorted(set(a) - set(b))); print('keys only in B:', sorted(set(b) - set(a)))
for k in sorted(set(a) & set(b)):
    x, y = norm(a[k]).splitlines(), norm(b[k]).splitlines()
    d = [l for l in difflib.unified_diff(x, y, 'A', 'B', lineterm='', n=0) if not l.startswith(('---', '+++', '@@'))]
    print(f'== {k}: ' + ('IDENTICAL' if not d else f'{len(d)} differing lines'))
    for l in d[:40]: print('   ', l)
