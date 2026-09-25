import json, glob, os
H = '/storage/retctx-handoff/state'
def roots(f):
    try: return [l.strip() for l in open(f'{H}/{f}') if l.strip()]
    except OSError: return []
rows = []
for label, fname in (('morning', 'snrw_roots.txt'), ('final', 'snrw_final_roots.txt')):
    for R in roots(fname):
        for case in sorted(glob.glob(R + '/run/0*-*')):
            if case.endswith('-superseded'): continue
            rep, kind = os.path.basename(case).split('-', 1)
            for rate in (2500, 2800, 3000, 3200):
                P = f'{case}/rate-{rate:05d}'
                if not os.path.exists(f'{P}/after/snapshot.json'): continue
                a = json.load(open(f'{P}/before/snapshot.json'))['cpu']; b = json.load(open(f'{P}/after/snapshot.json'))['cpu']; r = json.load(open(f'{P}/result.json'))
                def c(prefix): return sum((b[k]['cpu_ns'] - a[k]['cpu_ns']) / 30e9 for k in b if k in a and b[k]['name'].startswith(prefix))
                db = c('usertimeline-db'); n = r['completed_rps']
                rows.append((label, kind, rep, rate, n, r['mean_ms'], r['p99_ms'], db, 1000 * db / n, c('usertimeline-service'), c('composepost')))
print('run     kind rep  rate  got  mean   p99 | usertimeline-db cores  ms-CPU/req | ut-service  composepost')
for x in rows:
    print(f'{x[0]:7s} {x[1]:4s} {x[2]} {x[3]:5d} {x[4]:5.0f} {x[5]:6.1f} {x[6]:6.1f} | {x[7]:6.2f}  {x[8]:5.2f} | {x[9]:5.2f} {x[10]:5.2f}')
