"""Tomislav-RetCtx: one more FULL ramp on a case that is still deployed, same per-point protocol as the sweep runner
(30 s points, ~8 s between points = the runner's snapshot gap), new seed. Writes <root>/run/01-<kind>-ramp2/rate-XXXXX.
usage: full_ramp_repeat.py <root> <kind> <seed> <first> <last> <step>"""
import sys, json, time
sys.path.insert(0, '/users/tomislav/blueprint-docc-mod/utils')
from pathlib import Path
from run_dsb_sn_nw import run_wrk, connections_for
from dsb_apps import app_name
root, kind, seed, a, b, step = Path(sys.argv[1]), sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5]), int(sys.argv[6])
case = next(c for c in json.loads((root / 'cases.json').read_text()) if c['kind'] == kind)
app = app_name(case); out = root / 'run' / f'01-{kind}-ramp2'
w = run_wrk(out / 'warmup', 100, 60, seed, connections=10, app=app)
print(f"warmup 100 rps: got {w['completed_rps']:,.0f}", flush=True)
for r in range(a, b + 1, step):
    res = run_wrk(out / f'rate-{r:05d}', r, 30, seed, connections=connections_for(r), app=app)
    (out / f'rate-{r:05d}' / 'result.json').write_text(json.dumps(res, indent=1))
    print(f"RAMP2 {kind} {r}: got {res['completed_rps']:,.0f} mean {res['mean_ms']:.1f} p50 {res['p50_ms']:.1f} p99 {res['p99_ms']:.1f} err {res['non_2xx_3xx']}", flush=True)
    time.sleep(8)
print('RAMP2 DONE', flush=True)
