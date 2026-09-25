# Tomislav-RetCtx: cluster B handoff, 2026-09-24

Written on cluster A at about 12:05 UTC on 2026-09-24. The paper deadline is roughly 13:00 UTC on 2026-09-25.

**Your job on cluster B:** run the HotelReservation **no-work** sweep with the new back-to-back ramp-pass protocol. Run it on exactly the same code, images and stack as cluster A. Then send the result roots back to A.

**Before anything else, read your memory** (`~/.claude/projects/-users-tomislav/memory/MEMORY.md`), and read every rule file it lists. They are the user's standing rules. The ones that matter most here:

- Every run uses the CURRENT best stack, with the manifest verified before launch. The user was furious when a run silently used Jaeger/ES.
- Post progress updates with numbers regularly, about every 5 minutes. Never run silent monitors.
- Don't edit application service code.
- Ask questions in chat only.
- No subagents unless authorized.
- No commits unless asked, and no pushes.
- **Never run `git remote -v`**: a PAT lives in origin's URL.
- Use `setsid` for anything long-running.
- Never put latency on a log-scale axis.
- Loss tables always include vanilla.
- Figures always include None/Vanilla/PB/CGPB/SB.

## 1. Bring-up (once)

B started as a bare cluster (2026-09-24): Kubernetes was up, but there was no registry, no repos and no Go. Docker on the nodes already trusts 10.10.1.0/24 as an insecure registry. If `curl -sf http://10.10.1.1:30000/v2/` fails, create the registry first. It uses the same image and NodePort as A's:

```bash
sudo mkdir -p /storage/registry && kubectl apply -f /storage/retctx-handoff/scripts/registry-b.yaml
kubectl -n registry rollout status deploy/docker-registry
```

Then:

```bash
bash /storage/retctx-handoff/scripts/setup_cluster_b.sh
```

This script:

1. Checks the nodes and the registry at `10.10.1.1:30000`.
2. Puts `/users/tomislav/blueprint-docc-mod` on branch `retctx-reject-impl` at `79e9e9fa`, from the bundle. It then applies A's uncommitted working tree (`repo/*worktree.patch` + `repo/*untracked.tar.gz`). It refuses if B's repo has local changes.
3. Creates `.venv` if missing.
4. Installs A's exact `wrk2` binary at `/users/tomislav/DeathStarBench/wrk2/wrk` (md5 `701afcd8…`).
5. Copies the four source roots into `/users/tomislav/deployments/`.
6. Imports all 114 images into B's registry with **unchanged sha256 digests** (`scripts/registry_copy.py`), so every pinned ref resolves.
7. Does a DRYRUN: it derives the three roots and verifies stack and protocol, without deploying anything.

It must end with `SETUP OK`. Nothing is rebuilt on B. The collector (queue3 `sha256:1358b235…`) and all app images are A's bits.

Source roots, which are the same paths as on A:

- hotel no-work: `dsb-hotel/retctx-hotelnw-src-20260924T0631Z`
- SN no-work v/bridges: `dsb-sn/retctx-nw-opt-20260924T0145Z`
- SN no-work nt: `dsb-sn/retctx-nw-m5-20260920T045217Z`
- SN provenance: `dsb-sn/retctx-nwe2e-m5-on-r1-20260920T052509Z`

## 2. The protocol (user, 2026-09-24)

This is implemented in `utils/run_dsb_sn_nw.py` (`climb` / `plan['ramp_passes']`) and derived with `--ramp-passes N --plateau-stop`.

- **Per kind, one deployment, N full ramps back to back, no redeploy.** Deploy and warm up once. Then:
  - **Pass 1** climbs START, START+1000, … until the kind's **own plateau**: 3 consecutive points without a ≥1% gain over the best delivered rate. The rule applies independently to every kind.
  - **Passes 2..N** re-run exactly pass 1's rates, so every point has the same number of trials. There is a 60 s idle gap between passes, which drains the overloaded tail.
  - Each pass uses its own wrk2 seed (seed + 1000·(k−1)).
- **Layout.** Pass 1 is `<root>/run/01-<kind>/rate-XXXXX/`. Pass k is `<root>/run/01-<kind>/pass-kk/rate-XXXXX/`. Each `pass-kk` directory is laid out like a case directory, so every per-case tool works on it. `ramp-passes.json` and `plateau-stop.json` sit in the case directory.
- **Hotel no-work:** START = 5000 (the user said 5k or 10k; the low region is flat), steps of 1k, 30 s per point, N = PASSES (the user said "n=3 or n=5"; the default is 5).
- **Order:** nt, then v, then pb cgpb sb. There are 3 roots: `retctx-hotelnw-passes-{nt,v,br}-<stamp>`.
- **Stack:** identical to A's hotel no-work runs. Checked: the plans and manifests are identical to A's `retctx-hotelnw-mem-{nt,v,br}` roots apart from rates and passes.
  - otelcol agents `admissionotel500m` and a ClickHouse gateway (2 CPU), with the queue3 image.
  - GOMAXPROCS auto, backlog margin, gateway LP `resource_exhausted`, agent compression none.
  - Bridges add the priority receiver, SDK HP retry, and the queue stage at agents and gateway.
  - cpd 2..4 `depth_cubic`.
  - GOGC=off, GOMEMLIMIT=1GiB and gctrace on every app service.

