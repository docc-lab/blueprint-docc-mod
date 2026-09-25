"""Tomislav-RetCtx: exact per-point trace outcome from ClickHouse, taken while the case is still deployed (user 2026-09-24:
count trace % affected per ramp rate, not just span %). For each finished point (>= 20 s after it ends) of the hotel v / bridges
roots: every trace whose first span starts inside the wrk2 window, grouped by (span count, `_br` span count).
  intact   = traces with the full span count (mode of the case's first point)
  affected = 1 - intact / completed requests (a trace that lost every span is unseen and counts as affected)
  vanilla: affected = broken (no reconstruction);  bridges: HP-short = traces with fewer `_br` spans than the first point's mode
            (only meaningful if the first point shows one HP count per trace; its share is printed as hp_det).
Writes <point>/trace-census.json; prints one TRACES line per point. usage: hotel_trace_census.py <scratchpad>"""
import sys, os, json, glob, time, subprocess, urllib.request, urllib.parse, datetime as dt, collections
S = sys.argv[1]
def roots():
    out = []
    for x in ('hotelnw_v_root', 'hotelnw_br_root'):
        try: out.append(open(f'{S}/{x}.txt').read().strip())
        except OSError: pass
    return out
def ch_ip(kind):
    try:
        pods = json.loads(subprocess.run(['kubectl', '-n', 'dsb-hotel', 'get', 'pods', '-o', 'json'], capture_output=True, text=True, timeout=30).stdout)['items']
    except Exception: return None
    for p in pods:
        n = p['metadata']['name']
        if n.startswith(f'clickhouse-hotel-{kind}-') and p['status'].get('phase') == 'Running': return p['status'].get('podIP')
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
done_msg = False
while True:
    for R in roots():
        for case in sorted(glob.glob(f'{R}/run/01-*')):
            if '-interrupted-' in case: continue
            kind = os.path.basename(case).split('-', 1)[1]
            pts = sorted(glob.glob(f'{case}/rate-*/result.json'))
            for f in pts:
                P = os.path.dirname(f); out = f'{P}/trace-census.json'
                if os.path.exists(out): continue
                try: d = json.load(open(f))
                except Exception: continue
                if 'collector_deltas' not in d: continue
                if (dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(d['finished'])).total_seconds() < 20: continue
                ip = ch_ip(kind)
                if not ip:
                    json.dump({'missed': True, 'reason': 'case no longer deployed'}, open(out, 'w')); print(f"TRACES hotelnw {kind} {d['offered_rps']}: MISSED (case gone)", flush=True); continue
                # Tomislav-RetCtx (2026-09-24): under overload the gateway delivers spans to ClickHouse late (insert lag
                # ~17 s seen at 24-26k); count a point only once the store holds spans from >= 20 s past its window.
                try:
                    newest = q(ip, 'SELECT toUnixTimestamp64Nano(max(Timestamp)) FROM otel.otel_traces FORMAT TSV').strip()
                    if int(newest) / 1e9 < dt.datetime.fromisoformat(d['finished']).timestamp() + 35:
                        # the case's LAST point: no later traffic will ever advance max(Timestamp). Once the runner has left
                        # 'measuring' (settle / capture stages, load stopped), count as soon as the store stops growing.
                        st = json.load(open(f'{R}/run-status.json'))
                        if st.get('stage') == 'measuring' and st.get('case') == kind: continue
                        c1 = int(q(ip, 'SELECT count() FROM otel.otel_traces FORMAT TSV')); time.sleep(4)
                        c2 = int(q(ip, 'SELECT count() FROM otel.otel_traces FORMAT TSV'))
                        if c2 != c1: continue
                except Exception: continue
                try: rows = census(ip, d)
                except Exception as e:
                    print(f"TRACES hotelnw {kind} {d['offered_rps']}: query error {e.__class__.__name__}: {str(e)[:120]}", flush=True); continue
                ref_path = f'{case}/trace-census-ref.json'
                if not os.path.exists(ref_path):
                    byn = collections.Counter(); byhp = collections.Counter()
                    for n, hp, c in rows: byn[n] += c; byhp[hp] += c
                    ref = {'full': byn.most_common(1)[0][0], 'hp': byhp.most_common(1)[0][0], 'hp_det': byhp.most_common(1)[0][1] / max(sum(byhp.values()), 1),
                           'from': d['offered_rps']}
                    json.dump(ref, open(ref_path, 'w'))
                ref = json.load(open(ref_path))
                seen = sum(c for _, _, c in rows); intact = sum(c for n, _, c in rows if n >= ref['full'])
                hp_short = None
                if kind != 'v':
                    # Tomislav-RetCtx: traces that lost a CHECKPOINT = SDK refused-trace census (snapshot 'refused', traces
                    # with an HP span refused, summed over services = upper bound of the union) + HP spans the agents failed
                    # to send (each breaks at most one trace). The ClickHouse exporter never failed.
                    def refused_hp(snap):
                        try: return sum(v.get('traces_hp', 0) for v in json.load(open(snap)).get('refused', {}).values())
                        except Exception: return 0
                    sdk_hp = refused_hp(f'{P}/after/snapshot.json') - refused_hp(f'{P}/before/snapshot.json')
                    out_h = subprocess.run(['python3', f'{S}/snnw_hplp.py', case], capture_output=True, text=True).stdout
                    agent_hp = 0
                    for l in out_h.splitlines():
                        if l.split(':')[0].strip() == str(d['offered_rps']) and 'HP lost at agents' in l:
                            agent_hp = int(l.split('HP lost at agents')[1].split('|')[0])
                    hp_short = (sdk_hp, agent_hp)
                # Tomislav-RetCtx: denominator = max(traces seen, wrk2 completed): wrk2 undercounts at some rates (its own
                # clock / connection ramp, e.g. 98.4 % at 3k) while fully lost traces are unseen; max covers both.
                comp = max(d['completed_requests'], sum(c for _, _, c in rows))
                res = {'completed_requests': d['completed_requests'], 'denominator': comp, 'traces_seen': seen, 'intact': intact, 'full_spans': ref['full'], 'rows': rows,
                       'affected_pct': 100 * (1 - intact / comp), 'unseen_pct': 100 * (1 - seen / comp), 'ref': ref}
                if hp_short is not None: res.update(sdk_hp_traces=hp_short[0], agent_hp_spans=hp_short[1], broken_upper_pct=100 * (hp_short[0] + hp_short[1] + max(0, comp - seen)) / comp)
                json.dump(res, open(out, 'w'), indent=1)
                line = (f"TRACES hotelnw {kind} {d['offered_rps']}: affected {res['affected_pct']:.2f}% of {comp:,} traces"
                        f" (seen {seen:,}, unseen {res['unseen_pct']:.2f}%, full = {ref['full']} spans)")
                line += f" -> BROKEN {res['affected_pct']:.2f}%" if kind == 'v' else (f" = LP-only (reconstructable) | BROKEN (checkpoint lost) <= {res['broken_upper_pct']:.3f}%"
                        f" [SDK HP-refused traces {hp_short[0]}, agent HP spans failed {hp_short[1]}, unseen {max(0, comp - seen)}]")
                print(line, flush=True)
    try:
        if 'HOTELNW CHAIN COMPLETE' in open(f'{S}/hotelnw_chain.log').read(): print('TRACE CENSUS: hotelnw sweep complete', flush=True); break
    except OSError: pass
    time.sleep(10)
