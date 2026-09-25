#!/usr/bin/env bash
# Tomislav-RetCtx: bring cluster B to cluster A's exact code + images. Run on B's node-0 as tomislav, from the package:
#   bash /storage/retctx-handoff/scripts/setup_cluster_b.sh   (after utils/setup_environment.sh; re-runnable)
# Refuses to touch a repo with uncommitted changes. Idempotent.
set -euo pipefail
H=$(cd "$(dirname "$0")/.." && pwd)
B=/users/tomislav/blueprint-docc-mod
say() { echo "== $*"; }
say "cluster"; kubectl get nodes --no-headers | awk '{print $1, $2}' | tr '\n' ' '; echo
curl -sf -m 5 http://10.10.1.1:30000/v2/ >/dev/null && echo "registry 10.10.1.1:30000 up" || { echo "registry 10.10.1.1:30000 DOWN"; exit 1; }
say "blueprint-docc-mod"
P=$H/repo/blueprint-docc-mod-worktree.patch
if [ -d $B/.git ] && [ "$(git -C $B rev-parse HEAD)" = "$(cat $H/repo/blueprint-docc-mod-HEAD)" ] \
   && git -C $B apply --reverse --check --binary $P 2>/dev/null; then
  echo "repo already at A's tree (HEAD + worktree patch)"; cd $B
else
  if [ -d $B/.git ]; then
    cd $B
    [ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "REFUSING: $B has uncommitted changes (git status)"; exit 1; }
    git fetch $H/repo/blueprint-docc-mod.bundle retctx-reject-impl:refs/remotes/handoff/retctx-reject-impl
    git checkout -B retctx-reject-impl refs/remotes/handoff/retctx-reject-impl
  else
    git clone -b retctx-reject-impl $H/repo/blueprint-docc-mod.bundle $B; cd $B
  fi
  [ "$(git rev-parse HEAD)" = "$(cat $H/repo/blueprint-docc-mod-HEAD)" ] || { echo "HEAD mismatch"; exit 1; }
  git apply --binary $P
fi
tar xzf $H/repo/blueprint-docc-mod-untracked.tar.gz -C $B
say "venv"; [ -x $B/.venv/bin/python ] || python3 -m venv $B/.venv
$B/.venv/bin/pip install -q -r $H/repo/venv-requirements.txt  # A's exact versions (setup_environment only adds pyyaml + aiohttp)
$B/.venv/bin/python -c "import yaml, matplotlib; print('venv ok')"
say "wrk2"; mkdir -p /users/tomislav/DeathStarBench/wrk2
D=/users/tomislav/DeathStarBench; [ -d $D/.git ] && ! git -C $D apply --reverse --check $H/repo/DeathStarBench-wrk2.patch 2>/dev/null \
  && { git -C $D apply $H/repo/DeathStarBench-wrk2.patch && echo "applied A's wrk.c patch"; } || true
cmp -s $H/repo/wrk2-wrk.bin /users/tomislav/DeathStarBench/wrk2/wrk || install -m 755 $H/repo/wrk2-wrk.bin /users/tomislav/DeathStarBench/wrk2/wrk
md5sum /users/tomislav/DeathStarBench/wrk2/wrk; /users/tomislav/DeathStarBench/wrk2/wrk -v 2>&1 | head -1 || true
say "source roots"; for app in dsb-sn dsb-hotel; do mkdir -p /users/tomislav/deployments/$app; cp -an $H/roots/$app/* /users/tomislav/deployments/$app/; done
say "registry images (digest-preserving)"; python3 $H/scripts/registry_copy.py import $H/images/registry | tail -1
python3 $H/scripts/registry_copy.py check $(cat $H/images/all-refs.txt) | tail -2
say "raw data dir"; [ -w /storage/tomislav-retctx-e2e ] || { sudo mkdir -p /storage/tomislav-retctx-e2e && sudo chown tomislav /storage/tomislav-retctx-e2e; }; ls -ld /storage/tomislav-retctx-e2e
say "dry run (derive + verify, deploys nothing)"; DRYRUN=1 bash $H/scripts/passes_chain.sh hotelnw | tail -2
df -h / /storage | tail -2
say "SETUP OK"
