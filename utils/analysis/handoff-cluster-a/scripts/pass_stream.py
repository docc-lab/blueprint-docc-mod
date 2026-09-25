#!/usr/bin/env python3
"""Tomislav-RetCtx: event stream for a ramp-pass campaign (one line per event; feed it to a Monitor). Emits:
  POINT <kind> p<pass> <rate>: delivered, mean, p99, loss (vanilla: spans lost before the agents; bridges: LP lost % of
        LP and HP lost at the agents), and for pass >= 2 the same rate's pass-1 p99
  TRACES ... (every new trace-census.json),  STAGE ... (run-status changes, incl. deploy / warmup / pass-gap),
  PLATEAU ... (a kind's pass-1 plateau stop),  HEARTBEAT (every 300 s without another event, with the current state),
  CHAIN ... (last line of the chain log changes); exits on 'PASSES CHAIN COMPLETE' / 'PASSES CHAIN FAILED' in the chain log or a failed run.
usage: pass_stream.py ROOTS_FILE CHAIN_LOG"""
import sys, os, json, glob, time, subprocess, re
HERE = os.path.dirname(os.path.abspath(__file__)); ROOTS, LOG = sys.argv[1], sys.argv[2]
seen, last_status, last_chain, last_event = set(), {}, None, time.time()
if os.environ.get('SKIP_EXISTING'):  # restart without re-emitting what was already reported
    for R in (l.strip() for l in open(ROOTS) if l.strip()):
        seen |= set(glob.glob(f'{R}/run/*/rate-*/result.json') + glob.glob(f'{R}/run/*/pass-*/rate-*/result.json')
                    + glob.glob(f'{R}/run/*/rate-*/trace-census.json') + glob.glob(f'{R}/run/*/pass-*/rate-*/trace-census.json')
                    + glob.glob(f'{R}/run/*/plateau-stop.json'))
def roots():
    try: return [l.strip() for l in open(ROOTS) if l.strip()]
    except OSError: return []
def emit(s):
    global last_event; print(s, flush=True); last_event = time.time()
def loss(case_dir, rate, kind):
    out = subprocess.run([sys.executable, f'{HERE}/snnw_hplp.py', case_dir] + (['auto'] if kind == 'v' else []),
                         capture_output=True, text=True).stdout
    for l in out.splitlines():
        if l.split(':')[0].strip() == str(rate): return l.split(':', 1)[1].strip()
    return 'loss n/a'
while True:
    for R in roots():
        try: st = json.load(open(f'{R}/run-status.json'))
        except Exception: st = None
        if st:
            key = (st.get('state'), st.get('case'), st.get('stage'), st.get('pass'))
            if last_status.get(R) != key:
                last_status[R] = key
                emit(f"STAGE {time.strftime('%H:%M:%S', time.gmtime())} {os.path.basename(R)}: {st.get('state')} {st.get('case', '')} "
                     f"{st.get('stage', '')} pass {st.get('pass', 1)} {st.get('offered_rps', '')} {st.get('error', '')}".rstrip())
            if st.get('state') == 'failed': emit(f"RUN FAILED {R}: {st.get('error', '')[:300]}"); sys.exit(1)
        for case in sorted(glob.glob(f'{R}/run/[0-9][0-9]-*')):
            if '-interrupted-' in case: continue
            kind = os.path.basename(case).split('-', 1)[1]
            pf = f'{case}/plateau-stop.json'
            if os.path.exists(pf) and pf not in seen:
                seen.add(pf); p = json.load(open(pf))
                emit(f"PLATEAU {kind}: stopped after {p['stopped_after']}, best delivered {p['best_delivered']:,.0f} -> passes 2..N re-run this grid")
            for f in sorted(glob.glob(f'{case}/rate-*/result.json')) + sorted(glob.glob(f'{case}/pass-*/rate-*/result.json')):
                P = os.path.dirname(f)
                if f not in seen:
                    try: d = json.load(open(f))
                    except Exception: continue
                    if 'collector_deltas' not in d: continue
                    seen.add(f); k, rate = d.get('pass', 1), d['offered_rps']
                    line = (f"POINT {kind} p{k} {rate}: got {d['completed_rps']:,.0f} ({100 * d['completed_rps'] / rate:.1f}%) "
                            f"mean {d['mean_ms']:.1f} p99 {d['p99_ms']:.1f} ms")
                    if k > 1:
                        # pass 1 is this case's rate-* unless the root tops up an older sweep (PASS1_DIRS='{"kind": dir}')
                        p1 = json.loads(os.environ.get('PASS1_DIRS', '{}')).get(kind, case)
                        try: line += f" [p1 p99 {json.load(open(f'{p1}/rate-{rate:05d}/result.json'))['p99_ms']:.1f}]"
                        except Exception: pass
                    if kind != 'nt': line += ' | ' + loss(os.path.dirname(P), rate, kind)
                    emit(line)
                c = f'{P}/trace-census.json'
                if os.path.exists(c) and c not in seen:
                    seen.add(c); t = json.load(open(c))
                    if t.get('missed'): emit(f'TRACES {kind} {P.split(kind + "/")[-1]}: MISSED'); continue
                    b = f"BROKEN {t['affected_pct']:.2f}%" if kind == 'v' else f"LP-only {t['affected_pct']:.2f}%, BROKEN <= {t.get('broken_upper_pct', 0):.3f}%"
                    emit(f"TRACES {kind} p{t.get('pass', 1)} {P[-5:].lstrip('0')}: {b} of {t['denominator']:,} traces")
    try:
        lines = open(LOG).read().splitlines(); L = lines[-1] if lines else ''
    except OSError: L = ''
    if L != last_chain:
        last_chain = L; emit(f'CHAIN: {L[:220]}')
    if 'PASSES CHAIN COMPLETE' in L: sys.exit(0)
    if 'PASSES CHAIN FAILED' in L: sys.exit(1)
    if time.time() - last_event >= 300:
        emit(f"HEARTBEAT {time.strftime('%H:%M:%S', time.gmtime())}: " + '; '.join(f'{os.path.basename(r)} {v}' for r, v in last_status.items()))
    time.sleep(10)
