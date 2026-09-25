#!/usr/bin/env python3
"""Tomislav-RetCtx (2026-09-25): the bursty span-loss figures for the SN no-work rerun on the current stack (snburst),
in the burst_fig_style layout of FIGURES-2026-09-23 (2.2 in cells, 8 pt, same row order and labels).
  census-outcome: per configuration, share of traces VIABLE vs BROKEN, exact from the SDK refused-trace census
    (bridges: broken = lost a checkpoint/HP span; vanilla: broken = lost any span). User 2026-09-25: a bridge trace is
    viable as long as no checkpoint was lost, so there is no "intact" split for bridges.
  lp-loss-excluding-worst-agent: LP spans lost per agent node over the point = the SDK's lp_dropped (services on that
    node) + the agent priority queue's lp_send_failed + lp_evicted_spans, over (agent lp_enqueued + SDK lp_dropped);
    bar = aggregate over the 7 agents other than the worst, dots = each of them, open marker = the worst. Vanilla row:
    ALL spans lost per node (SDK spans_dropped / spans_received). Gateway-side LP evictions are not attributable to an
    agent and are reported in the printout only.
usage: snburst_figs.py --v ROOT --rev ROOT [--fwd ROOT] --out-dir DIR"""
import argparse, glob, gzip, json, os, re, sys
from pathlib import Path
import numpy as np
sys.path.insert(0, str(Path(__file__).resolve().parent))
import burst_fig_style as st
import matplotlib.pyplot as plt
from matplotlib.patches import Patch
from matplotlib.lines import Line2D

ap = argparse.ArgumentParser()
ap.add_argument('--v', required=True); ap.add_argument('--rev', required=True); ap.add_argument('--fwd')
ap.add_argument('--out-dir', required=True)
# Tomislav-RetCtx (user 2026-09-25): census-outcome rows (default all 7; e.g. v pb-on cgpb-on sb-on = response path only)
ap.add_argument('--census-rows', nargs='+', default=None)
# Tomislav-RetCtx (user 2026-09-25): 'rev' is implied (stated in the paper) -> plain bridge names in the census chart
ap.add_argument('--plain-rev-labels', action='store_true')
a = ap.parse_args()
RATE = 'rate-07500'
import gzip
POINTS = {'v': f'{a.v}/run/01-v/{RATE}'}
for k in ('pb', 'cgpb', 'sb'):
    POINTS[f'{k}-on'] = f'{a.rev}/run/01-{k}/{RATE}'
    if a.fwd: POINTS[f'{k}-off'] = f'{a.fwd}/run/01-{k}/{RATE}'
POINTS = {k: p for k, p in POINTS.items() if os.path.exists(f'{p}/result.json') and glob.glob(f'{p}/after/refused-*.bin')}

def records(path):
    b = np.frombuffer(open(path, 'rb').read(), dtype=np.uint8); n = len(b) // 17; b = b[:n * 17].reshape(n, 17)
    return np.ascontiguousarray(b[:, :16]).view(np.uint64).reshape(n, 2), b[:, 16]

def census(P, kind):
    res = json.load(open(f'{P}/result.json')); completed = res['completed_requests']; hp, anyl = [], []
    for after in sorted(glob.glob(f'{P}/after/refused-*.bin')):
        before = f'{P}/before/refused-{os.path.basename(after)[8:]}'
        nb = os.path.getsize(before) // 17 if os.path.exists(before) else 0
        ids, fl = records(after); ids, fl = ids[nb:], fl[nb:]
        hp.append(ids[(fl & 1) > 0]); anyl.append(ids[(fl & 2) > 0])
    u = lambda x: len(np.unique(np.concatenate(x), axis=0)) if x and sum(len(i) for i in x) else 0
    broken = u(anyl) if kind == 'v' else u(hp)
    # Tomislav-RetCtx (user 2026-09-25): share of traces that lost ANY span, from the 5000 uniformly sampled stored traces
    # (LP evicted inside the agent/gateway priority queues is invisible to the SDK census; bridges' roots are HP, never lost)
    d = json.load(gzip.open(f'{P}/settled-traces.json.gz', 'rt')); tr = d['data'] if isinstance(d, dict) and 'data' in d else d
    sampled_any = 100 * sum(1 for t in tr if len(t['spans']) < 23) / len(tr)
    return dict(completed=completed, broken=broken, pct_broken=100 * broken / completed, sampled_traces=len(tr),
                pct_any_sampled=sampled_any)

