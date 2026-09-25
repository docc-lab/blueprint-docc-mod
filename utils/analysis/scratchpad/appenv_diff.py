"""Tomislav-RetCtx: tracing-related env on application containers, hotel vs SN manifest (union over -service- containers)."""
import sys, yaml, re
def envs(path):
    out = {}
    for d in yaml.safe_load_all(open(path)):
        if not d or d.get('kind') not in ('Deployment', 'DaemonSet'): continue
        for c in d['spec']['template']['spec']['containers']:
            if '-service' not in c['name']: continue
            for e in c.get('env', []):
                if 'value' not in e: continue
                n, v = e['name'], str(e['value'])
                if re.search(r'_(DIAL|BIND)_ADDR$|HOSTNAME|_PORT$', n): continue
                out.setdefault(n, set()).add(v)
    return out
a, b = envs(sys.argv[1]), envs(sys.argv[2])
for n in sorted(set(a) | set(b)):
    x, y = a.get(n), b.get(n)
    if x != y: print(f'{n}: hotel={sorted(x) if x else None} sn={sorted(y) if y else None}')
    else: print(f'{n}: same {sorted(x)}')
