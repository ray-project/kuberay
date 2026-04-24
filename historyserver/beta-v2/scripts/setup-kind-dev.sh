#!/usr/bin/env bash
#
# One-shot dev bootstrap for History Server v2 beta-v2 (lazy mode).
#
# Provisions a local k8s stack end-to-end:
#   1. Local docker registry (so kind can pull images we just built).
#   2. kind cluster with a containerd registry mirror pointing at that registry.
#   3. KubeRay operator (provides the RayCluster CRD beta-v2 List/Gets).
#   4. MinIO (single-pod, for S3-compatible snapshot storage) —
#      applied straight from beta-v2/config/minio.yaml. That manifest
#      also ships the `historyserver-beta-v2-s3` Secret beta-v2 consumes,
#      so there is no inline Secret duplication here. The `ray-historyserver`
#      bucket is NOT pre-created: the collector (running inside the
#      sample RayCluster) creates it on first write.
#   5. Build + push the beta-v2 image (single binary) to the registry.
#   6. Apply beta-v2/config/ manifests with image ref patched to the local
#      registry.
#   7. Wait for deployment + print port-forward hints.
#
# Idempotent: safe to re-run — each step skips its work if already done.
#
# References:
#   - kind + local registry: https://kind.sigs.k8s.io/docs/user/local-registry/
#   - KubeRay helm chart:    https://github.com/ray-project/kuberay-helm
#   - MinIO minimal deploy:  https://min.io/docs/minio/kubernetes/upstream/

set -euo pipefail

CLUSTER_NAME=${CLUSTER_NAME:-beta-v2-dev}
REG_NAME=${REG_NAME:-kind-registry}
REG_PORT=${REG_PORT:-5001}
MINIO_NAMESPACE=${MINIO_NAMESPACE:-minio-dev}
KUBERAY_NAMESPACE=${KUBERAY_NAMESPACE:-ray-system}
# KubeRay operator version. beta-v2 is tested against v1.6.0+; v1.6.0 is
# the pinned default so fresh dev environments get a known-good CRD/operator
# combo without surprise upgrades. Override with KUBERAY_VERSION=<x.y.z>.
KUBERAY_VERSION=${KUBERAY_VERSION:-1.6.0}
BETA_V2_IMAGE_TAG=${BETA_V2_IMAGE_TAG:-dev}

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# BETA_V2_DIR points at historyserver/beta-v2/ (one level up from scripts/).
# REPO_ROOT points at historyserver/ so `make -C` resolves the Dockerfile
# relative paths.
BETA_V2_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO_ROOT=$(cd "$BETA_V2_DIR/.." && pwd)

