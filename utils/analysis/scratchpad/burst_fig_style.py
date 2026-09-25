"""Tomislav-RetCtx: shared style for the bursty-load figures, drawn at final size for a 2x2
figure* grid (each cell 0.49 textwidth ~ 3.4 in): top row = census outcome | LP loss excl. worst
agent (identical size, margins and row order, so the rows line up across the two cells);
bottom row = burst mechanism | table. 8.5 pt axis labels, nothing smaller than 8 pt."""
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

W = 2.2             # every cell (user: 2.2 in wide cells)
# Tomislav-RetCtx (user 2026-09-23): compressed vertically ~18 % (was 1.7 / 1.85)
H_TOP = 1.4         # both top-row cells
H_BOTTOM = 1.5      # mechanism cell
FS = 8
SMALL = 8
# Margins in INCHES, so the text bands keep their size when the height changes:
# x tick labels + x label below the axes; one legend row (0.10 in at 8 pt) above them.
BOTTOM_IN = 0.32
LEGEND_IN = 0.14
LEGEND_GAP_IN = 0.012  # legend's lower edge sits this far above the axes top
ROW_PAD = .45          # y-limit padding above/below the outer bars (was .6: left a gap under the legend)
# identical axes box for the two top-row panels (figure fractions)
TOP_MARGINS = dict(left=.265, right=.785, top=1 - LEGEND_IN / H_TOP, bottom=BOTTOM_IN / H_TOP)
ORDER = ['v', 'pb-on', 'pb-off', 'cgpb-on', 'cgpb-off', 'sb-on', 'sb-off']
# One place to rename the arms (the ablation naming is still open).
# user 2026-09-23: reverse (response path on) = "<bridge> rev"; forward-only = plain bridge name
LABEL = {'v': 'Vanilla', 'pb-on': 'PB rev', 'pb-off': 'PB', 'cgpb-on': 'CGPB rev',
         'cgpb-off': 'CGPB', 'sb-on': 'SB rev', 'sb-off': 'SB'}
BRIDGE = {'pb': '#2878B5', 'cgpb': '#D36B23', 'sb': '#33945B'}
INTACT, RECON, BROKEN = '#CFCFCF', '#9DC3E6', '#B2182B'

def setup():
    plt.rcParams.update({'font.size': FS, 'axes.labelsize': FS, 'xtick.labelsize': SMALL, 'ytick.labelsize': SMALL,
                         'legend.fontsize': SMALL, 'font.family': 'sans-serif', 'axes.linewidth': .6,
                         'xtick.major.width': .6, 'ytick.major.width': .6, 'pdf.fonttype': 42, 'svg.fonttype': 'none'})

def finish_bars(ax, fig, ys):
    ax.set_yticks(ys); ax.set_yticklabels([LABEL[k] for k in ORDER], fontsize=SMALL)
    ax.set_ylim(min(ys) - ROW_PAD, max(ys) + ROW_PAD)
    ax.set_xlim(0, 100); ax.set_xticks([0, 25, 50, 75, 100])
    for sp in ('top', 'right'): ax.spines[sp].set_visible(False)
    ax.grid(True, axis='x', lw=.3, alpha=.35); ax.set_axisbelow(True); ax.tick_params(length=2, pad=1.5)
    fig.subplots_adjust(**TOP_MARGINS)

def top_legend(fig, handles, top=None, height=None):
    """One-row legend centred just above the axes box (same position in both top-row panels;
    the mechanism panel passes its own axes top and figure height)."""
    top = TOP_MARGINS['top'] if top is None else top
    height = H_TOP if height is None else height
    return fig.legend(handles=handles, loc='lower center', bbox_to_anchor=(0.5, top + LEGEND_GAP_IN / height),
                      ncol=len(handles), frameon=False, handlelength=.8, handleheight=.8, columnspacing=.7,
                      handletextpad=.3, borderaxespad=0, borderpad=0)

def save(fig, out):
    from pathlib import Path
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    for ext in ('pdf', 'png', 'svg'): fig.savefig(f'{out}.{ext}', dpi=300)
    print('wrote', out)
