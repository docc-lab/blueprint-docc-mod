#!/usr/bin/env python3
"""Tomislav-RetCtx (2026-09-25): burst-mechanism timeline for the snburst rerun, from data already captured (no rerun).
Rows: offered rate per 10 s epoch (wrk2 burst trace, exact); spans lost per epoch as a share of arriving spans for vanilla
and one bridge (LP, and checkpoints = HP). The runner kept only the last 2000 log lines, so the per-second counters of the
2026-09-23 figure are not available; instead:
  * TIMING of SDK-side drops: the census records (append-only per service, in drop order) are placed in time by
    interpolating between ANCHORS = sampled traces (5000 uniform over the point, each with its start time) that appear in
    that service's records (order verified monotonic);
  * vanilla: each service's exact spans_dropped (SDK counter delta) distributed over epochs like its census records;
  * bridge HP (checkpoints): the exact cp_dropped per service distributed like its HP-flagged census records;
  * bridge LP: most LP loss happens inside the agents' priority queues (evictions), invisible to the census, so its
    per-epoch SHAPE comes from the sampled traces' missing spans per epoch, scaled so the 600 s total equals the exact LP
    loss (SDK lp_dropped + agent lp_send_failed + lp_evicted + gateway lp_evicted).
Arriving spans per epoch = epoch rate x 10 s x 23 spans/request."""
import argparse, glob, gzip, json, os, re, sys
from datetime import datetime
from pathlib import Path
import numpy as np
sys.path.insert(0, str(Path(__file__).resolve().parent))
import burst_fig_style as st
import matplotlib.pyplot as plt
from matplotlib.patches import Patch

ap = argparse.ArgumentParser()
ap.add_argument('--v', required=True, help='vanilla point dir'); ap.add_argument('--bridge', required=True, help='bridge point dir')
ap.add_argument('--bridge-label', default='SB rev'); ap.add_argument('--out', required=True)
ap.add_argument('--width', type=float, default=2.2); ap.add_argument('--height', type=float, default=None)
a = ap.parse_args(); E, N, SPR = 10, 60, 23

def epochs(P):
    R = {}
    for line in open(f'{P}/wrk.stderr'):
        m = re.match(r'burst epoch (\d+) t=[\d.]+s g=[\d.]+ rate=(\d+)', line)
        if m: R[int(m.group(1))] = float(m.group(2))
    return np.array([R.get(k, R[max(R)]) for k in range(N)])

def t0_us(P): return datetime.fromisoformat(json.load(open(f'{P}/command.json'))['started']).timestamp() * 1e6

def sample(P):
    d = json.load(gzip.open(f'{P}/settled-traces.json.gz', 'rt')); traces = d['data'] if isinstance(d, dict) and 'data' in d else d
    return {(t.get('traceID') or t['spans'][0]['traceID']).rjust(32, '0'): (min(s['startTime'] for s in t['spans']), len(t['spans'])) for t in traces}

def counters(x, pat):
    try: t = gzip.open(x, 'rt', errors='replace').read()
    except FileNotFoundError: return {}
    m = re.findall(pat, t)
    return {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m[-1])} if m else {}

