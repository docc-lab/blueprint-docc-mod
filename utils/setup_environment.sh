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
#   3. Build DeathStarBench's wrk2 load generator (apt: build-essential libssl-dev
#        libz-dev luarocks; luarocks install luasocket; make; install to /usr/local/bin/wrk)
#   4. Toolchain via install_blueprint_deps.sh:
#        - Go (latest, from go.dev tarball, NOT apt; PATH into ~/.bashrc + ~/.profile)
#        - protobuf compiler + lib (apt: protobuf-compiler libprotobuf-dev)
#        - Go protoc plugins: protoc-gen-go + protoc-gen-go-grpc
#        - kompose (latest GitHub release binary)
#   5. Python venv with d2k8s requirements (pyyaml) + the seeder dep (aiohttp)
#   6. Add the current user to the 'docker' group
#   7. Apply the in-cluster docker registry (namespace + pvc + deployment + service)
#   8. Pin all k8s nodes' CPUs to 2.2 GHz for deterministic clocks across runs
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

usage(){
cat <<'EOHELP'
setup_environment.sh — one-shot bootstrap of the DSB-SN bridges workflow on a fresh CloudLab head node

USAGE
  utils/setup_environment.sh            # run all 8 stages (idempotent, safe to re-run)
  utils/setup_environment.sh -h|--help  # this help

STAGES (all idempotent)
  1/8  base apt packages       git curl ca-certificates python3-venv python3-pip
  2/8  clone sibling repos     opentelemetry-collector-contrib + DeathStarBench
                               (--recurse-submodules), NEXT TO blueprint-docc-mod.
                               Skipped per-repo if already cloned — a restored copy
                               with local changes is never touched.
  3/8  build wrk2              DeathStarBench's load generator -> /usr/local/bin/wrk
  4/8  toolchain               Go (go.dev tarball, PATH into ~/.bashrc + ~/.profile),
                               protobuf compiler+lib, protoc-gen-go(-grpc), kompose
  5/8  python venv             blueprint-docc-mod/.venv with pyyaml + aiohttp (seeder!)
  6/8  docker group            adds $USER to 'docker' (newgrp docker / re-login after)
  7/8  in-cluster registry     namespace+pvc+deployment+service at NodePort 30000
                               (skipped with a notice if kubectl is unreachable)
  8/8  CPU pin                 pins EVERY k8s node to a fixed clock via
                               pin-cpu-22ghz.sh pin <ghz> --all. Nodes are
                               AUTO-DETECTED from `kubectl get nodes` — works for
                               any cluster size. NOT reboot-persistent.

ENVIRONMENT OVERRIDES
  CPU_GHZ=<ghz>         target clock for stage 8 (default 2.2 = c220g5 base clock)
  COLLECTOR_REPO=<url>  collector fork to clone (default docc-lab/opentelemetry-collector-contrib)
  DSB_REPO=<url>        DeathStarBench fork (default delimitrou/DeathStarBench)

EXAMPLES
  # standard fresh-cluster bootstrap:
  utils/setup_environment.sh
  # pin to 2.0 GHz instead of 2.2:
  CPU_GHZ=2.0 utils/setup_environment.sh
  # use the docc-lab DSB fork:
  DSB_REPO=https://github.com/docc-lab/DeathStarBench.git utils/setup_environment.sh
  # re-run later just to fix the registry + pin after a cluster rebuild (stages are
  # idempotent — clones/toolchain will fast-skip):
  utils/setup_environment.sh

AFTER IT FINISHES
  1. source ~/.bashrc              (pick up the Go PATH)
  2. newgrp docker                 (or re-login, for docker without sudo)
  3. source blueprint-docc-mod/.venv/bin/activate   (pyyaml + aiohttp for the build/seed scripts)
  4. re-run 'utils/pin-cpu-22ghz.sh pin <ghz> --all' after ANY node reboot (pin is not persistent)
EOHELP
exit "${1:-0}"
}
case "${1:-}" in -h|--help) usage 0;; "") :;; *) echo "unknown argument: $1" >&2; usage 1;; esac

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
# Target CPU clock for the 2.2 GHz pin stage (override, e.g. CPU_GHZ=2.0 ./setup_environment.sh).
CPU_GHZ=${CPU_GHZ:-2.2}

# =========================================================================
log "1/8  base apt packages"
# =========================================================================
sudo apt-get update -qq
sudo apt-get install -y git curl ca-certificates python3-venv python3-pip

# =========================================================================
log "2/8  clone sibling repos into $BASE"
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
log "3/8  build DeathStarBench wrk2 load generator -> /usr/local/bin/wrk"
# =========================================================================
sudo apt-get install -y build-essential libssl-dev libz-dev luarocks
sudo luarocks install luasocket
WRK2_DIR="$BASE/DeathStarBench/wrk2"
if [ -d "$WRK2_DIR" ]; then
  make -C "$WRK2_DIR"
  sudo cp "$WRK2_DIR/wrk" /usr/local/bin/wrk && sudo chmod +x /usr/local/bin/wrk
  echo "wrk installed: $(command -v wrk)"
else
  warn "no $WRK2_DIR — DeathStarBench clone is missing wrk2; skipping wrk build"
fi

# =========================================================================
log "4/8  toolchain (Go + protobuf + kompose) via install_blueprint_deps.sh"
# =========================================================================
bash "$SCRIPT_DIR/install_blueprint_deps.sh"
# make the freshly-installed Go usable for the remainder of THIS script
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"

# =========================================================================
log "5/8  python venv + deps -> $VENV"
# =========================================================================
[ -d "$VENV" ] || python3 -m venv "$VENV"
"$VENV/bin/pip" install --upgrade pip -q
"$VENV/bin/pip" install -q -r "$BP_ROOT/d2k8s/requirements.txt"   # pyyaml (d2k8s + pin/perf helpers)
"$VENV/bin/pip" install -q aiohttp                                # init_social_graph.py seeder
echo "venv ready: $VENV"

# =========================================================================
log "6/8  add $USER to the 'docker' group"
# =========================================================================
if id -nG "$USER" | tr ' ' '\n' | grep -qx docker; then
  echo "$USER already in docker group"
else
  sudo usermod -aG docker "$USER"
  warn "added $USER to 'docker' — log out/in (or run 'newgrp docker') for it to take effect"
fi

# =========================================================================
log "7/8  apply in-cluster docker registry (namespace 'registry', NodePort 30000)"
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
log "8/8  pin all node CPUs to ${CPU_GHZ} GHz (deterministic clocks across runs)"
# =========================================================================
if kubectl cluster-info >/dev/null 2>&1; then
  bash "$SCRIPT_DIR/pin-cpu-22ghz.sh" pin "$CPU_GHZ" --all \
    || warn "CPU pin reported errors on one or more nodes (see output above)"
  warn "CPU pin is NOT persistent across reboots — re-run 'utils/pin-cpu-22ghz.sh pin $CPU_GHZ --all' after any node reboot."
else
  warn "kubectl cluster not reachable — skipping CPU pin. Run later: utils/pin-cpu-22ghz.sh pin $CPU_GHZ --all"
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
