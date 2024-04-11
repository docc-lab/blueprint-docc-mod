#!/bin/bash

# Install Docker
for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc; do
    sudo apt-get remove -y $pkg
done

sudo apt-get update
sudo apt-get install -y ca-certificates curl

sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

sudo service docker start 
sudo systemctl start docker

# Download and install Go
wget https://go.dev/dl/go1.21.9.linux-amd64.tar.gz
sudo tar -xvf go1.21.9.linux-amd64.tar.gz -C /usr/local
rm go1.21.9.linux-amd64.tar.gz

echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.profile
source ~/.profile

# Install protobuf compiler
sudo apt-get install -y protobuf-compiler

# Install protocol buffer and gRPC Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2

echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.profile
source ~/.profile

# Clone blueprint and install dependencies via Go
git clone https://github.com/Blueprint-uServices/blueprint.git
cd blueprint/runtime/
go get go.opentelemetry.io/otel/sdk@v1.25.0
cd ../plugins
go get go.opentelemetry.io/otel/sdk@v1.25.0