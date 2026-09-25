#!/usr/bin/env python3
"""Tomislav-RetCtx: exact per-point trace census from ClickHouse, taken while the case is still deployed (generic over
apps, aware of ramp passes: <case>/rate-* and <case>/pass-kk/rate-*). For every finished point of every non-nt case:
all traces whose first span starts inside the wrk2 window, grouped by (span count, `_br` span count).
  intact   = traces with the full span count (mode of the case's first point)
  affected = 1 - intact / max(traces seen, wrk2 completed)   (a fully lost trace is unseen and counts as affected)
  vanilla: affected = BROKEN (no reconstruction)
  bridges: affected traces are LP-only unless a checkpoint was lost; BROKEN <= SDK HP-refused traces + agent HP spans
           send_failed + unseen traces (counters, snnw_hplp.py)
Waits until the store holds spans >= 35 s past the window (gateway insert lag under overload); a pass's LAST point
counts once the runner has left 'measuring' for that case and the row count stops growing.
Writes <point>/trace-census.json, prints one TRACES line per point.
usage: trace_census.py ROOTS_FILE [--until-file FILE]   (ROOTS_FILE: one experiment root per line, re-read every loop;
       exits when FILE exists, or when every root's run-status is complete/failed/stopped and nothing is pending)"""
import sys, os, json, glob, time, subprocess, urllib.request, datetime as dt, collections
HERE = os.path.dirname(os.path.abspath(__file__))
ROOTS = sys.argv[1]; UNTIL = sys.argv[sys.argv.index('--until-file') + 1] if '--until-file' in sys.argv else None
def roots():
    try: return [l.strip() for l in open(ROOTS) if l.strip()]
    except OSError: return []
def ch_ip(namespace, variant):
    try:
        pods = json.loads(subprocess.run(['kubectl', '-n', namespace, 'get', 'pods', '-o', 'json'], capture_output=True, text=True, timeout=30).stdout)['items']
    except Exception: return None
    for p in pods:
        if p['metadata']['name'].startswith(f'clickhouse-{variant}-') and p['status'].get('phase') == 'Running': return p['status'].get('podIP')
    return None
def q(ip, sql):
    req = urllib.request.Request(f'http://{ip}:8123/?user=otel&password=otel', data=sql.encode(), method='POST')
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=180) as r: return r.read().decode()
def ts(s): return dt.datetime.fromisoformat(s).astimezone(dt.timezone.utc).strftime('%Y-%m-%d %H:%M:%S.%f')
def census(ip, d):
    a, b = ts(d['started']), ts(d['finished'])
    sql = f"""SELECT n, hp, count() FROM (SELECT TraceId, count() AS n, countIf(mapContains(SpanAttributes, '_br')) AS hp, min(Timestamp) AS t0
      FROM otel.otel_traces WHERE Timestamp >= toDateTime64('{a}', 9, 'UTC') - INTERVAL 5 SECOND AND Timestamp < toDateTime64('{b}', 9, 'UTC') + INTERVAL 15 SECOND
      GROUP BY TraceId HAVING t0 >= toDateTime64('{a}', 9, 'UTC') AND t0 < toDateTime64('{b}', 9, 'UTC')) GROUP BY n, hp FORMAT TSV"""
    return [tuple(int(x) for x in l.split('\t')) for l in q(ip, sql).strip().splitlines() if l]
def refused_hp(snap):
    try: return sum(v.get('traces_hp', 0) for v in json.load(open(snap)).get('refused', {}).values())
    except Exception: return 0
