"""Tomislav-RetCtx: run-stage events for the monitor: prints a line on every case/stage change (deploy, warm-up,
measuring, trace settle, final capture, complete) and, when called with --heartbeat, the current stage with pod
readiness during deploys. usage: snnw_status.py <state-file> [--heartbeat] root..."""
import sys, json, os, subprocess, datetime as t
state_file, args = sys.argv[1], sys.argv[2:]
heartbeat = '--heartbeat' in args; roots = [a for a in args if a != '--heartbeat']
try: prev = json.load(open(state_file))
except Exception: prev = {}
now = t.datetime.now(t.timezone.utc)
def pods(ns='dsb-sn'):
    try:
        out = subprocess.run(['kubectl', '-n', ns, 'get', 'pods', '--no-headers'], capture_output=True, text=True, timeout=20).stdout.splitlines()
        ready = sum(1 for l in out if l.split()[1].split('/')[0] == l.split()[1].split('/')[1] and l.split()[2] == 'Running')
        return f'pods ready {ready}/{len(out)}'
    except Exception as e:
        return f'pods ? ({e.__class__.__name__})'
lines = []
for R in roots:
    p = f'{R}/run-status.json'
    if not os.path.exists(p): continue
    try: d = json.load(open(p))
    except Exception: continue
    key = f"{d.get('state')}|{d.get('case')}|{d.get('stage')}"
    name = os.path.basename(R)
    age = (now - t.datetime.fromisoformat(d['updated'])).total_seconds() / 60 if d.get('updated') else 0
    if d.get('state') == 'running':
        what = f"{d.get('case')}: {d.get('stage')}" + (f" at {d['offered_rps']}" if d.get('stage') == 'measuring' else '')
    else:
        what = f"{d.get('state')}"
    if prev.get(R) != key and not (d.get('stage') == 'measuring' and (prev.get(R) or '').endswith('|measuring')):
        extra = f" | {pods('dsb-hotel' if '/dsb-hotel/' in R else 'dsb-sn')}" if d.get('stage') in ('deploy', 'warmup') else ''
        lines.append(f"STAGE {now:%H:%M:%S} {name} {what}{extra}")
    elif heartbeat and d.get('state') == 'running':
        extra = f" | {pods('dsb-hotel' if '/dsb-hotel/' in R else 'dsb-sn')}" if d.get('stage') == 'deploy' else ''
        lines.append(f"STATUS {now:%H:%M:%S} {name} {what} (in this stage {age:.1f} min){extra}")
    prev[R] = key
json.dump(prev, open(state_file, 'w'))
for l in lines: print(l, flush=True)
