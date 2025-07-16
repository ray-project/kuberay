#!/bin/bash

set -euxo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PROJECT_ROOT="${SCRIPT_DIR}/../.." # hack/ -> ray-operator/ -> kuberay/

# Path to your Helm chart, relative to the PROJECT_ROOT (kuberay/)
HELM_CHART_RELATIVE_PATH="helm-chart/kuberay-operator"
HELM_CHART_PATH="${PROJECT_ROOT}/${HELM_CHART_RELATIVE_PATH}"

RAY_OPERATOR_PATH="${PROJECT_ROOT}/ray-operator"

# --- Configuration Variables ---
IMAGE_NAME="kuberay-operator"
IMAGE_TAG="kuberay-dev"
KIND_CLUSTER_NAME="kind"
KIND_NODE_IMAGE="kindest/node:v1.24.0"
OPERATOR_NAMESPACE="default"
HELM_RELEASE_NAME="kuberay-operator"

delete_kind_cluster() {
  echo "--- Deleting Kind Cluster ---"
  if kind get clusters | grep -q "${KIND_CLUSTER_NAME}"; then
    echo "Kind cluster '${KIND_CLUSTER_NAME}' found. Deleting it..."
    kind delete cluster --name "${KIND_CLUSTER_NAME}"
  else
    echo "Kind cluster '${KIND_CLUSTER_NAME}' not found. Skipping deletion."
  fi
}

create_kind_cluster() {
  echo "--- Creating Kind Cluster ---"
  echo "Creating Kind cluster '${KIND_CLUSTER_NAME}' with image '${KIND_NODE_IMAGE}'..."
  kind create cluster --name "${KIND_CLUSTER_NAME}" --image="${KIND_NODE_IMAGE}"
  echo "Kind cluster nodes are ready."
}

# Parsing for arguements
SHOW_LOGS=false 
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -l|--logs)
      SHOW_LOGS=true
      shift # Remove --logs or -l from processing
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Step 1: Ensure a clean Kind cluster
echo "--- Ensuring Clean Kind Cluster ---"
delete_kind_cluster
create_kind_cluster

echo "--- Building Docker Image ---"
FULL_IMAGE_NAME="${IMAGE_NAME}:${IMAGE_TAG}"
echo "Building image: ${FULL_IMAGE_NAME}"

# Execute make docker-build from the ray-operator directory
echo "Running make docker-build from ray-operator path: ${RAY_OPERATOR_PATH}"
make -C "${RAY_OPERATOR_PATH}" docker-build IMG="${FULL_IMAGE_NAME}"


# Step 4: Load the custom KubeRay image into the Kind cluster
echo "--- Loading Image into Kind Cluster ---"
kind load docker-image "${FULL_IMAGE_NAME}" --name "${KIND_CLUSTER_NAME}"

echo "--- Preparing Namespace for Operator Deployment ---"
kubectl create namespace "${OPERATOR_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# Step 5: Keep consistency and Syncing
echo "--- Keep consistency and Syncing (Project-Specific Synchronization) ---"
echo "Running make sync from ray-operator path: ${RAY_OPERATOR_PATH}"
make -C "${RAY_OPERATOR_PATH}" sync

# Step 6: Install KubeRay operator with the custom image via local Helm chart
echo "--- Installing KubeRay Operator via Helm Chart ---"
echo "Installing new Helm release: ${HELM_RELEASE_NAME}"
echo "Helm chart path: ${HELM_CHART_PATH}"
helm install "${HELM_RELEASE_NAME}" "${HELM_CHART_PATH}" \
  --namespace "${OPERATOR_NAMESPACE}" \
  --create-namespace \
  --set "image.repository=${IMAGE_NAME}" \
  --set "image.tag=${IMAGE_TAG}"

echo "--- Waiting for Deployment Rollout ---"
kubectl rollout status deployment "${HELM_RELEASE_NAME}" --namespace "${OPERATOR_NAMESPACE}" --timeout=5m

# Step 7: Check the logs
if [ "$SHOW_LOGS" = true ]; then
  echo "--- Streaming Controller Logs (Ctrl+C to stop) ---"
  kubectl logs -f deployment/"${HELM_RELEASE_NAME}" -n "${OPERATOR_NAMESPACE}"
fi

echo "--- Script Completed ---"
