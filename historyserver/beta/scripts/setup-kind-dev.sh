#!/usr/bin/env bash
#
# One-shot dev bootstrap for History Server v2 beta.
#
# Provisions a local k8s stack end-to-end:
#   1. Local docker registry (so kind can pull images we just built).
#   2. kind cluster with a containerd registry mirror pointing at that registry.
#   3. KubeRay operator (provides the RayCluster CRD the beta binaries List/Get).
#   4. MinIO (single-pod, for S3-compatible snapshot storage).
#   5. `ray-historyserver` bucket inside MinIO.
#   6. Build + push beta images (eventprocessor, historyserver) to the registry.
#   7. Apply beta/config/ manifests with image refs patched to the local registry.
#   8. Wait for deployments + print port-forward hints.
#
# Idempotent: safe to re-run — each step skips its work if already done.
#
# References:
#   - kind + local registry: https://kind.sigs.k8s.io/docs/user/local-registry/
#   - KubeRay helm chart:    https://github.com/ray-project/kuberay-helm
#   - MinIO minimal deploy:  https://min.io/docs/minio/kubernetes/upstream/

set -euo pipefail

CLUSTER_NAME=${CLUSTER_NAME:-beta-dev}
REG_NAME=${REG_NAME:-kind-registry}
REG_PORT=${REG_PORT:-5001}
MINIO_NAMESPACE=${MINIO_NAMESPACE:-minio-dev}
KUBERAY_NAMESPACE=${KUBERAY_NAMESPACE:-ray-system}
KUBERAY_VERSION=${KUBERAY_VERSION:-1.2.2}
BETA_IMAGE_TAG=${BETA_IMAGE_TAG:-dev}

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
# The `|| true` guards against re-running (already connected is an error).
if ! docker network inspect kind 2>/dev/null | grep -q "\"${REG_NAME}\""; then
  log "Connecting '$REG_NAME' to the 'kind' docker network"
  docker network connect kind "$REG_NAME" 2>/dev/null || true
fi

# Ensure kubectl points at the new cluster.
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# 3. KubeRay operator — installs the RayCluster CRD the beta binaries need.
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
# 4. MinIO — minimal single-pod dev deployment (NOT production).
#    Credentials intentionally hardcoded to `minioadmin:minioadmin` so the
#    dev workflow doesn't require touching Secrets. The beta manifests'
#    `historyserver-beta-s3` Secret is left absent — eventprocessor/
#    historyserver fall back to the ServiceAccount's IAM-style anon auth,
#    which MinIO happily accepts when configured with the anon role. For
#    symmetry we also create the Secret with the MinIO creds.
# ---------------------------------------------------------------------------
kubectl create namespace "$MINIO_NAMESPACE" 2>/dev/null || true

if kubectl -n "$MINIO_NAMESPACE" get deploy minio >/dev/null 2>&1; then
  log "MinIO already deployed in namespace '$MINIO_NAMESPACE'"
else
  log "Deploying MinIO in namespace '$MINIO_NAMESPACE'"
  kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio
  namespace: ${MINIO_NAMESPACE}
  labels:
    app: minio
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: minio
  template:
    metadata:
      labels:
        app: minio
    spec:
      containers:
      - name: minio
        image: minio/minio:latest
        args: ["server", "/data", "--console-address", ":9001"]
        env:
        - name: MINIO_ROOT_USER
          value: "minioadmin"
        - name: MINIO_ROOT_PASSWORD
          value: "minioadmin"
        ports:
        - containerPort: 9000
          name: s3
        - containerPort: 9001
          name: console
        volumeMounts:
        - name: data
          mountPath: /data
        resources:
          requests:
            cpu: "100m"
            memory: "256Mi"
          limits:
            cpu: "500m"
            memory: "1Gi"
      volumes:
      - name: data
        emptyDir:
          sizeLimit: 2Gi
---
apiVersion: v1
kind: Service
metadata:
  name: minio-service
  namespace: ${MINIO_NAMESPACE}
  labels:
    app: minio