**Time.** One point takes about 37 s. From 5k, pass 1 is about 37 (nt) + 28 (v) + 3×25 (bridges) = 140 points, about 86 min. **n=5 is about 7.5 h in total; n=3 is about 4.6 h.** Starting at 10k saves about 15 min per pass.

## 3. Launch and monitor

```bash
H=/storage/retctx-handoff
PASSES=5 START=5000 setsid nohup $H/scripts/passes_chain.sh hotelnw > $H/state/hotelnw_passes_chain.log 2>&1 < /dev/null &
```

The chain:

1. Preflights the registry.
2. Starts the **trace census** detached (`state/hotelnw_census.log`, one `TRACES` line per point).
3. For each group: derives, verifies (it fails loudly if the stack or protocol is not what is described above), then runs.

Roots are listed in `state/hotelnw_roots.txt`. The chain ends with `PASSES CHAIN COMPLETE` or `PASSES CHAIN FAILED: …`.

Stream progress to the user with a Monitor (30-min max, so re-arm it):

```bash
python3 -u /storage/retctx-handoff/scripts/pass_stream.py /storage/retctx-handoff/state/hotelnw_roots.txt /storage/retctx-handoff/state/hotelnw_passes_chain.log
```

It emits:

- `POINT <kind> p<pass> <rate>`: delivered, mean and p99, pass-1 p99 for comparison, and loss;
- `TRACES`, `STAGE`, `PLATEAU`, `CHAIN` lines;
- a `HEARTBEAT` every 5 min.

Report numbers to the user every point or every few minutes. Never go silent.

## 4. Measurement rules you must keep

- **Trace census** (`scripts/trace_census.py`) is exact, from ClickHouse, while the case is deployed.
  - affected = 1 − intact / max(seen, wrk2 completed).
  - Vanilla: affected = broken.
  - Bridges: affected traces are LP-only (reconstructable). Broken ≤ SDK HP-refused traces + agent HP send_failed + unseen.
  - It waits until the store holds spans ≥ 35 s past the window, because of the ClickHouse insert lag under overload (≈17 s). Without the wait, vanilla broken was overstated by up to 20 points.
  - The last point of a pass counts once the runner has left `measuring` and the row count is stable.
- **Latency:** pool across passes. Merge the wrk2 HdrHistogram spectra (`utils/pool_latency.py`) for the pooled p99, and use a bootstrap 5–95% band over passes. Mean is request-weighted. The user rejected median-of-p99s ("disingenuous").
  - `utils/plot_resp_vs_achieved.py` and `utils/plot_ramp_n1.py` already pool `pass-*` directories.
- The rare p99 "sawtooth" (one front-end backlog episode near the knee, about 1 in 7–9 windows) is real. It is why we pool N passes. Don't "clean" it.
- Report LP loss honestly. The hard criterion: bridges never break traces earlier than vanilla.

## 5. Reference numbers (A, n=1, same stack, 1k steps from 1k)

**Hotel no-work plateau (delivered):**

| Kind | Plateau |
|---|---|
| nt | 37.6k (front end 7.8/8 cores) |
| v | 29.1k |
| PB | 25.5k |
| CGPB | 25.3k |
| SB | 23.8k |

**Corrected census.** Vanilla broken is 0 through 19k, then:

| Rate | 20k | 21k | 22k | 23k | 24k |
|---|---|---|---|---|---|
| Vanilla broken | 18.9% | 42.3% | 69.2% | 66.0% | 95.5% |

Every bridge had **0 broken** at every rate from 1k to 41k.

A new pass-1 point far off these numbers means something differs. Stop and tell the user.

## 6. Pitfalls

- **Self-matching kills.** `pgrep`/`pkill -f` patterns match your own shell. Bracket them: `pgrep -f "[r]un_dsb_sn_nw.py run --out $R"`.
- **Root disk.** On A, `/` hit 100%, from old build dirs and the Go build cache. Check `df -h /` before launch; results go to `/users/tomislav/deployments` on `/`.
- **Detaching.** `nohup` alone dies with the shell. Always use `setsid nohup … < /dev/null &`.
- **Stopping a run.** If the user wants a run stopped, stop the runner and write the case `complete.json` by hand (A's `hotelnw_chain2.sh` did this). Don't leave a half case that a restart would rename to `-interrupted-`.
- **SN no-work.** `passes_chain.sh snnw` also works (START 1000, cpd 2..6 depth_cubic), and its images are imported too. Only run it if the user asks.

## 7. Sending results back

When the chain completes, tell the user. Then, from A (or B), with the user's go-ahead:

```bash
rsync -a /users/tomislav/deployments/dsb-hotel/retctx-hotelnw-passes-*-<stamp> A:/users/tomislav/deployments/dsb-hotel/
```

Raw data under `/storage/tomislav-retctx-e2e/<root>` is optional. Figures are made on A with `utils/plot_resp_vs_achieved.py` and `plot_ramp_n1.py` (`--app hotel`).
