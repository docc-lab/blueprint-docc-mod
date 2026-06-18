#!/usr/bin/env bash
#
# install_blueprint_deps.sh — install & configure all toolchain deps for Blueprint/DSB-SN.
#
#   1. Latest Go (from go.dev tarball, NOT apt) → /usr/local/go, PATH in ~/.bashrc + ~/.profile
#   2. System protobuf compiler (apt: protobuf-compiler)
#   3. Go protobuf plugins: protoc-gen-go + protoc-gen-go-grpc
#   4. kompose (latest GitHub release binary → /usr/local/bin)
#
# Idempotent: safe to re-run. Uses sudo for /usr/local + apt (passwordless sudo assumed).
#
set -euo pipefail

log(){ printf '\n\033[1;32m=== %s ===\033[0m\n' "$*"; }
warn(){ printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }

# --- arch detection -------------------------------------------------------
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac
OS=linux
GOROOT=/usr/local/go
GOBIN_USER="$HOME/go/bin"

# =========================================================================
log "1/4  Go (latest) → $GOROOT"
# =========================================================================
LATEST_GO=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -n1)   # e.g. go1.23.4
[ -n "$LATEST_GO" ] || { echo "could not resolve latest Go version" >&2; exit 1; }
CURRENT_GO=""
[ -x "$GOROOT/bin/go" ] && CURRENT_GO=$("$GOROOT/bin/go" version | awk '{print $3}')

if [ "$CURRENT_GO" = "$LATEST_GO" ]; then
  echo "Go already at $LATEST_GO — skipping download."
else
  TARBALL="${LATEST_GO}.${OS}-${ARCH}.tar.gz"
  echo "installing $LATEST_GO (was: ${CURRENT_GO:-none})"
  curl -fsSL -o "/tmp/${TARBALL}" "https://go.dev/dl/${TARBALL}"
  sudo rm -rf "$GOROOT"
  sudo tar -C /usr/local -xzf "/tmp/${TARBALL}"
  rm -f "/tmp/${TARBALL}"
fi

# PATH config in ~/.bashrc and ~/.profile (idempotent, marker-guarded)
PATH_BLOCK_BEGIN="# >>> blueprint go path >>>"
PATH_BLOCK_END="# <<< blueprint go path <<<"
read -r -d '' PATH_BLOCK <<EOF || true
$PATH_BLOCK_BEGIN
export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin
$PATH_BLOCK_END
EOF
for rc in "$HOME/.bashrc" "$HOME/.profile"; do
  touch "$rc"
  if grep -qF "$PATH_BLOCK_BEGIN" "$rc"; then
    echo "PATH block already present in $rc"
  else
    printf '\n%s\n' "$PATH_BLOCK" >> "$rc"
    echo "added Go PATH block to $rc"
  fi
done
# make go usable for the rest of THIS script
export PATH="$PATH:/usr/local/go/bin:$GOBIN_USER"
go version

# =========================================================================
log "2/4  system protobuf compiler (apt)"
# =========================================================================
sudo apt-get update -qq
sudo apt-get install -y protobuf-compiler
protoc --version

# =========================================================================
log "3/4  Go protobuf plugins (protoc-gen-go + protoc-gen-go-grpc)"
# =========================================================================
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
echo "installed to $(go env GOPATH)/bin :"
ls -1 "$(go env GOPATH)/bin" | grep -E 'protoc-gen-go' || warn "plugins not found in GOPATH/bin"

# =========================================================================
log "4/4  kompose (latest GitHub release)"
# =========================================================================
KOMPOSE_TAG=$(curl -fsSL https://api.github.com/repos/kubernetes/kompose/releases/latest \
              | grep -oE '"tag_name":\s*"[^"]+"' | head -n1 | cut -d'"' -f4)   # e.g. v1.34.0
[ -n "$KOMPOSE_TAG" ] || { echo "could not resolve latest kompose tag" >&2; exit 1; }
echo "kompose $KOMPOSE_TAG"
curl -fsSL -o /tmp/kompose \
  "https://github.com/kubernetes/kompose/releases/download/${KOMPOSE_TAG}/kompose-${OS}-${ARCH}"
chmod +x /tmp/kompose
sudo mv /tmp/kompose /usr/local/bin/kompose
kompose version

# =========================================================================
log "DONE — open a new shell (or 'source ~/.bashrc') to pick up PATH"
# =========================================================================
cat <<SUMMARY
  go               : $(go version | awk '{print $3, $4}')
  protoc           : $(protoc --version)
  protoc-gen-go    : $(command -v protoc-gen-go || echo MISSING)
  protoc-gen-go-grpc: $(command -v protoc-gen-go-grpc || echo MISSING)
  kompose          : $(kompose version 2>/dev/null | head -n1)
SUMMARY
