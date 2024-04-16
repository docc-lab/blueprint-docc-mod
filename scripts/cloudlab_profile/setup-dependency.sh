#!/bin/bash

# Install Docker
for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc; do
    sudo -E -u geniuser apt-get remove -y $pkg
done

sudo -E -u geniuser apt-get update
sudo -E -u geniuser apt-get install -y ca-certificates curl

sudo -E -u geniuser install -m 0755 -d /etc/apt/keyrings
sudo -E -u geniuser curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo -E -u geniuser chmod a+r /etc/apt/keyrings/docker.asc

sudo -E -u geniuser echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo -E -u geniuser apt-get update
sudo -E -u geniuser apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

sudo -E -u geniuser service docker start 
sudo -E -u geniuser systemctl start docker

# Dow-E nload and install Go
sudo -E -u geniuser wget https://go.dev/dl/go1.21.9.linux-amd64.tar.gz
sudo -E -u geniuser tar -xvf go1.21.9.linux-amd64.tar.gz -C /usr/local
sudo -E -u geniuser rm go1.21.9.linux-amd64.tar.gz

sudo -E -u geniuser echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.profile
sudo -E -u geniuser source ~/.profile

# Ins-E tall protobuf compiler
sudo -E -u geniuser sudo apt-get install -y protobuf-compiler

# Ins-E tall protocol buffer and gRPC Go plugins
sudo -E -u geniuser go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
sudo -E -u geniuser go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2

sudo -E -u geniuser echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.profile
sudo -E -u geniuser source ~/.profile

# Clo-E ne blueprint and install dependencies via Go
sudo -E -u geniuser git clone https://github.com/Blueprint-uServices/blueprint.git
sudo -E -u geniuser cd blueprint/runtime/
sudo -E -u geniuser go get go.opentelemetry.io/otel/sdk@v1.25.0
sudo -E -u geniuser cd blueprint/plugins
sudo -E -u geniuser go get go.opentelemetry.io/otel/sdk@v1.25.0