def timed_records(P, smp, t0):
    """per service: (epoch index of each record in this point, flags) placed by anchor interpolation."""
    out = {}
    for after in sorted(glob.glob(f'{P}/after/refused-*.bin')):
        svc = re.sub(r'-service-.*', '', os.path.basename(after)[8:])
        before = f'{P}/before/refused-{os.path.basename(after)[8:]}'
        nb = os.path.getsize(before) // 17 if os.path.exists(before) else 0
        b = open(after, 'rb').read(); n = len(b) // 17
        if n - nb <= 0: continue
        ids = [b[i * 17:i * 17 + 16].hex() for i in range(nb, n)]; flags = np.frombuffer(b, np.uint8)[16::17][nb:n]
        anc = [(i, smp[x][0]) for i, x in enumerate(ids) if x in smp]
        if len(anc) < 2:  # too few anchors: spread uniformly over the point (reported)
            ep = np.linspace(0, N - 1e-9, len(ids)).astype(int)
        else:
            ai, at = np.array([p[0] for p in anc]), np.array([p[1] for p in anc])
            at = np.maximum.accumulate(at)  # enforce monotone time along drop order
            ep = np.clip(((np.interp(np.arange(len(ids)), ai, at) - t0) / 1e6 // E).astype(int), 0, N - 1)
        out[svc] = (ep, flags, len(anc))
    return out

def svc_delta(P, svc, pat):
    x = (glob.glob(f'{P}/after/logs-{svc}-service-*.txt.gz') or [None])[0]
    if not x: return {}
    A, B = counters(x, pat), counters(x.replace('/after/', '/before/'), pat)
    return {k: A.get(k, 0) - B.get(k, 0) for k in A}

R = epochs(a.v); Rb = epochs(a.bridge); arriving = R * E * SPR; arriving_b = Rb * E * SPR
# ---- vanilla ----
smp_v = sample(a.v); t0v = t0_us(a.v); recs = timed_records(a.v, smp_v, t0v); v_lost = np.zeros(N); anchors_v = {}
for svc, (ep, fl, na) in recs.items():
    d = svc_delta(a.v, svc, r'\bvanilla_processor_metrics (.*)'); tot = d.get('spans_dropped', 0)
    if tot <= 0: continue
    h = np.bincount(ep, minlength=N).astype(float); v_lost += tot * h / h.sum(); anchors_v[svc] = na
v_share = 100 * v_lost / arriving
# ---- bridge ----
smp_b = sample(a.bridge); t0b = t0_us(a.bridge); recs_b = timed_records(a.bridge, smp_b, t0b)
kind = re.search(r'/01-(pb|cgpb|sb)/', a.bridge).group(1); pat = rf'\b{kind}_processor_metrics (.*)'
hp_lost = np.zeros(N); hp_total = 0; lp_sdk = 0
for svc, (ep, fl, na) in recs_b.items():
    d = svc_delta(a.bridge, svc, pat); cp = d.get('cp_dropped', 0); lp_sdk += d.get('lp_dropped', 0); hp_total += cp
    hep = ep[(fl & 1) > 0]
    if cp > 0 and len(hep): h = np.bincount(hep, minlength=N).astype(float); hp_lost += cp * h / h.sum()
lp_queue = 0
for x in glob.glob(f'{a.bridge}/after/logs-otel*.txt.gz'):
    get = lambda f: (lambda m: json.loads(m[-1]) if m else {})(re.findall(r'priority_queue_metrics\t(\{.*\})', gzip.open(f, 'rt', errors='replace').read()) if os.path.exists(f) else [])
    A, B = get(x), get(x.replace('/after/', '/before/'))
    lp_queue += sum(A.get(k, 0) - B.get(k, 0) for k in ('lp_send_failed', 'lp_evicted_spans'))
lp_total = lp_sdk + lp_queue
miss = np.zeros(N); cnt = np.zeros(N)
for tid, (start, n) in smp_b.items():
    k = int((start - t0b) / 1e6 // E)
    if 0 <= k < N: miss[k] += max(0, SPR - n); cnt[k] += 1
est = np.where(cnt > 0, miss / np.maximum(cnt, 1) / SPR, 0) * arriving_b
lp_lost = est * lp_total / est.sum()
lp_share, hp_share = 100 * lp_lost / arriving_b, 100 * hp_lost / arriving_b

print(f"vanilla: lost {v_lost.sum():,.0f} spans = {100*v_lost.sum()/arriving.sum():.2f}% of arriving; epoch max {v_share.max():.1f}%; anchors per service {anchors_v}")
print(f"{a.bridge_label}: LP lost {lp_total:,} (SDK {lp_sdk:,} + queues {lp_queue:,}) = {100*lp_total/arriving_b.sum():.1f}% of all arriving spans, epoch max {lp_share.max():.1f}%; "
      f"checkpoints lost {hp_total:,} in {int((hp_lost > 0).sum())} epochs, epoch max {hp_share.max():.3f}%; sample traces per epoch {int(cnt.min())}..{int(cnt.max())}")

st.setup(); fs = st.FS; H = a.height or st.H_BOTTOM
fig, axes = plt.subplots(2, 1, figsize=(a.width, H), sharex=True, gridspec_kw=dict(height_ratios=[1, 1.3], hspace=0.18))
t = np.arange(N + 1) * E
ax = axes[0]; ax.step(t, np.append(R, R[-1]) / 1000, where='post', color='#1A1A1A', lw=.8)
ax.axhline(8.0, color='#888888', lw=.6, ls='--')  # vanilla's sustained (300 s) first-loss rate
ax.set_ylabel('Offered\n(req/s)', fontsize=fs); ax.set_ylim(5, 11); ax.set_yticks([6, 8, 10]); ax.set_yticklabels(['6k', '8k', '10k'])
ax = axes[1]; step = lambda v: np.append(v, v[-1])
ax.fill_between(t, 0, step(lp_share), color='#9DC3E6', lw=0, step='post', label=f'{a.bridge_label} (LP)')
ax.fill_between(t, 0, step(v_share), color='#7F7F7F', lw=0, alpha=.85, step='post', label='Vanilla')
ax.fill_between(t, step(lp_share), step(lp_share + hp_share), color='#B2182B', lw=0, step='post', label='checkpoints')
ax.set_ylim(0, 100); ax.set_ylabel('Lost (%)', fontsize=fs); ax.set_yticks([0, 50, 100]); ax.set_xlabel('Time (s)', fontsize=fs, labelpad=1)
top = 1 - st.LEGEND_IN / H
st.top_legend(fig, axes[1].get_legend_handles_labels()[0], top=top, height=H)
for ax in axes:
    ax.set_xlim(0, 600); ax.grid(True, lw=.3, alpha=.35); ax.tick_params(length=2, pad=1.5); ax.set_axisbelow(True)
    for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
axes[1].set_xticks([0, 200, 400, 600])
fig.subplots_adjust(left=.235, right=.955, top=top, bottom=st.BOTTOM_IN / H)
st.save(fig, a.out)
json.dump(dict(vanilla_share=v_share.tolist(), lp_share=lp_share.tolist(), hp_share=hp_share.tolist(), offered=R.tolist(),
               lp_total=lp_total, hp_total=hp_total), open(a.out + '.data.json', 'w'))
