#!/usr/bin/env python3
"""Tomislav-RetCtx: outcome figure from the exact SDK census (burst_fig_style layout, top-left
cell): per configuration, share of all traces intact / only LP lost (reconstructable) / a
checkpoint lost (vanilla: any span lost). Right column: the broken share."""
import json, argparse
from pathlib import Path
from matplotlib.patches import Patch
import burst_fig_style as st
import matplotlib.pyplot as plt
ap = argparse.ArgumentParser(); ap.add_argument('--out', default='/users/tomislav/deployments/dsb-sn/FIGURES-2026-09-23/census-outcome')
a = ap.parse_args(); st.setup()
d = json.load(open(Path(__file__).resolve().parent / 'census_outcome.json'))
fig, ax = plt.subplots(figsize=(st.W, st.H_TOP))
ys = list(range(len(st.ORDER)))[::-1]
for y, key in zip(ys, st.ORDER):
    if key not in d:
        ax.text(50, y, 'pending', ha='center', va='center', fontsize=st.SMALL, color='#888888'); continue
    p = d[key]['pct']; left = 0
    for state, col in (('intact', st.INTACT), ('reconstructable', st.RECON), ('broken', st.BROKEN)):
        ax.barh(y, p[state], left=left, color=col, height=.72, lw=0); left += p[state]
    b = p['broken']; txt = '0' if d[key]['broken'] == 0 else (f'{b:.2f}%' if b < 1 else f'{b:.1f}%')
    ax.text(102, y, txt, va='center', ha='left', fontsize=st.SMALL, color=st.BROKEN if d[key]['broken'] else '#333333')

ax.set_xlabel('Traces (%)', labelpad=1)
st.finish_bars(ax, fig, ys)
st.top_legend(fig, [Patch(color=st.INTACT, label='intact'), Patch(color=st.RECON, label='LP lost'),
                    Patch(color=st.BROKEN, label='broken')])
st.save(fig, a.out)
