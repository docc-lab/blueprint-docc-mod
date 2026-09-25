"""Tomislav-RetCtx: one point's line for stream7.sh. usage: stream7_point.py <result.json> <scratchpad>"""
import sys, json, glob, os, statistics, subprocess, re
f, S = sys.argv[1], sys.argv[2]; d = json.load(open(f))
kind = f.split('/run/')[1].strip('/').split('/')[0].split('-', 1)[1]; rate = int(f.split('rate-')[1][:5])
app = 'HOTEL' if '/dsb-hotel/' in f else 'SN'
def res(p): return json.load(open(p)) if os.path.exists(p) else None
def root(name):
    try: return open(f'{S}/{name}.txt').read().strip()
    except OSError: return None
got, off = d['completed_rps'], d['offered_rps']
err = (d.get('non_2xx_3xx') or 0); se = d.get('socket_errors'); err += sum(se.values()) if isinstance(se, dict) else (se or 0)
line = f"{app} {kind} {rate}: got {got:,.0f} ({got/off:.1%}) mean {d['mean_ms']:.1f} ms p99 {d['p99_ms']:.1f} | gen {d['generator_cpu_seconds']/d['wall_seconds']:.1f} cores, err {err}"
if app == 'SN':
    nt_roots = [root('snnw_mem_nt_root'), root('snnw_mem_nttail_root')]; v_root = root('snnw_memch_v_root')
    prev = sorted(glob.glob(f'/users/tomislav/deployments/dsb-sn/retctx-nwe2e-m5-on-r*/run/01-{kind}/rate-{rate:05d}/result.json')); plabel = 'matrix'
else:
    nt_roots = [root('hotel_mem_nt_root')]; v_root = root('hotel_mem_v_root')
    pat = {'nt': 'retctx-hotel-cubic-chprobe-*', 'v': 'retctx-hotel-cubic-opt2-v-*', 'pb': 'retctx-hotel-cubic-opt2-pbcgpb-*',
           'cgpb': 'retctx-hotel-cubic-opt2-pbcgpb-*', 'sb': 'retctx-hotel-cubic-opt2-sb-*'}[kind]
    prev = sorted(glob.glob(f'/users/tomislav/deployments/dsb-hotel/{pat}/run/01-{kind}/rate-{rate:05d}/result.json')); plabel = 'default-GC opt2'
pp = [r for r in (res(x) for x in prev) if r]
if pp: line += f" | {plabel} got {statistics.mean(p['completed_rps'] for p in pp):,.0f} mean {statistics.mean(p['mean_ms'] for p in pp):.1f}"
if kind != 'nt':
    for nr in nt_roots:
        nt = res(f'{nr}/run/01-nt/rate-{rate:05d}/result.json') if nr else None
        if nt and 'collector_deltas' in nt: line += f" | nt-mem got {nt['completed_rps']:,.0f} mean {nt['mean_ms']:.1f}"
if kind in ('pb', 'cgpb', 'sb') and v_root:
    vv = res(f'{v_root}/run/01-v/rate-{rate:05d}/result.json')
    if vv and 'collector_deltas' in vv: line += f" | v-mem got {vv['completed_rps']:,.0f} mean {vv['mean_ms']:.1f}"
def loss(case_dir, full=None):
    out = subprocess.run(['python3', f'{S}/snnw_hplp.py', case_dir] + ([full] if full else []), capture_output=True, text=True).stdout
    for l in out.splitlines():
        if l.split(':')[0].strip() == str(rate):
            m = re.search(r'LP lost +([\d.]+)%', l) or re.search(r'before agents +([\d.]+)%', l); h = re.search(r'HP lost at agents (\d+)', l)
            return (m.group(1) if m else '?'), (h.group(1) if h else None)
    return None, None
case_dir = os.path.dirname(os.path.dirname(f))
if kind == 'v':
    lv, _ = loss(case_dir, 'auto'); line += f" | LOSS v spans {lv}%"
elif kind in ('pb', 'cgpb', 'sb'):
    lp, hp = loss(case_dir); lv = loss(f'{v_root}/run/01-v', 'auto')[0] if v_root else None
    line += f" | LOSS {kind} LP {lp}% HP {hp} | v spans {lv}%"
if got < 0.97 * off: line += "  << SHORTFALL"
print(line, flush=True)
