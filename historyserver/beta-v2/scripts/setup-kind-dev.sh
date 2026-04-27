#!/usr/bin/env bash
#
# One-shot dev bootstrap for the History Server. Idempotent — safe to re-run.

set -euo pipefail

CLUSTER_NAME=${CLUSTER_NAME:-beta-v2-dev}
REG_NAME=${REG_NAME:-kind-registry}
REG_PORT=${REG_PORT:-5001}
MINIO_NAMESPACE=${MINIO_NAMESPACE:-minio-dev}
KUBERAY_NAMESPACE=${KUBERAY_NAMESPACE:-ray-system}
# KubeRay operator version. Override with KUBERAY_VERSION=<x.y.z>.
KUBERAY_VERSION=${KUBERAY_VERSION:-1.6.0}
BETA_V2_IMAGE_TAG=${BETA_V2_IMAGE_TAG:-dev}

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BETA_V2_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
# REPO_ROOT lets make -C resolve the Dockerfile's relative COPY paths.
REPO_ROOT=$(cd "$BETA_V2_DIR/.." && pwd)

log()  { echo "[$(date +%H:%M:%S)] $*"; }
fail() { echo "ERROR: $*" >&2; exit 1; }

# 0. Preflight — fail fast if required tools are missing.
log "Preflight: checking required tools"
MISSING=()
for tool in kind kubectl docker helm; do
  command -v "$tool" >/dev/null 2>&1 || MISSING+=("$tool")
done
if [ ${#MISSING[@]} -gt 0 ]; then
  fail "missing required tools: ${MISSING[*]} (install them and re-run)"
fi

# 1. Local docker registry — kind nodes pull from here via the registry mirror in step 2.
if ! docker inspect "$REG_NAME" >/dev/null 2>&1; then
  log "Starting local registry '$REG_NAME' on 127.0.0.1:$REG_PORT"
  docker run -d --restart=always \
    -p "127.0.0.1:${REG_PORT}:5000" \
    --name "$REG_NAME" \
    registry:2 >/dev/null
else
  log "Local registry '$REG_NAME' already running"
fi

# 2. kind cluster with a containerd mirror pointing at the local registry.
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  log "kind cluster '$CLUSTER_NAME' already exists"
else
  log "Creating kind cluster '$CLUSTER_NAME'"
  cat <<EOF | kind create cluster --name "$CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REG_PORT}"]
    endpoint = ["http://${REG_NAME}:5000"]
EOF
fi

# Connect registry to the kind network so containerd can resolve the mirror.
if ! docker network inspect kind 2>/dev/null | grep -q "\"${REG_NAME}\""; then
  log "Connecting '$REG_NAME' to the 'kind' docker network"
  docker network connect kind "$REG_NAME" 2>/dev/null || true
fi

kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

# 3. KubeRay operator — installs the RayCluster CRD beta-v2 needs.
if kubectl get crd rayclusters.ray.io >/dev/null 2>&1; then
  log "KubeRay operator already installed (CRD present)"
else
  log "Installing KubeRay operator v${KUBERAY_VERSION}"
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/ >/dev/null 2>&1 || true
  helm repo update >/dev/null
  helm install kuberay-operator kuberay/kuberay-operator \
    --version "$KUBERAY_VERSION" \
    --create-namespace \
    --namespace "$KUBERAY_NAMESPACE" >/dev/null
  kubectl -n "$KUBERAY_NAMESPACE" wait --for=condition=Available --timeout=180s \
    deploy/kuberay-operator
fi

# 4. MinIO + credentials. minio.yaml ships:
#    - Namespace minio-dev + `minio-creds` Secret (MinIO server login)
#    - `historyserver-beta-v2-s3` Secret in `default` ns (HS Deployment consumer)
#    - MinIO Deployment + Service
#    The `ray-historyserver` bucket is NOT pre-created — the collector creates
#    it on first write.
log "Applying beta-v2/config/minio.yaml (MinIO deploy + both Secrets)"
kubectl apply -f "$BETA_V2_DIR/config/minio.yaml"
kubectl -n "$MINIO_NAMESPACE" wait --for=condition=Available --timeout=180s \
  deploy/minio

# 5. Build + push the beta-v2 image. Driven from historyserver/ so the
# Dockerfile's relative COPY paths resolve.
log "Building + pushing beta-v2 image to localhost:${REG_PORT} (tag=${BETA_V2_IMAGE_TAG})"
make -C "$REPO_ROOT" \
  BETA_V2_IMAGE_REGISTRY="localhost:${REG_PORT}" \
  BETA_V2_IMAGE_TAG="${BETA_V2_IMAGE_TAG}" \
  pushbeta-v2

# 6. Apply beta-v2 manifests. Sed the image ref to point at the local registry.
log "Applying beta-v2/config/ manifests"
kubectl apply -f "$BETA_V2_DIR/config/rbac.yaml"
kubectl apply -f "$BETA_V2_DIR/config/configmap-s3.yaml"

# sed -E for portability between GNU and BSD sed (macOS).
sed -E "s|image: historyserver-beta-v2-historyserver:v0\\.1\\.0|image: localhost:${REG_PORT}/historyserver-beta-v2-historyserver:${BETA_V2_IMAGE_TAG}|g" \
  "$BETA_V2_DIR/config/historyserver.yaml" | kubectl apply -f -

# 7. Wait + print access hints.
log "Waiting for beta-v2 deployment to become Available"
kubectl -n default wait --for=condition=Available --timeout=180s deploy/historyserver-beta-v2

log "Done. Stack is running. See beta-v2/README.md §4 for smoke-test recipes."
log "Quick start:"
log "  kubectl port-forward svc/historyserver-beta-v2 8080:30080"