def last(path, pat, js):
    try: t = gzip.open(path, 'rt', errors='replace').read()
    except FileNotFoundError: return None
    m = re.findall(pat, t)
    if not m: return None
    return json.loads(m[-1]) if js else {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m[-1])}

def node_map(P):
    return {it['metadata']['name']: it['spec'].get('nodeName', '?') for it in json.load(open(f'{P}/after/pods.json'))['items']}

def per_node(P, kind):
    """(lost, arriving) per agent node: LP for bridges, all spans for vanilla."""
    nodes = node_map(P); out = {}; missing = 0
    pat = r'\bvanilla_processor_metrics (.*)' if kind == 'v' else r'\b(?:pb|cgpb|sb)_processor_metrics (.*)'
    for x in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
        pod = os.path.basename(x)[5:-7]; n = nodes.get(pod, '?')
        A, B = last(x, pat, False), last(x.replace('/after/', '/before/'), pat, False)
        if not A or not B: missing += 1; continue
        d = {k: A.get(k, 0) - B.get(k, 0) for k in A}
        lo, ar = out.get(n, (0, 0))
        if kind == 'v': out[n] = (lo + d.get('spans_dropped', 0), ar + d.get('spans_received', 0))
        else: out[n] = (lo + d.get('lp_dropped', 0), ar + d.get('lp_dropped', 0))
    gw_lp_evicted = 0
    if kind != 'v':
        for x in glob.glob(f'{P}/after/logs-otel*.txt.gz'):
            pod = os.path.basename(x)[5:-7]; n = nodes.get(pod, '?')
            A = last(x, r'priority_queue_metrics\t(\{.*\})', True); B = last(x.replace('/after/', '/before/'), r'priority_queue_metrics\t(\{.*\})', True) or {}
            if not A: continue
            d = {k: A.get(k, 0) - B.get(k, 0) for k in ('lp_enqueued', 'lp_send_failed', 'lp_evicted_spans')}
            if 'otelgw' in pod: gw_lp_evicted += d['lp_evicted_spans']; continue
            lo, ar = out.get(n, (0, 0))
            out[n] = (lo + d['lp_send_failed'] + d['lp_evicted_spans'], ar + d['lp_enqueued'])
    return {n: v for n, v in out.items() if n.startswith('node-')}, missing, gw_lp_evicted

st.setup()
out_dir = Path(a.out_dir); out_dir.mkdir(parents=True, exist_ok=True)
summary = {}
for key, P in POINTS.items():
    kind = 'v' if key == 'v' else key.split('-')[0]
    c = census(P, kind); agents, missing, gw = per_node(P, kind)
    frac = {n: 100 * lo / max(1, ar) for n, (lo, ar) in agents.items()}
    worst = max(frac, key=frac.get)
    rest = {n: v for n, v in frac.items() if n != worst}
    agg = 100 * sum(lo for n, (lo, ar) in agents.items() if n != worst) / max(1, sum(ar for n, (lo, ar) in agents.items() if n != worst))
    allagg = 100 * sum(lo for lo, ar in agents.values()) / max(1, sum(ar for lo, ar in agents.values()))
    summary[key] = dict(point=P, **c, worst=worst, worst_frac=frac[worst], rest=rest, agg=agg, allagg=allagg,
                        missing_service_snapshots=missing, gateway_lp_evicted=gw)
    print(f"{st.LABEL[key]:9s} broken {c['broken']:>9,} of {c['completed']:,} = {c['pct_broken']:7.3f}% | any span lost (sample of {c['sampled_traces']}) {c['pct_any_sampled']:5.1f}% | "
          f"{'LP' if kind != 'v' else 'all'} lost: all agents {allagg:5.1f}%, excl. worst ({worst} {frac[worst]:.1f}%) {agg:5.1f}%"
          f" | gateway LP evicted {gw:,} | missing snapshots {missing}")
