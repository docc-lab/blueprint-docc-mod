import sys, json, glob, os, statistics
f, S = sys.argv[1], sys.argv[2]; d = json.load(open(f))
kind = f.split('/run/')[1].strip('/').split('/')[0].split('-', 1)[1]; rate = int(f.split('rate-')[1][:5])
def res(p):
    return json.load(open(p)) if os.path.exists(p) else None
got, off = d['completed_rps'], d['offered_rps']
err = d.get('non_2xx_3xx') or 0
se = d.get('socket_errors'); err += sum(se.values()) if isinstance(se, dict) else (se or 0)
gen = d['generator_cpu_seconds'] / d['wall_seconds']
line = f"CH+MEMLIMIT {kind} {rate}: got {got:,.0f} ({got/off:.1%}) mean {d['mean_ms']:.1f} ms p99 {d['p99_ms']:.1f} | gen {gen:.1f} cores, err {err}"
M = sorted(glob.glob('/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*'))
mp = [r for r in (res(f'{m}/run/01-{kind}/rate-{rate:05d}/result.json') for m in M) if r]
if mp: line += f" | matrix got {statistics.mean(p['completed_rps'] for p in mp):,.0f} mean {statistics.mean(p['mean_ms'] for p in mp):.1f}"
if kind != 'nt':
    for x in ('snnw_mem_nt_root', 'snnw_mem_nttail_root'):
        try: nt = res(open(f'{S}/{x}.txt').read().strip() + f'/run/01-nt/rate-{rate:05d}/result.json')
        except OSError: nt = None
        if nt: line += f" | nt-mem got {nt['completed_rps']:,.0f} mean {nt['mean_ms']:.1f}"
if kind in ('pb', 'cgpb', 'sb'):
    try: vv = res(open(f'{S}/snnw_memch_v_root.txt').read().strip() + f'/run/01-v/rate-{rate:05d}/result.json')
    except OSError: vv = None
    if vv and 'collector_deltas' in vv: line += f" | v-mem got {vv['completed_rps']:,.0f} mean {vv['mean_ms']:.1f}"
import subprocess, re as _re
def _loss(case_dir, full=None):
    out = subprocess.run(['python3', f'{S}/snnw_hplp.py', case_dir] + ([str(full)] if full else []), capture_output=True, text=True).stdout
    for l in out.splitlines():
        if l.split(':')[0].strip() == str(rate):
            m = _re.search(r'LP lost +([\d.]+)%', l) or _re.search(r'before agents +([\d.]+)%', l); h = _re.search(r'HP lost at agents (\d+)', l)
            return (m.group(1) if m else '?'), (h.group(1) if h else None)
    return None, None
case_dir = os.path.dirname(os.path.dirname(f))
if kind == 'v':
    lv, _ = _loss(case_dir, 23.0); line += f" | LOSS v spans {lv}%"
elif kind in ('pb', 'cgpb', 'sb'):
    lp, hp = _loss(case_dir)
    try: lv, _ = _loss(open(f'{S}/snnw_memch_v_root.txt').read().strip() + '/run/01-v', 23.0)
    except OSError: lv = None
    line += f" | LOSS {kind} LP {lp}% HP {hp} | v spans {lv}%"
if got < 0.97 * off: line += "  << SHORTFALL"
print(line, flush=True)
