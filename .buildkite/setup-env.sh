#!/bin/bash

# Install Go
export PATH=$PATH:/usr/local/go/bin

# Install kind
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.11.1/kind-linux-amd64
chmod +x ./kind
mv ./kind /usr/local/bin/kind

# Install Docker
bash scripts/install-docker.sh

# Delete dangling clusters
kind delete clusters --all

# Install Helm
curl -Lo helm.tar.gz https://get.helm.sh/helm-v3.12.2-linux-amd64.tar.gz
tar -zxvf helm.tar.gz
mv linux-amd64/helm /usr/local/bin/helm
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update

# Install python 3.11 and pip
apt-get update
apt-get install -y python3.11 python3.11-venv
python3 -m venv .venv
source .venv/bin/activate

# Install requirements
pip install -r tests/framework/config/requirements.txt

# Bypass Git's ownership check due to unconventional user IDs in Docker containers
git config --global --add safe.directory /workdir
