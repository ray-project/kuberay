#!/bin/bash

# set -euxo pipefail:
# -e: Exit immediately if a command exits with a non-zero status.
# -u: Treat unset variables as an error when substituting.
# -x: Print commands and their arguments as they are executed (useful for debugging).
# -o pipefail: The return value of a pipeline is the status of the last command to exit with a non-zero status,
#              or zero if all commands in the pipeline exit successfully.
set -euxo pipefail

# --- Configuration Variables ---
# IMPORTANT: Customize these variables for your environment and project.
# Use your Docker registry and a unique image name.
# For Kind, if not pushing to a remote registry, you can use "kind-registry" or similar.
IMAGE_REPO="yourregistry" # e.g., "gcr.io/my-gcp-project" or "docker.io/myusername"
IMAGE_NAME="kuberay-operator"
# Set your desired image tag. A unique tag like a timestamp is recommended during development.
IMAGE_TAG="nightly" # Example: "my-custom-build-20250625-1030" or "nightly"

KIND_CLUSTER_NAME="kind" # Your Kind cluster name
OPERATOR_NAMESPACE="default" # Or "kuberay-system", "kuberay-operator", etc.
HELM_RELEASE_NAME="kuberay-operator" # The Helm release name you use

HELM_CHART_PATH="../helm-chart/kuberay-operator" # Path to your Helm chart

# --- Script Logic ---

echo "--- Checking for Kind Cluster ---"
# Check if the Kind cluster already exists
if ! kind get clusters | grep -q "${KIND_CLUSTER_NAME}"; then
  echo "Kind cluster '${KIND_CLUSTER_NAME}' not found. Creating it..."
  # You can customize your Kind cluster creation command here if needed.
  kind create cluster --name "${KIND_CLUSTER_NAME}" --image=kindest/node:v1.24.0
else
  echo "Kind cluster '${KIND_CLUSTER_NAME}' already exists. Skipping creation."
fi

# Step 2: Modify KubeRay source code (Manual Step)
# For example, add a log by adding setupLog.Info("Hello KubeRay") in the function `main` in `main.go`.

echo "--- Building Docker Image ---"
FULL_IMAGE_NAME="${IMAGE_REPO}/${IMAGE_NAME}:${IMAGE_TAG}"
echo "Building image: ${FULL_IMAGE_NAME}"

# Delete existing local image to ensure a fresh build and avoid caching issues.
# The '|| true' prevents the script from exiting if the image doesn't exist yet.
docker rmi -f "${FULL_IMAGE_NAME}" || true

# Step 3: Build an image
# This command will copy the source code directory into the image, and build it.
# Command: IMG={IMG_REPO}:{IMG_TAG} make docker-build
make docker-build IMG="${FULL_IMAGE_NAME}"

# To skip Go project compilation, run the following command instead:
# IMG=kuberay/operator:nightly make docker-image

echo "--- Loading Image into Kind Cluster ---"
# Step 4: Load the custom KubeRay image into the Kind cluster.
# Command: kind load docker-image {IMG_REPO}:{IMG_TAG}
kind load docker-image "${FULL_IMAGE_NAME}" --name "${KIND_CLUSTER_NAME}"

echo "--- Uninstalling and Installing KubeRay Operator ---"
# Check if the Helm release exists before trying to uninstall
if helm list --namespace "${OPERATOR_NAMESPACE}" | grep -q "${HELM_RELEASE_NAME}"; then
  echo "Uninstalling existing Helm release: ${HELM_RELEASE_NAME}"
  helm uninstall "${HELM_RELEASE_NAME}" --namespace "${OPERATOR_NAMESPACE}"
  echo "Waiting for resources to be terminated..."
  sleep 10 # Give Kubernetes some time to clean up
else
  echo "Helm release '${HELM_RELEASE_NAME}' not found. Skipping uninstall."
fi

# Step 6: Install KubeRay operator with the custom image via local Helm chart
# (Path: helm-chart/kuberay-operator)
# Command: helm install kuberay-operator --set image.repository={IMG_REPO} --set image.tag={IMG_TAG} ../helm-chart/kuberay-operator
echo "Installing new Helm release: ${HELM_RELEASE_NAME}"
helm install "${HELM_RELEASE_NAME}" "${HELM_CHART_PATH}" \
  --namespace "${OPERATOR_NAMESPACE}" \
  --set "image.repository=${IMAGE_REPO}/${IMAGE_NAME}" \
  --set "image.tag=${IMAGE_TAG}"

echo "--- Waiting for Deployment Rollout ---"
# Wait for the operator deployment to successfully roll out.
# This ensures the new pod is running before we check logs.
kubectl rollout status deployment "${HELM_RELEASE_NAME}" --namespace "${OPERATOR_NAMESPACE}" --timeout=5m

echo "--- Streaming Controller Logs (Ctrl+C to stop) ---"
# Step 7: Check the logs
# Note: This command directly targets the deployment for logs.
# kubectl logs -f deployments/"${HELM_RELEASE_NAME}" -n "${OPERATOR_NAMESPACE}"

echo "--- Script Completed ---"