log()  { echo "[$(date +%H:%M:%S)] $*"; }
fail() { echo "ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 0. Preflight — fail fast if tools are missing so the user doesn't debug
#    half-configured state.
# ---------------------------------------------------------------------------
log "Preflight: checking required tools"
MISSING=()
for tool in kind kubectl docker helm; do
  command -v "$tool" >/dev/null 2>&1 || MISSING+=("$tool")
done
if [ ${#MISSING[@]} -gt 0 ]; then
  fail "missing required tools: ${MISSING[*]} (install them and re-run)"
fi

# ---------------------------------------------------------------------------
# 1. Local docker registry — lets us push images that the kind nodes can pull
#    via the containerd mirror configured in step 2.
# ---------------------------------------------------------------------------
if ! docker inspect "$REG_NAME" >/dev/null 2>&1; then
  log "Starting local registry '$REG_NAME' on 127.0.0.1:$REG_PORT"
  docker run -d --restart=always \
    -p "127.0.0.1:${REG_PORT}:5000" \
    --name "$REG_NAME" \
    registry:2 >/dev/null
else
  log "Local registry '$REG_NAME' already running"
fi

# ---------------------------------------------------------------------------
# 2. kind cluster with registry mirror — follows the upstream recipe.
# ---------------------------------------------------------------------------
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

# Ensure kubectl points at the new cluster.
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# 3. KubeRay operator — installs the RayCluster CRD beta-v2 needs.
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# 4. MinIO + credentials — apply beta-v2/config/minio.yaml as-is.
#
#    That single file ships:
#      - Namespace minio-dev
#      - `minio-creds` Secret (MinIO server login)
#      - `historyserver-beta-v2-s3` Secret (in `default` namespace) that
#        the historyserver Deployment already references via secretKeyRef
#      - MinIO Deployment + Service
#
#    WHY not kubectl create-secret here as well: the Secret already lives
#    in minio.yaml. Duplicating it in the script would drift from the
#    committed manifest — any edit would have to be made in two places.
#
#    WHY no mc-init step: the collector container inside the sample
#    RayCluster creates the bucket on first write, so we don't need to
#    pre-create `ray-historyserver` here.
# ---------------------------------------------------------------------------
log "Applying beta-v2/config/minio.yaml (MinIO deploy + both Secrets)"
kubectl apply -f "$BETA_V2_DIR/config/minio.yaml"
kubectl -n "$MINIO_NAMESPACE" wait --for=condition=Available --timeout=180s \
  deploy/minio

# ---------------------------------------------------------------------------
# 5. Build + push the beta-v2 image to the local registry.
#    We drive `make` from the historyserver/ dir so the Dockerfile's
#    relative COPY paths resolve.
# ---------------------------------------------------------------------------
log "Building + pushing beta-v2 image to localhost:${REG_PORT} (tag=${BETA_V2_IMAGE_TAG})"
make -C "$REPO_ROOT" \
  BETA_V2_IMAGE_REGISTRY="localhost:${REG_PORT}" \
  BETA_V2_IMAGE_TAG="${BETA_V2_IMAGE_TAG}" \
  pushbeta-v2

# ---------------------------------------------------------------------------
# 6. Apply the rest of the beta-v2 manifests. Sed the Deployment's image
#    ref so it points at our local registry instead of the placeholder in
#    the committed YAML.
# ---------------------------------------------------------------------------
log "Applying beta-v2/config/ manifests"
kubectl apply -f "$BETA_V2_DIR/config/rbac.yaml"
kubectl apply -f "$BETA_V2_DIR/config/configmap-s3.yaml"

# sed -E for portability between GNU and BSD sed (macOS).
sed -E "s|image: historyserver-beta-v2-historyserver:v0\\.1\\.0|image: localhost:${REG_PORT}/historyserver-beta-v2-historyserver:${BETA_V2_IMAGE_TAG}|g" \
  "$BETA_V2_DIR/config/historyserver.yaml" | kubectl apply -f -

# ---------------------------------------------------------------------------
# 7. Wait + print access hints.
# ---------------------------------------------------------------------------
log "Waiting for beta-v2 deployment to become Available"
kubectl -n default wait --for=condition=Available --timeout=180s deploy/historyserver-beta-v2

log "Done. beta-v2 stack is running."
log ""
log "Access the history server UI / API:"
log "  kubectl port-forward svc/historyserver-beta-v2 8080:30080"
log "  open http://localhost:8080"
log ""
log "Scrape metrics (combined API + metrics on :30080):"
log "  kubectl port-forward svc/historyserver-beta-v2 8080:30080"
log "  curl http://localhost:8080/metrics | grep -E 'enter_cluster|singleflight|sessions_'"
log ""
log "Smoke-test /enter_cluster blocking path (first call lazily builds the"
log "snapshot; the second call should be sub-ms):"
log "  time curl -s -o /dev/null http://localhost:8080/enter_cluster/<ns>/<name>/<session>"
log "  time curl -s -o /dev/null http://localhost:8080/enter_cluster/<ns>/<name>/<session>"
log ""
log "Tail logs:"
log "  kubectl logs -f -l app=historyserver-beta-v2 --tail=50"
log ""
log "MinIO console (creds: minioadmin / minioadmin):"
log "  kubectl -n ${MINIO_NAMESPACE} port-forward svc/minio-service 9001:9001"
log "  open http://localhost:9001"
