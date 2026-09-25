"""Tomislav-RetCtx: knee-region repeats against a case that is STILL DEPLOYED (no redeploy): a 30 s settle point,
then `passes` ascending passes over `rates`, 30 s each, seeds 1001.. per pass (as the runner's repetitions).
Writes <root>/run/01-<kind>-repeats/pass-N/rate-XXXXX/result.json. usage: knee_repeats.py <root> <kind> <passes> <rate>..."""
import sys, json, time
sys.path.insert(0, '/users/tomislav/blueprint-docc-mod/utils')
from pathlib import Path
from run_dsb_sn_nw import run_wrk, connections_for
from dsb_apps import app_name
root, kind, passes, rates = Path(sys.argv[1]), sys.argv[2], int(sys.argv[3]), [int(r) for r in sys.argv[4:]]
case = next(c for c in json.loads((root / 'cases.json').read_text()) if c['kind'] == kind)
app = app_name(case); out = root / 'run' / f'01-{kind}-repeats'
w = run_wrk(out / 'settle', 15000, 30, 1001, connections=connections_for(15000), app=app)
print(f"settle 15000: got {w['completed_rps']:,.0f} mean {w['mean_ms']:.1f}", flush=True)
for p in range(passes):
    for r in rates:
        res = run_wrk(out / f'pass-{p + 1}' / f'rate-{r:05d}', r, 30, 1001 + p, connections=connections_for(r), app=app)
        (out / f'pass-{p + 1}' / f'rate-{r:05d}' / 'result.json').write_text(json.dumps(res, indent=1))
        print(f"REPEAT {kind} pass {p + 1} {r}: got {res['completed_rps']:,.0f} mean {res['mean_ms']:.1f} p50 {res['p50_ms']:.1f} p99 {res['p99_ms']:.1f} err {res['non_2xx_3xx']}", flush=True)
print('REPEATS DONE', flush=True)
