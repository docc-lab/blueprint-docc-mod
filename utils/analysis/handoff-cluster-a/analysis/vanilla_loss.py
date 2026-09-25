# Tomislav-RetCtx: vanilla span loss per offered rate at every pipeline stage, per repetition, from exact counters:
# SDK (vanilla_processor_metrics: spans_dropped = queue overflow + failed batches), agents (collector_deltas: receiver
# refused / exporter send failed), gateway (Prometheus before->after: receiver refused+failed, exporter send/enqueue failed).
# Agent receiver refusals are final for vanilla (OTLP_RETRY off) and land in the SDK's spans_dropped, so they are counted there
# once; gateway receiver refusals are retried by the agents (retry_on_failure), so they are reported, not counted as loss.
# usage: vanilla_loss.py <vanilla root> [...]
import glob, gzip, re, os, sys, json
def vlast(p):
    try: t = gzip.open(p, 'rt', errors='replace').read()
    except FileNotFoundError: return {}
    m = re.findall(r'vanilla_processor_metrics (.*)', t)
    return {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m[-1])} if m else {}
GW = ('otelcol_receiver_refused_spans_total', 'otelcol_receiver_failed_spans_total',
      'otelcol_exporter_send_failed_spans_total', 'otelcol_exporter_enqueue_failed_spans_total', 'otelcol_exporter_sent_spans_total')
def gw(p):
    try: t = gzip.open(p, 'rt', errors='replace').read()
    except FileNotFoundError: return {}
    out = dict.fromkeys(GW, 0.0)
    for line in t.splitlines():
        if line.startswith('#'): continue
        for k in GW:
            if line.startswith(k + '{') or line.startswith(k + ' '):
                out[k] += float(line.rsplit(' ', 1)[1])
    return out
for R in sys.argv[1:]:
    print(f'== {os.path.basename(R.rstrip("/"))}')
    rows = {}
    for case in sorted(glob.glob(f'{R}/run/[0-9][0-9]-v')):
        for P in sorted(glob.glob(f'{case}/rate-*')):
            if not os.path.exists(f'{P}/result.json'): continue
            d = json.load(open(f'{P}/result.json')); c = d.get('collector_deltas') or {}
            s = {'spans_received': 0, 'spans_dropped': 0}; miss = 0
            nxt = sorted(glob.glob(f'{case}/rate-*')); nxt = nxt[nxt.index(P) + 1] if nxt.index(P) + 1 < len(nxt) else None
            for x in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
                a, b = vlast(x), vlast(x.replace('/after/', '/before/'))
                # Tomislav-RetCtx: a log-tail snapshot can miss the metrics line (gctrace floods at saturation); fall back to
                # the next point's before snapshot, else leave the service out of this point and count it as missing
                if not a and nxt:
                    a = vlast(x.replace(f'{P}/after/', f'{nxt}/before/'))
                if not a or not b: miss += 1; continue
                for k in s: s[k] += a.get(k, 0) - b.get(k, 0)
            g = dict.fromkeys(GW, 0.0)
            for x in glob.glob(f'{P}/after/gateway-*.txt.gz'):
                a, b = gw(x), gw(x.replace('/after/', '/before/'))
                for k in GW: g[k] += a.get(k, 0) - b.get(k, 0)
            agent_lost = c.get('otelcol_exporter_send_failed_spans_total', 0)
            gw_lost = g[GW[1]] + g[GW[2]] + g[GW[3]]; gw_refused = g[GW[0]]
            lost = s['spans_dropped'] + agent_lost + gw_lost
            rows.setdefault(d['offered_rps'], []).append((os.path.basename(case), d['completed_rps'], s['spans_received'], s['spans_dropped'],
                                                          c.get('otelcol_receiver_refused_spans_total', 0), agent_lost, gw_lost, lost, miss, gw_refused))
    for rate in sorted(rows):
        rs = rows[rate]; tot = sum(r[2] for r in rs); lost = sum(r[7] for r in rs)
        per = ', '.join(f"{r[0][:2]}:{100 * r[7] / max(r[2], 1):.2f}%" for r in rs if r[7])
        print(f"{rate:6d}: runs {len(rs)} | got {sum(r[1] for r in rs) / len(rs):7,.0f} | lost {int(lost):>9,} of {tot:>11,} = {100 * lost / max(tot, 1):6.3f}%"
              f" | SDK {sum(r[3] for r in rs):,} agents {int(sum(r[5] for r in rs)):,} gateway {int(sum(r[6] for r in rs)):,}"
              f" | gateway refusals (retried by agents) {int(sum(r[9] for r in rs)):,}" + (f" | per run {per}" if per else '')
              + (f" | service snapshots missing {sum(r[8] for r in rs)}" if sum(r[8] for r in rs) else ''))
    # Tomislav-RetCtx: whole-run check -- the SDK counters are cumulative since process start, so the largest spans_dropped any
    # snapshot of a run shows is that service's total SDK loss up to that snapshot
    for case in sorted(glob.glob(f'{R}/run/[0-9][0-9]-v')):
        worst = {}
        for x in glob.glob(f'{case}/*/*/logs-*-service-*.txt.gz'):
            v = vlast(x)
            if v:
                svc = os.path.basename(x).split('-service')[0][5:]; worst[svc] = max(worst.get(svc, 0), v.get('spans_dropped', 0))
        bad = {k: v for k, v in worst.items() if v}
        print(f"  {os.path.basename(case)} whole run, cumulative SDK spans_dropped: {bad if bad else 0} ({len(worst)} services)")
