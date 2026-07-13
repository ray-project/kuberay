#!/usr/bin/env bash
# Bootstrap script for the Federated RayCluster local development environment.
#
# Creates 3 kind clusters with Cilium CNI and ClusterMesh for cross-cluster
# Pod-to-Pod connectivity.
set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INFRA_DIR="${SCRIPT_DIR}/../infra"

PRIMARY_CLUSTER="frc-primary"
MEMBER_A_CLUSTER="frc-member-a"
MEMBER_B_CLUSTER="frc-member-b"
ALL_CLUSTERS=("${PRIMARY_CLUSTER}" "${MEMBER_A_CLUSTER}" "${MEMBER_B_CLUSTER}")

PRIMARY_CONTEXT="kind-${PRIMARY_CLUSTER}"
MEMBER_A_CONTEXT="kind-${MEMBER_A_CLUSTER}"
MEMBER_B_CONTEXT="kind-${MEMBER_B_CLUSTER}"

CLUSTERMESH_SERVICE_TYPE="${CLUSTERMESH_SERVICE_TYPE:-NodePort}"

# Cilium images to pre-pull and load into kind clusters.
# Pre-loading avoids 27 parallel pulls from quay.io (9 nodes x 3 images)
# and makes the setup work reliably regardless of network speed.
CILIUM_IMAGES=(
  "quay.io/cilium/cilium:v1.19.1"
  "quay.io/cilium/cilium-envoy:v1.35.9-1770979049-232ed4a26881e4ab4f766f251f258ed424fff663"
  "quay.io/cilium/operator-generic:v1.19.1"
  "quay.io/cilium/clustermesh-apiserver:v1.19.1"
  "quay.io/cilium/certgen:v0.3.2"
)

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: missing required command: $1" >&2
    exit 1
  }
}

# ── Step 1: Check dependencies ───────────────────────────────────────────────
echo "[1/8] Checking dependencies..."
need_cmd docker
need_cmd kubectl
need_cmd helm
need_cmd kind
need_cmd cilium

# ── Step 2: Pull Cilium images to host ───────────────────────────────────────
echo "[2/8] Pulling Cilium images to host (if not already cached)..."
for image in "${CILIUM_IMAGES[@]}"; do
  if docker image inspect "${image}" >/dev/null 2>&1; then
    echo "  Already cached: ${image}"
  else
    echo "  Pulling: ${image}..."
    docker pull "${image}"
  fi
done

# ── Step 3: Create kind clusters ─────────────────────────────────────────────
echo "[3/8] Creating kind clusters..."
for cluster in "${ALL_CLUSTERS[@]}"; do
  kind delete cluster --name "${cluster}" 2>/dev/null || true
done

kind create cluster --config "${INFRA_DIR}/kind/frc-primary.yaml"
kind create cluster --config "${INFRA_DIR}/kind/frc-member-a.yaml"
kind create cluster --config "${INFRA_DIR}/kind/frc-member-b.yaml"

# ── Step 4: Load Cilium images into kind clusters ────────────────────────────
echo "[4/8] Loading Cilium images into kind clusters..."
for cluster in "${ALL_CLUSTERS[@]}"; do
  echo "  ${cluster}: loading ${#CILIUM_IMAGES[@]} images..."
  for image in "${CILIUM_IMAGES[@]}"; do
    if ! kind load docker-image "${image}" --name="${cluster}" >/dev/null 2>&1; then
      echo "    WARNING: failed to load ${image} into ${cluster}"
    fi
  done
done

# ── Step 5: Install Cilium ───────────────────────────────────────────────────
echo "[5/8] Installing Cilium on all clusters..."

install_cilium() {
  local ctx="$1" name="$2" id="$3"
  echo "  Installing Cilium on ${name} (id=${id})..."
  cilium install \
    --context "${ctx}" \
    --set cluster.name="${name}" \
    --set cluster.id="${id}" \
    --set ipam.mode=kubernetes \
    --set image.useDigest=false \
    --set operator.image.useDigest=false \
    --set envoy.image.useDigest=false \
    --set clustermesh.apiserver.image.useDigest=false \
    --set certgen.image.useDigest=false
}