spec:
  selector:
    app: minio
  ports:
  - name: s3
    port: 9000
    targetPort: 9000
  - name: console
    port: 9001
    targetPort: 9001
  type: ClusterIP
EOF
  kubectl -n "$MINIO_NAMESPACE" wait --for=condition=Available --timeout=180s \
    deploy/minio
fi

# ---------------------------------------------------------------------------
# 5. Create the `ray-historyserver` bucket using a one-shot mc job.
#    We run mc inside the cluster so we don't need it on the host.
# ---------------------------------------------------------------------------
log "Ensuring 'ray-historyserver' bucket exists in MinIO"
# Newer minio/mc images are distroless and have no `sh`. Use MC_HOST_<alias>
# env var to declare the alias inline, avoiding a shell wrapper.
kubectl -n "$MINIO_NAMESPACE" run mc-init \
  --rm -i --restart=Never \
  --image=minio/mc:latest \
  --env="MC_HOST_local=http://minioadmin:minioadmin@minio-service:9000" \
  --command -- mc mb --ignore-existing local/ray-historyserver \
  || log "bucket create step exited non-zero (likely already existed)"

# ---------------------------------------------------------------------------
# 6. Build + push beta images to the local registry.
#    We drive `make` from the repo root so relative Dockerfile paths resolve.
# ---------------------------------------------------------------------------
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

log "Building + pushing beta images to localhost:${REG_PORT} (tag=${BETA_IMAGE_TAG})"
make -C "$REPO_ROOT" \
  BETA_IMAGE_REGISTRY="localhost:${REG_PORT}" \
  BETA_IMAGE_TAG="${BETA_IMAGE_TAG}" \
  pushbeta

# ---------------------------------------------------------------------------
# 7. Apply the beta manifests. For the two Deployments we sed the image ref
#    so it points at our local registry instead of the placeholder in the
#    committed YAML.
# ---------------------------------------------------------------------------
log "Creating MinIO-backed S3 credentials Secret"
kubectl create secret generic historyserver-beta-s3 \
  --namespace default \
  --from-literal=access_key=minioadmin \
  --from-literal=secret_key=minioadmin \
  --dry-run=client -o yaml | kubectl apply -f -

log "Applying beta/config/ manifests"
kubectl apply -f "$REPO_ROOT/beta/config/rbac.yaml"
kubectl apply -f "$REPO_ROOT/beta/config/configmap-s3.yaml"

# sed -E for portability between GNU and BSD sed (macOS).
sed -E "s|image: historyserver-beta-eventprocessor:v0\\.1\\.0|image: localhost:${REG_PORT}/historyserver-beta-eventprocessor:${BETA_IMAGE_TAG}|g" \
  "$REPO_ROOT/beta/config/eventprocessor.yaml" | kubectl apply -f -

sed -E "s|image: historyserver-beta-historyserver:v0\\.1\\.0|image: localhost:${REG_PORT}/historyserver-beta-historyserver:${BETA_IMAGE_TAG}|g" \
  "$REPO_ROOT/beta/config/historyserver.yaml" | kubectl apply -f -

# ---------------------------------------------------------------------------
# 8. Wait + print access hints.
# ---------------------------------------------------------------------------
log "Waiting for beta deployments to become Available"
kubectl -n default wait --for=condition=Available --timeout=180s deploy/eventprocessor-beta
kubectl -n default wait --for=condition=Available --timeout=180s deploy/historyserver-beta

log "Done. Beta stack is running."
log ""
log "Access the history server UI / API:"
log "  kubectl port-forward svc/historyserver-beta 8080:30080"
log "  open http://localhost:8080"
log ""
log "Scrape processor metrics:"
log "  kubectl port-forward deploy/eventprocessor-beta 9090:9090"
log "  curl http://localhost:9090/metrics"
log ""
log "Tail logs:"
log "  kubectl logs -f deploy/eventprocessor-beta"
log "  kubectl logs -f -l app=historyserver-beta --tail=50"
log ""
log "MinIO console (creds: minioadmin / minioadmin):"
log "  kubectl -n ${MINIO_NAMESPACE} port-forward svc/minio-service 9001:9001"
log "  open http://localhost:9001"
