#!/usr/bin/env python3
"""Tomislav-RetCtx (2026-09-25): burst-mechanism timeline from WHOLE pod logs (per-second counters over the full point;
cluster B timeline pair, runner RETCTX_LOG_TAIL=-1). Rows: offered rate per 10 s epoch (wrk2 burst trace, exact); spans lost
per epoch as a share of the spans the SDKs produced in that epoch:
  vanilla     = sum over services of the SDK's per-second spans_dropped / spans_received (vanilla_processor_metrics);
  bridge HP   = SDK cp_dropped + agents hp_send_failed;
  bridge LP   = SDK lp_dropped + agents' and gateway's priority-queue lp_send_failed + lp_evicted_spans;
all from per-second cumulative counters, differenced and summed per 10 s epoch aligned to the wrk2 start."""
import argparse, glob, gzip, json, re, sys
from datetime import datetime, timezone
from pathlib import Path
import numpy as np
sys.path.insert(0, str(Path(__file__).resolve().parent))
import burst_fig_style as st
import matplotlib.pyplot as plt

ap = argparse.ArgumentParser()
ap.add_argument('--v', required=True); ap.add_argument('--bridge', required=True); ap.add_argument('--bridge-label', default='SB rev')
ap.add_argument('--out', required=True); ap.add_argument('--width', type=float, default=2.2); ap.add_argument('--height', type=float, default=None)
a = ap.parse_args(); E, N = 10, 60

def epochs(P):
    R = {}
    for line in open(f'{P}/wrk.stderr'):
        m = re.match(r'burst epoch (\d+) t=[\d.]+s g=[\d.]+ rate=(\d+)', line)
        if m: R[int(m.group(1))] = float(m.group(2))
    return np.array([R.get(k, R[max(R)]) for k in range(N)])

def t0(P): return datetime.fromisoformat(json.load(open(f'{P}/command.json'))['started']).timestamp()

