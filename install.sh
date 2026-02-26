#!/usr/bin/env bash
set -euo pipefail

GO_VERSION="${GO_VERSION:-1.24.9}"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"
GOROOT="/usr/local/go"

PROFILE_FILE="$HOME/.profile"
BASHRC_FILE="$HOME/.bashrc"

echo "▶ Installing Go ${GO_VERSION} to ${GOROOT} ..."

# Download
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
wget -qO "$tmp/$GO_TARBALL" "$GO_URL"

# Install (requires sudo)
sudo rm -rf "$GOROOT"
sudo tar -C /usr/local -xzf "$tmp/$GO_TARBALL"

# Add Go to PATH (write *literal* $PATH, don't expand it now)
add_line_if_missing () {
  local line="$1"
  local file="$2"
  mkdir -p "$(dirname "$file")" || true
  touch "$file"
  grep -Fqx "$line" "$file" || echo "$line" >> "$file"
}

GO_PATH_LINE='export PATH="$PATH:/usr/local/go/bin"'
add_line_if_missing "$GO_PATH_LINE" "$PROFILE_FILE"
add_line_if_missing "$GO_PATH_LINE" "$BASHRC_FILE"

# Load PATH for THIS script run so `go` works immediately
export PATH="$PATH:/usr/local/go/bin"

echo "▶ go version:"
go version

echo "▶ Installing protobuf compiler (protoc) ..."
sudo apt-get update -y
sudo apt-get install -y protobuf-compiler

echo "▶ Ensuring GOPATH/bin is on PATH ..."
GOPATH_BIN_LINE='export PATH="$PATH:$(go env GOPATH)/bin"'
add_line_if_missing "$GOPATH_BIN_LINE" "$PROFILE_FILE"
add_line_if_missing "$GOPATH_BIN_LINE" "$BASHRC_FILE"

# Load it for THIS script too
export PATH="$PATH:$(go env GOPATH)/bin"

echo "▶ Installing protoc-gen-go and protoc-gen-go-grpc ..."
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

echo "▶ Verifying installs:"
command -v protoc >/dev/null && protoc --version
command -v protoc-gen-go >/dev/null && protoc-gen-go --version || true
command -v protoc-gen-go-grpc >/dev/null && protoc-gen-go-grpc --version || true

echo "✅ Done."
echo "👉 Open a NEW terminal (or run: source ~/.profile) to pick up PATH changes."
