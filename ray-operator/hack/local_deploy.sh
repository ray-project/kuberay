#!/bin/bash

# set -euxo pipefail:
# -e: Exit immediately if a command exits with a non-zero status.
# -u: Treat unset variables as an error when substituting.
# -x: Print commands and their arguments as they are executed (useful for debugging).
# -o pipefail: The return value of a pipeline is the status of the last command to exit with a non-zero status,
#              or zero if all commands in the pipeline exit successfully
set -euxo pipefail

# --- Configuration Variables ---
IMAGE_REPO="yourregistry"
IMAGE_NAME="kuberay-operator"
# Set your desired image tag. A unique tag like a timestamp is recommended during development
IMAGE_TAG="nightly"

KIND_CLUSTER_NAME="kind"
KIND_NODE_IMAGE="kindest/node:v1.24.0" # Specify the Kind node image version for consistency
OPERATOR_NAMESPACE="default"
HELM_RELEASE_NAME="kuberay-operator"
HELM_CHART_PATH="../../helm-chart/kuberay-operator" # Path to your Helm chart

delete_kind_cluster() {
  echo "--- Deleting Kind Cluster ---"
  if kind get clusters | grep -q "${KIND_CLUSTER_NAME}"; then
    echo "Kind cluster '${KIND_CLUSTER_NAME}' found. Deleting it..."
    kind delete cluster --name "${KIND_CLUSTER_NAME}"
    echo "Kind cluster '${KIND_CLUSTER_NAME}' deleted."
  else
    echo "Kind cluster '${KIND_CLUSTER_NAME}' not found. Skipping deletion."
  fi
}
create_kind_cluster() {
  echo "--- Creating Kind Cluster ---"
  echo "Creating Kind cluster '${KIND_CLUSTER_NAME}' with image '${KIND_NODE_IMAGE}'..."
  # You can add --config kind-config.yaml here if you use a specific configuration file
  kind create cluster --name "${KIND_CLUSTER_NAME}" --image="${KIND_NODE_IMAGE}"
  echo "Waiting for Kind cluster nodes to be ready..."
  kubectl wait --for=condition=Ready nodes --all --timeout=5m
  echo "Kind cluster nodes are ready."
}

# Step 1: Ensure a clean Kind cluster (Addressing CRD Persistence)
# This step replaces the original "Checking for Kind Cluster" and implicitly handles Helm uninstall
echo "--- Ensuring Clean Kind Cluster (Addressing CRD Persistence) ---"
delete_kind_cluster
create_kind_cluster

echo "--- Building Docker Image ---"
FULL_IMAGE_NAME="${IMAGE_REPO}/${IMAGE_NAME}:${IMAGE_TAG}"
echo "Building image: ${FULL_IMAGE_NAME}"

# Delete existing local image to ensure a fresh build
# The '|| true' prevents the script from exiting if the image doesn't exist yet
docker rmi -f "${FULL_IMAGE_NAME}" || true

# Change directory to the project root before running make docker-build
# Assuming the script is in ray-operator/hack, ../ takes it to ray-operator/
echo "Changing directory to project root for docker-build: $(pwd)/.."
pushd ../

# Now execute the make command from the project root
make docker-build IMG="${FULL_IMAGE_NAME}"
popd
echo "Returned to original directory: $(pwd)"


# Step 4: Load the custom KubeRay image into the Kind cluster
echo "--- Loading Image into Kind Cluster ---"
# Command: kind load docker-image {IMG_REPO}:{IMG_TAG}
kind load docker-image "${FULL_IMAGE_NAME}" --name "${KIND_CLUSTER_NAME}"

# --- Prepare Namespace ---
echo "--- Preparing Namespace for Operator Deployment ---"
# Ensure the operator's namespace exists.
# Using apply instead of create makes it idempotent (won't fail if already exists)
kubectl create namespace "${OPERATOR_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# Step 5: Keep consistency and Syncing
# If you update RBAC or CRD, you need to synchronize them.
# With cluster recreation, CRD/RBAC sync is handled by helm install
echo "--- Keep consistency and Syncing (Project-Specific Synchronization) ---"
echo "Changing directory to project root for sync: $(pwd)/.."
pushd ../
make sync
popd
echo "Returned to original directory: $(pwd)"


# Step 6: Install KubeRay operator with the custom image via local Helm chart
# (Path: helm-chart/kuberay-operator)
# Command: helm install kuberay-operator --set image.repository={IMG_REPO} --set image.tag={IMG_TAG} ../helm-chart/kuberay-operator
echo "--- Installing KubeRay Operator via Helm Chart ---"
echo "Installing new Helm release: ${HELM_RELEASE_NAME}"
helm install "${HELM_RELEASE_NAME}" "${HELM_CHART_PATH}" \
  --namespace "${OPERATOR_NAMESPACE}" \
  --create-namespace \
  --set "image.repository=${IMAGE_REPO}/${IMAGE_NAME}" \
  --set "image.tag=${IMAGE_TAG}"

echo "--- Waiting for Deployment Rollout ---"
# Wait for the operator deployment to successfully roll out
# This ensures the new pod is running before we check logs
kubectl rollout status deployment "${HELM_RELEASE_NAME}" --namespace "${OPERATOR_NAMESPACE}" --timeout=5m

# Step 7: Check the logs
# echo "--- Streaming Controller Logs (Ctrl+C to stop) ---"
# kubectl logs -f deployment/"${HELM_RELEASE_NAME}" -n "${OPERATOR_NAMESPACE}"

echo "--- Script Completed ---"