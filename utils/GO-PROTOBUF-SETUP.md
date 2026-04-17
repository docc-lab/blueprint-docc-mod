# Go and Protocol Buffers (minimal install)

Replace `go1.xx.x` and `linux-amd64` with the current version and your OS/architecture from [https://go.dev/dl/](https://go.dev/dl/).

## 1. Install Go (official tarball)

```bash
curl -fL "https://go.dev/dl/go1.xx.x.linux-amd64.tar.gz" -o /tmp/go.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.linux-amd64.tar.gz
```

## 2. Configure `PATH` in `~/.profile` and `~/.bashrc`

Append the same block to **both** files (login shells read `~/.profile`; many interactive terminals only read `~/.bashrc`).

```bash
if [ -d /usr/local/go/bin ]; then
    export PATH="/usr/local/go/bin:$PATH"
fi
if [ -d "$HOME/go/bin" ]; then
    export PATH="$HOME/go/bin:$PATH"
fi
```

## 3. Load `PATH` and install system `protoc`

```bash
source ~/.profile
sudo apt-get update
sudo apt-get install -y protobuf-compiler
```

## 4. Install Go `protoc` plugins

```bash
source ~/.profile
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

## 5. Verify

```bash
source ~/.profile
go version
protoc --version
command -v protoc-gen-go protoc-gen-go-grpc
```

Example for Go + gRPC generated code:

```bash
protoc --go_out=. --go-grpc_out=. your.proto
```

Add `google.golang.org/protobuf` and `google.golang.org/grpc` to each module with `go get` as needed for the generated code to compile.

## 6. Install Kompose

Replace `linux-amd64` with `linux-arm64` on ARM64. Binary installs to `/usr/local/bin` (already on a typical `PATH`).

```bash
curl -fL "https://github.com/kubernetes/kompose/releases/latest/download/kompose-linux-amd64" -o /tmp/kompose
chmod +x /tmp/kompose
sudo mv /tmp/kompose /usr/local/bin/kompose
kompose version
```
