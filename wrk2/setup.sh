#!/bin/bash
set -euo pipefail

WRK2_DIR="$HOME/blueprint-docc-mod/wrk2"
LUAJIT_DIR="$WRK2_DIR/deps/luajit"

mkdir -p "$WRK2_DIR/deps"

# Install required system packages (build tools + OpenSSL headers)
if command -v apt-get >/dev/null 2>&1; then
  REQUIRED_PACKAGES=(build-essential libssl-dev)
  MISSING_PACKAGES=()
  for pkg in "${REQUIRED_PACKAGES[@]}"; do
    dpkg -s "$pkg" >/dev/null 2>&1 || MISSING_PACKAGES+=("$pkg")
  done

  if [ "${#MISSING_PACKAGES[@]}" -gt 0 ]; then
    echo "[INFO] Installing packages: ${MISSING_PACKAGES[*]}"
    sudo apt-get update
    sudo apt-get install -y "${MISSING_PACKAGES[@]}"
  else
    echo "[INFO] Required system packages already installed"
  fi
else
  echo "[WARNING] apt-get not available. Ensure build-essential and libssl-dev are installed manually."
fi

if [ ! -d "$LUAJIT_DIR" ]; then
  echo "[INFO] Cloning LuaJIT into deps/luajit..."
  git clone https://github.com/LuaJIT/LuaJIT.git "$LUAJIT_DIR"
else
  echo "[INFO] deps/luajit already exists; pulling latest..."
  git -C "$LUAJIT_DIR" pull --ff-only
fi

echo "[INFO] Building LuaJIT..."
make -C "$LUAJIT_DIR" BUILDMODE=static

echo "[INFO] Building wrk2..."
cd "$WRK2_DIR"
make

echo "[SUCCESS] wrk2 build complete at $WRK2_DIR/wrk"