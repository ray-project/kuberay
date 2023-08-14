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

# Install kubectl.
curl -LO https://dl.k8s.io/release/v1.27.3/bin/linux/amd64/kubectl
curl -LO "https://dl.k8s.io/release/v1.27.3/bin/linux/amd64/kubectl.sha256"
echo "$(cat kubectl.sha256)  kubectl" | sha256sum --check
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# Create the cluster
time kind create cluster --wait 120s --config tests/framework/config/kind-config-buildkite.yml
docker ps

# Configure kubectl for kind cluster
kubectl config set clusters.kind-kind.server https://docker:6443

# Verify that kubectl works
kubectl version
kubectl cluster-info
kubectl get nodes
kubectl get pods --all-namespaces

# Install Helm
curl -Lo helm.tar.gz https://get.helm.sh/helm-v3.12.2-linux-amd64.tar.gz
tar -zxvf helm.tar.gz
mv linux-amd64/helm /usr/local/bin/helm
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update

# Install KubeRay operator
pushd ray-operator
IMG=kuberay/operator:nightly make docker-image
kind load docker-image kuberay/operator:nightly
popd

pushd helm-chart/kuberay-operator
helm install kuberay-operator --set image.repository=kuberay/operator --set image.tag=nightly --wait --timeout=5m0s .
popd

echo "Kuberay operator successfully installed."

# Remove kind cluster
kind delete cluster

# Install python 3.11 and pip
apt-get update
apt-get install -y python3.11 python3.11-venv
python3 -m venv .venv
source .venv/bin/activate

# Install requirements
pip install -r tests/framework/config/requirements.txt

# Bypass Git's ownership check due to unconventional user IDs in Docker containers
git config --global --add safe.directory /workdir