def series(rows, key, T0):
    """per-epoch sums of the increments of a cumulative counter; rows = sorted [(t, dict)]."""
    out = np.zeros(N); prev = None
    for t, d in rows:
        if prev is not None:
            k = int((t - T0) // E)
            if 0 <= k < N: out[k] += d.get(key, 0) - prev.get(key, 0)
        prev = d
    return out

def sdk_rows(P, kind):
    pat = re.compile(rf'^(\d{{4}}/\d\d/\d\d \d\d:\d\d:\d\d) INFO {kind}_processor_metrics (.*)$')
    out = []
    for f in glob.glob(f'{P}/after/logs-*-service-*.txt.gz'):
        rows = []
        for line in gzip.open(f, 'rt', errors='replace'):
            m = pat.match(line)
            if m:
                t = datetime.strptime(m.group(1), '%Y/%m/%d %H:%M:%S').replace(tzinfo=timezone.utc).timestamp()
                rows.append((t, {k: int(v) for k, v in re.findall(r'(\w+)=(\d+)', m.group(2))}))
        if rows: out.append(sorted(rows, key=lambda r: r[0]))
    return out

def queue_rows(P):
    out = []
    for f in glob.glob(f'{P}/after/logs-otel*.txt.gz'):
        rows = []
        for line in gzip.open(f, 'rt', errors='replace'):
            if 'priority_queue_metrics' not in line: continue
            t = datetime.strptime(line[:23], '%Y-%m-%dT%H:%M:%S.%f').replace(tzinfo=timezone.utc).timestamp()
            rows.append((t, json.loads(line.split('priority_queue_metrics\t', 1)[1])))
        if rows: out.append(sorted(rows, key=lambda r: r[0]))
    return out

R = epochs(a.v)
T0 = t0(a.v); vs = sdk_rows(a.v, 'vanilla')
v_drop = sum(series(r, 'spans_dropped', T0) for r in vs); v_recv = sum(series(r, 'spans_received', T0) for r in vs)
kind = re.search(r'/01-(pb|cgpb|sb)/', a.bridge).group(1)
Tb = t0(a.bridge); bs = sdk_rows(a.bridge, kind); qs = queue_rows(a.bridge)
b_recv = sum(series(r, 'spans_received', Tb) for r in bs)
hp = sum(series(r, 'cp_dropped', Tb) for r in bs) + sum(series(r, 'hp_send_failed', Tb) for r in qs)
lp = sum(series(r, 'lp_dropped', Tb) for r in bs) + sum(series(r, 'lp_send_failed', Tb) + series(r, 'lp_evicted_spans', Tb) for r in qs)
v_share = 100 * v_drop / np.maximum(v_recv, 1); hp_share = 100 * hp / np.maximum(b_recv, 1); lp_share = 100 * lp / np.maximum(b_recv, 1)
# worst pod decides: every pod's per-second rows must start before the point and end after it
cov_v = max(r[0][0] for r in vs) - T0, min(r[-1][0] for r in vs) - T0
cov_b = max(r[0][0] for r in bs + qs) - Tb, min(r[-1][0] for r in bs + qs) - Tb
print(f"coverage, worst pod (s from wrk start): vanilla {cov_v[0]:.0f}..{cov_v[1]:.0f}, bridge {cov_b[0]:.0f}..{cov_b[1]:.0f} (need <=0 .. >=599)")
assert cov_v[0] <= 0 and cov_v[1] >= 599 and cov_b[0] <= 0 and cov_b[1] >= 599, 'per-second logs do not cover the whole point'
print(f"vanilla lost {v_drop.sum():,.0f} of {v_recv.sum():,.0f} = {100*v_drop.sum()/max(1,v_recv.sum()):.2f}%, epoch max {v_share.max():.1f}%")
print(f"{a.bridge_label}: LP lost {lp.sum():,.0f} = {100*lp.sum()/max(1,b_recv.sum()):.1f}% of spans, epoch max {lp_share.max():.1f}%; HP lost {hp.sum():,.0f} in {int((hp>0).sum())} epochs, epoch max {hp_share.max():.3f}%")

st.setup(); fs = st.FS; H = a.height or st.H_BOTTOM
fig, axes = plt.subplots(2, 1, figsize=(a.width, H), sharex=True, gridspec_kw=dict(height_ratios=[1, 1.3], hspace=0.18))
t = np.arange(N + 1) * E; step = lambda v: np.append(v, v[-1])
ax = axes[0]; ax.step(t, step(R) / 1000, where='post', color='#1A1A1A', lw=.8)
ax.axhline(8.0, color='#888888', lw=.6, ls='--')  # vanilla's sustained (300 s) first-loss rate
ax.set_ylabel('Offered\n(req/s)', fontsize=fs); ax.set_ylim(5, 11.6); ax.set_yticks([6, 8, 10]); ax.set_yticklabels(['6k', '8k', '10k'])
ax = axes[1]
ax.fill_between(t, 0, step(lp_share), color='#9DC3E6', lw=0, step='post', label=f'{a.bridge_label} (LP)')
ax.fill_between(t, 0, step(v_share), color='#7F7F7F', lw=0, alpha=.85, step='post', label='Vanilla')
ax.fill_between(t, step(lp_share), step(lp_share + hp_share), color='#B2182B', lw=0, step='post', label='checkpoints' if hp.sum() else None)
if not hp.sum():  # nothing to draw: say so in the panel (a legend entry would not fit the 2.2 in width)
    ax.text(.98, .95, '0 checkpoints lost', transform=ax.transAxes, ha='right', va='top', fontsize=fs, color='#B2182B')
ax.set_ylim(0, 100); ax.set_ylabel('Lost (%)', fontsize=fs); ax.set_yticks([0, 50, 100]); ax.set_xlabel('Time (s)', fontsize=fs, labelpad=1)
top = 1 - st.LEGEND_IN / H
st.top_legend(fig, axes[1].get_legend_handles_labels()[0], top=top, height=H)
for ax in axes:
    ax.set_xlim(0, 600); ax.grid(True, lw=.3, alpha=.35); ax.tick_params(length=2, pad=1.5); ax.set_axisbelow(True)
    for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
axes[1].set_xticks([0, 200, 400, 600])
fig.subplots_adjust(left=.26, right=.955, top=top, bottom=st.BOTTOM_IN / H)
st.save(fig, a.out)
json.dump(dict(offered=R.tolist(), vanilla_share=v_share.tolist(), lp_share=lp_share.tolist(), hp_share=hp_share.tolist()), open(a.out + '.data.json', 'w'))
