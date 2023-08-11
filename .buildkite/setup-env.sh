#!/bin/bash

# General Setup
echo "+++ 🛠 Setup Environment"

# Install Go
echo "--- 🌐 Installing Go"
export PATH=$PATH:/usr/local/go/bin

# Install kind
echo "--- 🐳 Installing kind"
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.11.1/kind-linux-amd64
chmod +x ./kind
mv ./kind /usr/local/bin/kind

# Install Docker
echo "--- 📦 Installing Docker"
bash scripts/install-docker.sh

# Delete dangling clusters
echo "--- 🔥 Deleting dangling clusters"
kind delete clusters --all

# Install kubectl
echo "--- 🚀 Installing kubectl"
curl -LO https://dl.k8s.io/release/v1.27.3/bin/linux/amd64/kubectl
curl -LO "https://dl.k8s.io/release/v1.27.3/bin/linux/amd64/kubectl.sha256"
echo "$(cat kubectl.sha256)  kubectl" | sha256sum --check
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# Install Helm
echo "--- ⛵ Installing Helm"
curl -Lo helm.tar.gz https://get.helm.sh/helm-v3.12.2-linux-amd64.tar.gz
tar -zxvf helm.tar.gz
mv linux-amd64/helm /usr/local/bin/helm
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update

# Build KubeRay operator image
echo "--- 🏗 Building KubeRay operator image"
pushd ray-operator
IMG=kuberay/operator:nightly make docker-image
popd

# Install python 3.10 and pip
echo "--- 🐍 Installing Python 3.10 and pip"
apt-get update
apt-get install -y python3.10 python3-pip

# Install requirements
echo "--- 📋 Installing Python requirements"
pip install -r tests/framework/config/requirements.txt

# Bypass Git's ownership check
echo "--- 🛡 Bypassing Git ownership check"
git config --global --add safe.directory /workdir

# Wrap up
echo "+++ 🌟 Setup complete!"