install_cilium "${PRIMARY_CONTEXT}"  "${PRIMARY_CLUSTER}"  1
install_cilium "${MEMBER_A_CONTEXT}" "${MEMBER_A_CLUSTER}" 2
install_cilium "${MEMBER_B_CONTEXT}" "${MEMBER_B_CLUSTER}" 3

echo "  Waiting for Cilium to be ready (timeout: 15m per cluster)..."
cilium status --context "${PRIMARY_CONTEXT}" --wait --wait-duration 15m
cilium status --context "${MEMBER_A_CONTEXT}" --wait --wait-duration 15m
cilium status --context "${MEMBER_B_CONTEXT}" --wait --wait-duration 15m

# ── Step 6: Enable ClusterMesh ───────────────────────────────────────────────
echo "[6/8] Enabling ClusterMesh (${CLUSTERMESH_SERVICE_TYPE} mode)..."
cilium clustermesh enable --context "${PRIMARY_CONTEXT}" --service-type "${CLUSTERMESH_SERVICE_TYPE}"
cilium clustermesh enable --context "${MEMBER_A_CONTEXT}" --service-type "${CLUSTERMESH_SERVICE_TYPE}"
cilium clustermesh enable --context "${MEMBER_B_CONTEXT}" --service-type "${CLUSTERMESH_SERVICE_TYPE}"

echo "  Waiting for ClusterMesh to be ready (timeout: 15m per cluster)..."
cilium clustermesh status --context "${PRIMARY_CONTEXT}" --wait --wait-duration 15m
cilium clustermesh status --context "${MEMBER_A_CONTEXT}" --wait --wait-duration 15m
cilium clustermesh status --context "${MEMBER_B_CONTEXT}" --wait --wait-duration 15m

# ── Step 7: Connect clusters ────────────────────────────────────────────────
echo "[7/8] Connecting clusters into mesh..."
# Each cluster has its own Cilium CA; --allow-mismatching-ca bundles remote CAs
# into the trust chain so mutual TLS works across independently-installed clusters.
cilium clustermesh connect --context "${PRIMARY_CONTEXT}" --destination-context "${MEMBER_A_CONTEXT}" --allow-mismatching-ca
cilium clustermesh connect --context "${PRIMARY_CONTEXT}" --destination-context "${MEMBER_B_CONTEXT}" --allow-mismatching-ca
cilium clustermesh connect --context "${MEMBER_A_CONTEXT}" --destination-context "${MEMBER_B_CONTEXT}" --allow-mismatching-ca

echo "  Waiting for mesh connections to stabilize (timeout: 15m per cluster)..."
cilium clustermesh status --context "${PRIMARY_CONTEXT}" --wait --wait-duration 15m
cilium clustermesh status --context "${MEMBER_A_CONTEXT}" --wait --wait-duration 15m
cilium clustermesh status --context "${MEMBER_B_CONTEXT}" --wait --wait-duration 15m

# ── Step 8: Verify ──────────────────────────────────────────────────────────
echo "[8/8] Verifying cluster connectivity..."
kubectl --context "${PRIMARY_CONTEXT}" get nodes
kubectl --context "${MEMBER_A_CONTEXT}" get nodes
kubectl --context "${MEMBER_B_CONTEXT}" get nodes

echo ""
echo "Done!"
echo
echo "Contexts:"
echo "  ${PRIMARY_CONTEXT}"
echo "  ${MEMBER_A_CONTEXT}"
echo "  ${MEMBER_B_CONTEXT}"
echo
echo "Next steps:"
echo "  # Run cross-cluster connectivity test:"
echo "  ./hack/smoke-test.sh"
echo
echo "  # Deploy a cross-cluster Ray cluster:"
echo "  ./hack/deploy-ray-cluster.sh"
