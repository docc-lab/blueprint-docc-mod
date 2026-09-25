"""Tomislav-RetCtx: SN no-work, optimized SDK vs the n=5 matrix ON rounds: peak completed req/s and mean latency
at selected rates per kind (matrix = mean over the 5 ON rounds), plus composepost CPU at 5000."""
import glob, json, os, statistics, sys, datetime
NEW = sys.argv[1]
MATRIX = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
RATES = (1000, 3000, 5000, 6000, 6500, 7000, 7500)
def case_dir(root, kind):
    c = glob.glob(f'{root}/run/*-{kind}')
    return c[0] if c else None
def point(d, rate):
    f = f'{d}/rate-{rate:05d}/result.json'
    return json.load(open(f)) if os.path.exists(f) else None
def peak(d):
    return max((json.load(open(f))['completed_rps'] for f in glob.glob(f'{d}/rate-*/result.json')), default=None)
def cpu(d, rate, svc='composepost'):
    P = f'{d}/rate-{rate:05d}'
    try: B = json.load(open(f'{P}/before/snapshot.json')); A = json.load(open(f'{P}/after/snapshot.json'))
    except Exception: return None
    dt = (datetime.datetime.fromisoformat(A['started']) - datetime.datetime.fromisoformat(B['finished'])).total_seconds()
    for u, x in A['cpu'].items():
        if u in B['cpu'] and x['name'].startswith(svc):
            return (x['cpu_ns'] - B['cpu'][u]['cpu_ns']) / 1e9 / dt
f = lambda x, n=0: '-' if x is None else f'{x:,.{n}f}'
print(f"{'kind':8s} {'peak req/s (matrix -> new)':30s} " + ' '.join(f'{"mean ms @"+str(r):>20s}' for r in RATES) + '   composepost cores @5000')
for kind in ('nt', 'v', 'pb', 'cgpb', 'sb'):
    mdirs = [d for d in (case_dir(r, kind) for r in MATRIX) if d]
    nd = case_dir(NEW, kind)
    mp = statistics.mean(p for p in (peak(d) for d in mdirs) if p) if mdirs else None
    np_ = peak(nd) if nd else None
    cells = []
    for r in RATES:
        ms = [p['mean_ms'] for p in (point(d, r) for d in mdirs) if p]
        mm = statistics.mean(ms) if ms else None
        nn = point(nd, r)['mean_ms'] if nd and point(nd, r) else None
        cells.append(f'{f(mm,1)} -> {f(nn,1)}')
    mc = [c for c in (cpu(d, 5000) for d in mdirs) if c]
    print(f"{kind:8s} {f(mp)+' -> '+f(np_):30s} " + ' '.join(f'{c:>20s}' for c in cells) + f"   {f(statistics.mean(mc) if mc else None,2)} -> {f(cpu(nd,5000) if nd else None,2)}")