json.dump(summary, open(out_dir / 'snburst-summary.json', 'w'), indent=1)
ys = list(range(len(st.ORDER)))[::-1]

# ---- census-outcome: intact | LP lost | broken ----
ALL_ORDER = list(st.ORDER)
if a.census_rows: st.ORDER[:] = a.census_rows
ALL_LABEL = dict(st.LABEL)
if a.plain_rev_labels: st.LABEL.update({'pb-on': 'PB', 'cgpb-on': 'CGPB', 'sb-on': 'SB'})
ys = list(range(len(st.ORDER)))[::-1]
fig, ax = plt.subplots(figsize=(st.W, st.H_TOP))
for y, key in zip(ys, st.ORDER):
    if key not in summary:
        ax.text(50, y, 'pending', ha='center', va='center', fontsize=st.SMALL, color='#888888'); continue
    b = summary[key]['pct_broken']
    lp = 0 if key == 'v' else max(0.0, summary[key]['pct_any_sampled'] - b)   # LP lost only (sample), bridges
    ax.barh(y, 100 - b - lp, color=st.INTACT, height=.72, lw=0)
    ax.barh(y, lp, left=100 - b - lp, color=st.RECON, height=.72, lw=0)
    ax.barh(y, b, left=100 - b, color=st.BROKEN, height=.72, lw=0)
    txt = '0' if summary[key]['broken'] == 0 else (f'{b:.2f}%' if b < 1 else f'{b:.1f}%')
    ax.text(102, y, txt, va='center', ha='left', fontsize=st.SMALL, color=st.BROKEN if summary[key]['broken'] else '#333333')
ax.set_xlabel('Traces (%)', labelpad=1)
st.finish_bars(ax, fig, ys)
st.top_legend(fig, [Patch(color=st.INTACT, label='intact'), Patch(color=st.RECON, label='LP lost'), Patch(color=st.BROKEN, label='broken')])
st.save(fig, str(out_dir / 'census-outcome'))

# ---- lp-loss-excluding-worst-agent ----
st.ORDER[:] = ALL_ORDER; st.LABEL.clear(); st.LABEL.update(ALL_LABEL); ys = list(range(len(st.ORDER)))[::-1]
fig, ax = plt.subplots(figsize=(st.W, st.H_TOP))
for y, key in zip(ys, st.ORDER):
    if key not in summary:
        ax.text(50, y, 'pending', ha='center', va='center', fontsize=st.SMALL, color='#888888'); continue
    r = summary[key]
    if key == 'v': c, off = '#7F7F7F', False
    else: c, off = st.BRIDGE[key.split('-')[0]], key.endswith('-off')
    ax.barh(y, r['agg'], height=.62, color=c, alpha=.35 if off else .6, lw=0, hatch='////' if off else None, edgecolor=c)
    ax.scatter(list(r['rest'].values()), [y] * len(r['rest']), s=6, color=c, edgecolor='white', linewidth=.3, zorder=3)
    ax.scatter([r['worst_frac']], [y], s=16, facecolor='none', edgecolor=st.BROKEN, linewidth=1, zorder=4)
    ax.text(102, y, f"{r['agg']:.0f} ({r['allagg']:.0f})", va='center', ha='left', fontsize=st.SMALL, color='#333333')
ax.set_xlabel('LP spans lost (%)', labelpad=1)
st.finish_bars(ax, fig, ys)
st.top_legend(fig, [Patch(color='#BBBBBB', label='7 agents'),
                    Line2D([], [], ls='', marker='o', ms=3.5, color='#888888', label='each'),
                    Line2D([], [], ls='', marker='o', ms=4.5, mfc='none', mec=st.BROKEN, mew=1, label='worst')])
st.save(fig, str(out_dir / 'lp-loss-excluding-worst-agent'))
