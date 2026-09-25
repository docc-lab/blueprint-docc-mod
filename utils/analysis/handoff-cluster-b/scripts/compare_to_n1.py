#!/usr/bin/env python3
"""Tomislav-RetCtx: a derived root must equal its n=1 reference root except for what a protocol change is allowed to
change. Compares plan.json (minus protocol keys) and the kind's manifest STRUCTURALLY: every Kubernetes document is
parsed, embedded collector configs (ConfigMap data) are parsed as YAML, and only the following are normalized away:
build stamps (2026MMDDTHHMM[SS]Z in any case), raw-data paths, Blueprint build ids, app service image digests
(--new-images), and the discovery key reverse_passthrough (--passthrough, which must then be true in the new root).
usage: compare_to_n1.py ROOT N1_ROOT KIND [--passthrough] [--new-images] [--app-only]   (exit 1 + the differences on mismatch)
--app-only (untraced kind whose n=1 root predates the current tracing stack): every APPLICATION document must be identical;
tracing-infrastructure documents (collectors, gateway, trace backend) and the plan's tracing-stack keys are not compared."""
import json, re, sys
import yaml

R, N1, KIND = sys.argv[1:4]
PT, NEW, APPONLY = '--passthrough' in sys.argv, '--new-images' in sys.argv, '--app-only' in sys.argv
INFRA = re.compile(r'^(otelcol|otelgw|clickhouse|jaeger|jaegershim|elasticsearch)-')
INFRA_KEYS = {'gateway_lp_refusal', 'us_margin_mode', 'clickhouse', 'backend_tuning', 'elasticsearch_cpus', 'collector_cpu',
              'collector_gomaxprocs', 'backend', 'jaeger_cpus', 'agent_compression', 'collector_profile', 'collector_image_override',
              'gateway_cpu', 'store', 'collector_pipeline'}
SKIP = {'created', 'note', 'ramp_rates', 'raw_data_storage', 'ramp_passes', 'plateau_stop', 'knee_windows', 'derived_from',
        'cases', 'case_order_note', 'discovery_override', 'repetitions', 'seeds', 'repeat_grid', 'trace_capture'}


def text(s):
    s = re.sub(r'2026\d{4}[tT]\d{4,6}[zZ]', 'STAMP', s)
    s = re.sub(r'/storage/tomislav-retctx-e2e/[^\s"\']+', 'RAW', s)
    s = re.sub(r'"blueprint\.uservices/build": "\d+"', '"blueprint.uservices/build": "BUILD"', s)
    if NEW:
        s = re.sub(r'(-service-[A-Za-z0-9-]+?-ctr)@sha256:[0-9a-f]{64}', r'\1@APP', s)
    return s


def collector(cfg):
    cm = (cfg.get('receivers') or {}).get('configdiscovery', {}).get('config_map')
    if isinstance(cm, dict) and PT:
        cm.pop('reverse_passthrough', None)  # presence in the NEW root is checked on its raw manifest
    return cfg


def norm_doc(doc):
    doc = json.loads(text(json.dumps(doc)))
    for k in list((doc.get('metadata') or {}).get('labels') or {}):
        if k == 'blueprint.uservices/build':
            doc['metadata']['labels'][k] = 'BUILD'
    tmpl = ((doc.get('spec') or {}).get('template') or {}).get('metadata') or {}
    if 'blueprint.uservices/build' in (tmpl.get('labels') or {}):
        tmpl['labels']['blueprint.uservices/build'] = 'BUILD'
    if doc.get('kind') == 'ConfigMap':
        for key, value in list((doc.get('data') or {}).items()):
            try:
                parsed = yaml.safe_load(value)
            except yaml.YAMLError:
                continue
            if isinstance(parsed, dict) and 'receivers' in parsed:
                doc['data'][key] = collector(parsed)
    return json.dumps(doc, sort_keys=True)


def docs(path):
    return sorted(norm_doc(d) for d in yaml.safe_load_all(open(path))
                  if d and not (APPONLY and INFRA.match((d.get('metadata') or {}).get('name', ''))))


errors = []
a, b = json.load(open(f'{N1}/plan.json')), json.load(open(f'{R}/plan.json'))
diff = {x: (a.get(x), b.get(x)) for x in set(a) | set(b) if x not in SKIP and not (APPONLY and x in INFRA_KEYS) and a.get(x) != b.get(x)}
if diff:
    errors.append(f'plan differs: {diff}')
m_new = open(f'{R}/builds/{KIND}/manifest.yaml').read()
if PT and 'reverse_passthrough: true' not in m_new:
    errors.append('new manifest lacks reverse_passthrough: true')
da, db = docs(f'{N1}/builds/{KIND}/manifest.yaml'), docs(f'{R}/builds/{KIND}/manifest.yaml')
if da != db:
    only_a, only_b = [x for x in da if x not in db], [x for x in db if x not in da]
    errors.append(f'manifest: {len(only_a)} documents only in n=1, {len(only_b)} only in new')
    for x, y in zip(only_a[:2], only_b[:2]):
        import difflib
        errors.extend(l for l in difflib.unified_diff(json.dumps(json.loads(x), indent=1, sort_keys=True).splitlines(),
                                                      json.dumps(json.loads(y), indent=1, sort_keys=True).splitlines(),
                                                      lineterm='', n=0) if l[:1] in '+-' and not l.startswith(('+++', '---')))
if errors:
    print(f'{KIND}: MISMATCH vs {N1.split("/")[-1]}'); print('\n'.join(errors[:40])); sys.exit(1)
print(f'{KIND}: plan + manifest match n=1 ({N1.split("/")[-1]}) except rates/passes'
      + (' / passthrough' if PT else '') + (' / app image digests' if NEW else '') + (' (application documents only)' if APPONLY else ''))
