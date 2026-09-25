#!/usr/bin/env python3
"""Produce standalone figures from the archived measurements."""
import json
from pathlib import Path

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D
import numpy as np

ROOT = Path(__file__).resolve().parent
comparison = json.loads((ROOT / 'comparison.json').read_text())
analyses = {s['phase']: json.loads((ROOT / s['phase'] / 'analysis.json').read_text()) for s in comparison}
labels = ['Fixed 2', 'Fixed 4', 'Random 2–6']
colors = ['#466784', '#6c92aa', '#c76430']
plt.rcParams.update({'font.family': 'DejaVu Sans', 'font.size': 11,
                     'axes.spines.top': False, 'axes.spines.right': False,
                     'axes.titleweight': 'bold', 'savefig.facecolor': 'white'})

fig, axes = plt.subplots(1, 3, figsize=(15, 4.7), layout='constrained')
fig.suptitle('Variable checkpoint distance in DSB Social Network · CGPB', fontsize=17, fontweight='bold')
x = np.arange(3)
floor = [s['root_checkpoints_per_request'] + s['leaf_checkpoints_per_request'] for s in comparison]
interior = [s['interior_checkpoints_per_request'] for s in comparison]
axes[0].bar(x, floor, color='#c9d2d8', label='Root + server leaves')
axes[0].bar(x, interior, bottom=floor, color=colors, label='Other scheduled checkpoints')
axes[0].set_xticks(x, labels)
axes[0].set_title('Checkpoints per request')
axes[0].set_ylim(0, max(s['checkpoints_per_request'] for s in comparison) + 2.2)
for i, s in enumerate(comparison):
    axes[0].text(i, s['checkpoints_per_request'] + .18, f'{s["checkpoints_per_request"]:.2f}', ha='center', fontweight='bold')
axes[0].legend(fontsize=9, loc='lower center')
values = [s['bridge_payload_bytes_per_request'] for s in comparison]
axes[1].bar(x, values, color=colors)
axes[1].set_xticks(x, labels)
axes[1].set_title('Exported bridge bytes per request')
axes[1].set_ylabel('Decoded _br + _d payload bytes')
axes[1].set_ylim(0, max(values) * 1.2)
for i, value in enumerate(values):
    axes[1].text(i, value + 5, f'{value:.2f}', ha='center', fontweight='bold')
draws = comparison[2]['inferred_root_draw_counts']
distances = list(range(2, 7))
counts = [draws.get(str(d), 0) for d in distances]
axes[2].bar(distances, counts, color=colors[2])
axes[2].axhline(comparison[2]['requests'] / 5, color='#64748b', linestyle='--', linewidth=1, label='Uniform expectation')
axes[2].set_xticks(distances)
axes[2].set_xlabel('Root-selected distance (inferred from trace)')
axes[2].set_ylabel('Requests')
axes[2].set_title('Observed random root distances')
axes[2].set_ylim(0, max(counts) * 1.23)
for d, n in zip(distances, counts):
    axes[2].text(d, n + .45, str(n), ha='center')
axes[2].legend(fontsize=9)
fig.supxlabel('100 matching Lua ComposePost inputs per setting · 5 requests/sec · 23 spans/request · root → deepest leaf: 7 spans', fontsize=10)
fig.savefig(ROOT / 'comparison.png', dpi=170)
fig.savefig(ROOT / 'comparison.svg')
plt.close(fig)

# Use actual observed paths; leaf checkpoints do not imply that TTL expired.
selected = [('Fixed 2', analyses['fixed2']['traces'][0]), ('Fixed 4', analyses['fixed4']['traces'][0])]
for distance in range(2, 7):
    trace = next(t for t in analyses['random2_6']['traces'] if t['root_draw_candidates'] == [distance])
    selected.append((f'Random 2–6 · root chose {distance}', trace))
fig, ax = plt.subplots(figsize=(11.5, 5.5), layout='constrained')
roles = {'root': '#263a4a', 'checkpoint': '#c76430', 'ordinary': '#c9d2d8', 'leaf': '#438875'}
shown = []
for index, (label, trace) in enumerate(selected):
    rows = {r['span_id']: r for r in trace['rows']}
    leaf = next(r for r in rows.values() if r['operation'] == 'UrlShortenServiceServer_ComposeUrls')
    path = [leaf]
    while path[-1]['parent_span_id']:
        path.append(rows[path[-1]['parent_span_id']])
    path.reverse()
    y = len(selected) - index - 1
    ax.plot([0, 6], [y, y], color='#b5c1ca', linewidth=2, zorder=1)
    for row in path:
        role = 'root' if row['depth'] == 0 else 'leaf' if row['leaf'] else 'checkpoint' if row['checkpoint'] else 'ordinary'
        ax.scatter(row['depth'], y, s=245, color=roles[role], edgecolor='white', linewidth=1.5, zorder=2)
        ax.text(row['depth'], y, str(row['depth']), ha='center', va='center', fontsize=9,
                color='white' if role != 'ordinary' else '#263a4a', fontweight='bold')
    shown.append({'label': label, 'trace_id': trace['trace_id'], 'path': path})
ax.set_yticks(list(reversed(range(len(selected)))), [label for label, _ in selected])
ax.set_xticks(range(7), ['Wrk2API\nserver', 'ComposePost\nclient', 'ComposePost\nserver', 'Text\nclient', 'Text\nserver', 'URL shorten\nclient', 'URL shorten\nserver'])
ax.set_xlim(-.35, 6.35)
ax.set_ylim(-.65, len(selected) - .2)
ax.spines[['left', 'bottom']].set_visible(False)
ax.tick_params(length=0)
ax.set_title('Where checkpoints landed on one 7-span path', pad=18, fontsize=16)
legend = [Line2D([0], [0], marker='o', color='w', label=label, markerfacecolor=roles[role], markersize=10)
          for role, label in [('root', 'Root'), ('checkpoint', 'Scheduled checkpoint'), ('ordinary', 'Ordinary span'), ('leaf', 'Server leaf checkpoint')]]
fig.legend(handles=legend, ncol=4, loc='outside lower center', frameon=False, fontsize=10)
fig.savefig(ROOT / 'checkpoint-paths.png', dpi=170)
fig.savefig(ROOT / 'checkpoint-paths.svg')
plt.close(fig)
(ROOT / 'illustrated-traces.json').write_text(json.dumps(shown, indent=2) + '\n')
print('Created comparison.png/svg and checkpoint-paths.png/svg from measured traces.')