while True:
    pending = 0; active = False
    for R in roots():
        try: st = json.load(open(f'{R}/run-status.json'))
        except Exception: st = {}
        if st.get('state') not in ('complete', 'failed', 'stopped'): active = True
        try: cases = {c['name']: c for c in json.load(open(f'{R}/cases.json'))}
        except Exception: continue
        app = os.path.basename(R)
        for case in sorted(glob.glob(f'{R}/run/[0-9][0-9]-*')):
            if '-interrupted-' in case: continue
            name = os.path.basename(case).split('-', 1)[1]; c = cases.get(name)
            if not c or c['kind'] == 'nt' or c.get('collector_profile') == 'sink': continue
            kind = c['kind']
            for f in sorted(glob.glob(f'{case}/rate-*/result.json')) + sorted(glob.glob(f'{case}/pass-*/rate-*/result.json')):
                P = os.path.dirname(f); out = f'{P}/trace-census.json'
                if os.path.exists(out): continue
                try: d = json.load(open(f))
                except Exception: pending += 1; continue
                if 'collector_deltas' not in d: pending += 1; continue
                pending += 1
                if (dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(d['finished'])).total_seconds() < 20: continue
                tag = f"{kind} p{d.get('pass', 1)} {d['offered_rps']}"
                ip = ch_ip(c['namespace'], c['variant'])
                if not ip:
                    json.dump({'missed': True, 'reason': 'case no longer deployed'}, open(out, 'w')); pending -= 1
                    print(f'TRACES {tag}: MISSED (case gone)', flush=True); continue
                try:
                    newest = q(ip, 'SELECT toUnixTimestamp64Nano(max(Timestamp)) FROM otel.otel_traces FORMAT TSV').strip()
                    if int(newest) / 1e9 < dt.datetime.fromisoformat(d['finished']).timestamp() + 35:
                        if st.get('stage') == 'measuring' and st.get('case') == name: continue
                        c1 = int(q(ip, 'SELECT count() FROM otel.otel_traces FORMAT TSV')); time.sleep(4)
                        if int(q(ip, 'SELECT count() FROM otel.otel_traces FORMAT TSV')) != c1: continue
                    rows = census(ip, d)
                except Exception as e:
                    print(f'TRACES {tag}: query error {e.__class__.__name__}: {str(e)[:120]}', flush=True); continue
                ref_path = f'{case}/trace-census-ref.json'
                if not os.path.exists(ref_path):
                    byn = collections.Counter(); byhp = collections.Counter()
                    for n, hp, cnt in rows: byn[n] += cnt; byhp[hp] += cnt
                    json.dump({'full': byn.most_common(1)[0][0], 'hp': byhp.most_common(1)[0][0], 'from': d['offered_rps']}, open(ref_path, 'w'))
                ref = json.load(open(ref_path))
                seen = sum(cnt for _, _, cnt in rows); intact = sum(cnt for n, _, cnt in rows if n >= ref['full'])
                comp = max(d['completed_requests'], seen)
                res = {'completed_requests': d['completed_requests'], 'denominator': comp, 'traces_seen': seen, 'intact': intact,
                       'full_spans': ref['full'], 'rows': rows, 'affected_pct': 100 * (1 - intact / comp),
                       'unseen_pct': 100 * (1 - seen / comp), 'ref': ref, 'pass': d.get('pass', 1)}
                line = f"TRACES {tag}: affected {res['affected_pct']:.2f}% of {comp:,} traces (seen {seen:,}, unseen {res['unseen_pct']:.2f}%, full = {ref['full']} spans)"
                if kind == 'v':
                    line += f" -> BROKEN {res['affected_pct']:.2f}%"
                else:
                    sdk_hp = refused_hp(f'{P}/after/snapshot.json') - refused_hp(f'{P}/before/snapshot.json')
                    agent_hp = 0
                    for l in subprocess.run([sys.executable, f'{HERE}/snnw_hplp.py', os.path.dirname(P)], capture_output=True, text=True).stdout.splitlines():
                        if l.split(':')[0].strip() == str(d['offered_rps']) and 'HP lost at agents' in l:
                            agent_hp = int(l.split('HP lost at agents')[1].split('|')[0])
                    res.update(sdk_hp_traces=sdk_hp, agent_hp_spans=agent_hp,
                               broken_upper_pct=100 * (sdk_hp + agent_hp + max(0, comp - seen)) / comp)
                    line += (f" = LP-only (reconstructable) | BROKEN <= {res['broken_upper_pct']:.3f}%"
                             f" [SDK HP-refused traces {sdk_hp}, agent HP spans failed {agent_hp}, unseen {max(0, comp - seen)}]")
                json.dump(res, open(out, 'w'), indent=1); pending -= 1
                print(line, flush=True)
    if UNTIL and os.path.exists(UNTIL) and not pending: print('TRACE CENSUS: done', flush=True); break
    if not UNTIL and roots() and not active and not pending: print('TRACE CENSUS: all roots finished', flush=True); break
    time.sleep(10)
