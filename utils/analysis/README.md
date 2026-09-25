# Tomislav-RetCtx: analysis, figure, campaign-chain and monitor scripts (Sept 2026 DSB campaigns)

Snapshot of every script used to run, monitor, analyse and plot the RetCtx / Bridges DeathStarBench campaigns
(Social Network and HotelReservation, real-work and no-work, n=5 ramps, bursty span-loss runs, collector
load tests), copied from the two CloudLab clusters on 2026-09-25 before the experiment expired. The reusable
harness itself stays in `utils/` (derive / prepare / build / run / plot_* scripts); this folder keeps the rest.

Scripts use the ABSOLUTE paths of those machines (`/users/tomislav/deployments/...`, the session scratchpad
`/tmp/claude-20002/.../scratchpad`, `/storage/retctx-handoff`), so they document exactly what ran; rerunning one
elsewhere means adjusting its paths.

| Folder | Source | What is in it |
|---|---|---|
| `scratchpad/` | cluster A session scratchpad | campaign chains (`*_chain.sh`, e.g. `snburst3_chain.sh` = the cap-2.25 bursty span-loss campaign), monitors (`*_monitor.sh`, `live_line.py`), census / loss analysis (`refused_union.py`, `census_outcome.py`, `cp_census.py`), figure scripts (`snburst_figs.py`, `snburst_mechanism2.py`, `plot_burst_mechanism.py`, `plot_census_outcome.py`, ...), and `*_roots.txt` = the experiment roots each campaign used |
| `handoff-cluster-a/`, `handoff-cluster-b/` | `/storage/retctx-handoff` on each cluster | `scripts/` (chains such as `hotelrw_final.sh`, `snrw_nopt_chain.sh`, `snburst_tl.sh`; `pass_stream.py`, `compare_to_n1.py`, `registry_copy.py`, `snnw_hplp.py`), `analysis/` (overhead / knee / vanilla-loss analyses, e.g. `snrw_overhead*.py`, `vanilla_loss.py`), `HANDOFF.md`, `QA-2026-09-24.md` |
| `deployments/` | scripts kept inside experiment roots under `~/deployments` (outside `run/` data) | per-experiment analyzers / monitors / plotters, same relative paths (e.g. `collector-load/spanload-dense-ramps-20260914T185629Z/analyze_spanload_suite.py`) |

Figures produced by these scripts: `~/deployments/dsb-sn/FIGURES-2026-09-2*`, `~/deployments/dsb-hotel/FIGURES-2026-09-24`
(each has a README / `.runs.txt` naming its data roots). Loss experiments need three switches: services
`RETCTX_REFUSED_CENSUS=on` and `RETCTX_REFUSED_RECORDS=on` (`derive_dsb_sn_nw.py --census`), runner `RETCTX_REFUSED_BIN=on`;
whole pod logs for per-second timelines: runner `RETCTX_LOG_TAIL=-1`.

## figures/
Tomislav-RetCtx: every rendered figure (pdf / png / svg) from cluster A, same relative paths as under `~/deployments`:
the `*/FIGURES-2026-09-*` folders whole (with their README.md, `.runs.txt`, summary / data JSON), plus loose figures
kept inside experiment roots (e.g. `collector-load/spanload-dense-ramps-*/throughput-combined-squished.*`). Cluster B
had no figure folders (its runs were copied to A and plotted there). Raw run data and census records are not archived
(rerunnable from the chains in `scratchpad/` and `handoff-*`).
