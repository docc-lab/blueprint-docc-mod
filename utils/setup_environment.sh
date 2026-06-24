#!/usr/bin/env bash
#
# setup_environment.sh — full one-time bootstrap for the Blueprint / DSB-SN
# bridges workflow on a fresh CloudLab node.
#
# Installs EVERYTHING needed to build & deploy DSB-SN variants:
#   1. Base apt packages (git, curl, python3-venv/pip)
#   2. Clone the sibling repos next to blueprint-docc-mod:
#        - docc-lab's opentelemetry-collector-contrib
#        - DeathStarBench (--recurse-submodules)
#   3. Toolchain via install_blueprint_deps.sh:
#        - Go (latest, from go.dev tarball, NOT apt; PATH into ~/.bashrc + ~/.profile)
#        - protobuf compiler + lib (apt: protobuf-compiler libprotobuf-dev)
#        - Go protoc plugins: protoc-gen-go + protoc-gen-go-grpc
#        - kompose (latest GitHub release binary)
#   4. Python venv with d2k8s requirements (pyyaml) + the seeder dep (aiohttp)
#   5. Add the current user to the 'docker' group
#   6. Apply the in-cluster docker registry (namespace + pvc + deployment + service)
#   7. Pin all k8s nodes' CPUs to 2.2 GHz for deterministic clocks across runs
#        (pin-cpu-22ghz.sh pin --all)
#
# Idempotent: safe to re-run. Assumes passwordless sudo. The registry + CPU-pin
# steps need a working kubectl context (skipped with a notice if unreachable).
#
# Override repo URLs via env if you use forks, e.g.:
#   DSB_REPO=https://github.com/docc-lab/DeathStarBench.git ./setup_environment.sh
#
# Run with bash; if launched via `sh`, re-exec under bash (Ubuntu /bin/sh is dash).
if [ -z "${BASH_VERSION:-}" ]; then exec bash "$0" "$@"; fi
set -euo pipefail

log(){  printf '\n\033[1;32m=== %s ===\033[0m\n' "$*"; }
warn(){ printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }

# --- locations ------------------------------------------------------------
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)   # .../blueprint-docc-mod/utils
BP_ROOT=$(dirname "$SCRIPT_DIR")                            # .../blueprint-docc-mod
BASE=$(dirname "$BP_ROOT")                                  # parent dir holding sibling repos
VENV="$BP_ROOT/.venv"

# Repo URLs (override via env to use forks).
COLLECTOR_REPO=${COLLECTOR_REPO:-https://github.com/docc-lab/opentelemetry-collector-contrib.git}
DSB_REPO=${DSB_REPO:-https://github.com/delimitrou/DeathStarBench.git}

# =========================================================================
log "1/7  base apt packages"
# =========================================================================
sudo apt-get update -qq
sudo apt-get install -y git curl ca-certificates python3-venv python3-pip

# =========================================================================
log "2/7  clone sibling repos into $BASE"
# =========================================================================
clone_if_absent(){   # <url> <dest> [extra git clone args...]
  local url=$1 dest=$2; shift 2
  if [ -d "$dest/.git" ]; then
    echo "already cloned: $dest"
  else
    echo "cloning $url -> $dest"
    git clone "$@" "$url" "$dest"
  fi
}
clone_if_absent "$COLLECTOR_REPO" "$BASE/opentelemetry-collector-contrib"
clone_if_absent "$DSB_REPO"       "$BASE/DeathStarBench" --recurse-submodules
# ensure submodules are present even if DSB was cloned earlier without them
[ -d "$BASE/DeathStarBench/.git" ] && \
  git -C "$BASE/DeathStarBench" submodule update --init --recursive || \
  warn "DeathStarBench submodule update skipped/failed"

# =========================================================================
log "3/7  toolchain (Go + protobuf + kompose) via install_blueprint_deps.sh"
# =========================================================================
bash "$SCRIPT_DIR/install_blueprint_deps.sh"
# make the freshly-installed Go usable for the remainder of THIS script
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"

# =========================================================================
log "4/7  python venv + deps -> $VENV"
# =========================================================================
[ -d "$VENV" ] || python3 -m venv "$VENV"
"$VENV/bin/pip" install --upgrade pip -q
"$VENV/bin/pip" install -q -r "$BP_ROOT/d2k8s/requirements.txt"   # pyyaml (d2k8s + pin/perf helpers)
"$VENV/bin/pip" install -q aiohttp                                # init_social_graph.py seeder
echo "venv ready: $VENV"

# =========================================================================
log "5/7  add $USER to the 'docker' group"
# =========================================================================
if id -nG "$USER" | tr ' ' '\n' | grep -qx docker; then
  echo "$USER already in docker group"
else
  sudo usermod -aG docker "$USER"
  warn "added $USER to 'docker' — log out/in (or run 'newgrp docker') for it to take effect"
fi

# =========================================================================
log "6/7  apply in-cluster docker registry (namespace 'registry', NodePort 30000)"
# =========================================================================
if kubectl cluster-info >/dev/null 2>&1; then
  kubectl create namespace registry --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -f "$SCRIPT_DIR/registry-pvc.yaml" \
                -f "$SCRIPT_DIR/registry-deployment.yaml" \
                -f "$SCRIPT_DIR/registry-service.yaml"
  echo "registry applied."
else
  warn "kubectl cluster not reachable — skipping registry apply. Run later:"
  warn "  kubectl create namespace registry --dry-run=client -o yaml | kubectl apply -f -"
  warn "  kubectl apply -f $SCRIPT_DIR/registry-pvc.yaml -f $SCRIPT_DIR/registry-deployment.yaml -f $SCRIPT_DIR/registry-service.yaml"
fi

# =========================================================================
log "7/7  pin all node CPUs to 2.2 GHz (deterministic clocks across runs)"
# =========================================================================
if kubectl cluster-info >/dev/null 2>&1; then
  bash "$SCRIPT_DIR/pin-cpu-22ghz.sh" pin --all \
    || warn "CPU pin reported errors on one or more nodes (see output above)"
  warn "CPU pin is NOT persistent across reboots — re-run 'utils/pin-cpu-22ghz.sh pin --all' after any node reboot."
else
  warn "kubectl cluster not reachable — skipping CPU pin. Run later: utils/pin-cpu-22ghz.sh pin --all"
fi

# =========================================================================
log "DONE"
# =========================================================================
cat <<SUMMARY

Next steps:
  1. Open a NEW shell (or: source ~/.bashrc) to pick up the Go PATH.
  2. For docker without sudo: log out/in, or run 'newgrp docker'.
  3. Activate the venv before running build_deploy_dsb.sh so its 'python3' has
     pyyaml + aiohttp (the build script and seeder call 'python3' directly):
         source $VENV/bin/activate
  4. Build + deploy a variant, e.g.:
         cd $BP_ROOT
         utils/build_deploy_dsb.sh -s docker_pb_es -n pb_test --cpd 2 --gc natural --apply --seed

Locations:
  collector       : $BASE/opentelemetry-collector-contrib
  DeathStarBench  : $BASE/DeathStarBench
  python venv     : $VENV
SUMMARY
