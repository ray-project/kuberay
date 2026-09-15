#!/usr/bin/env bash
# Deploy a cross-cluster Ray cluster: head on frc-primary, one worker on each member cluster.
#
# Prerequisites:
#   - 3 kind clusters running with Cilium ClusterMesh (run bootstrap-federated-ray-lab.sh first)
#   - rayproject/ray:2.52.0 image available (pulled automatically if not cached)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFESTS_DIR="${SCRIPT_DIR}/../infra/manifests"

PRIMARY_CONTEXT="kind-frc-primary"
MEMBER_A_CONTEXT="kind-frc-member-a"
MEMBER_B_CONTEXT="kind-frc-member-b"

RAY_IMAGE="rayproject/ray:2.52.0"

cleanup_ray() {
  echo ""
  echo "=== Cleaning up Ray pods ==="
  kubectl --context "${PRIMARY_CONTEXT}"  delete pod ray-head   --ignore-not-found 2>/dev/null || true
  kubectl --context "${MEMBER_A_CONTEXT}" delete pod ray-worker --ignore-not-found 2>/dev/null || true
  kubectl --context "${MEMBER_B_CONTEXT}" delete pod ray-worker --ignore-not-found 2>/dev/null || true
}

# Handle --cleanup flag
if [ "${1:-}" = "--cleanup" ]; then
  cleanup_ray
  exit 0
fi

# ── Step 1: Pull and load Ray image ─────────────────────────────────────────
echo "[1/4] Preparing Ray image..."
if docker image inspect "${RAY_IMAGE}" >/dev/null 2>&1; then
  echo "  Already cached: ${RAY_IMAGE}"
else
  echo "  Pulling: ${RAY_IMAGE} (~1.5 GB, may take a few minutes)..."
  docker pull "${RAY_IMAGE}"
fi

for cluster in frc-primary frc-member-a frc-member-b; do
  echo "  Loading into ${cluster}..."
  if ! kind load docker-image "${RAY_IMAGE}" --name="${cluster}" >/dev/null 2>&1; then
    echo "    WARNING: failed to load ${RAY_IMAGE} into ${cluster}"
  fi
done

# ── Step 2: Deploy Ray head on primary cluster ──────────────────────────────
echo "[2/4] Deploying Ray head on frc-primary..."
kubectl --context "${PRIMARY_CONTEXT}" delete pod ray-head --ignore-not-found 2>/dev/null || true
kubectl --context "${PRIMARY_CONTEXT}" apply -f "${MANIFESTS_DIR}/ray-head.yaml"

echo "  Waiting for head pod to be ready..."
kubectl --context "${PRIMARY_CONTEXT}" wait --for=condition=Ready pod/ray-head --timeout=300s

echo "  Waiting for GCS to accept connections..."
HEAD_IP=$(kubectl --context "${PRIMARY_CONTEXT}" get pod ray-head -o jsonpath='{.status.podIP}')
for i in $(seq 1 30); do
  if kubectl --context "${PRIMARY_CONTEXT}" exec ray-head -- ray health-check --address "${HEAD_IP}:6379" >/dev/null 2>&1; then
    echo "  GCS is ready."
    break
  fi
  sleep 2
done

# ── Step 3: Get head Pod IP and deploy workers ──────────────────────────────
echo "[3/4] Head Pod IP: ${HEAD_IP}"
echo "  Deploying workers with RAY_HEAD_ADDRESS=${HEAD_IP}:6379..."

# Deploy worker on member-a
kubectl --context "${MEMBER_A_CONTEXT}" delete pod ray-worker --ignore-not-found 2>/dev/null || true
sed "s|PLACEHOLDER:6379|${HEAD_IP}:6379|g" "${MANIFESTS_DIR}/ray-worker.yaml" \
  | kubectl --context "${MEMBER_A_CONTEXT}" apply -f -

# Deploy worker on member-b
kubectl --context "${MEMBER_B_CONTEXT}" delete pod ray-worker --ignore-not-found 2>/dev/null || true
sed "s|PLACEHOLDER:6379|${HEAD_IP}:6379|g" "${MANIFESTS_DIR}/ray-worker.yaml" \
  | kubectl --context "${MEMBER_B_CONTEXT}" apply -f -

echo "  Waiting for workers to be ready..."
kubectl --context "${MEMBER_A_CONTEXT}" wait --for=condition=Ready pod/ray-worker --timeout=300s
kubectl --context "${MEMBER_B_CONTEXT}" wait --for=condition=Ready pod/ray-worker --timeout=300s

# ── Step 4: Verify Ray cluster formed ───────────────────────────────────────
echo "[4/4] Verifying Ray cluster..."
sleep 10

echo ""
HEAD_IP=$(kubectl --context "${PRIMARY_CONTEXT}" get pod ray-head -o jsonpath='{.status.podIP}')
echo "=== Ray cluster status ==="
kubectl --context "${PRIMARY_CONTEXT}" exec ray-head -- ray status --address "${HEAD_IP}:6379"
echo ""

echo "=== Node IPs (via dashboard API) ==="
kubectl --context "${PRIMARY_CONTEXT}" exec ray-head -- python3 -c "
import urllib.request, json
data = json.loads(urllib.request.urlopen('http://localhost:8265/nodes?view=summary').read())
nodes = data.get('data', {}).get('summary', [])
for n in sorted(nodes, key=lambda x: x.get('ip', '')):
    ip = n.get('ip', '?')
    hostname = n.get('hostname', '?')
    print(f'  {ip}  hostname={hostname}')
print()
ips = [n['ip'] for n in nodes if 'ip' in n]
subnets = set(ip.rsplit('.', 2)[0] for ip in ips)
print(f'Nodes: {len(nodes)}, Unique subnets: {len(subnets)} (expect 3 for cross-cluster)')
"
echo ""

echo "Done! Ray cluster is running across 3 Kubernetes clusters."
echo ""
echo "Useful commands:"
echo "  kubectl --context ${PRIMARY_CONTEXT} exec ray-head -- ray status --address ${HEAD_IP}:6379"
echo "  kubectl --context ${PRIMARY_CONTEXT} port-forward ray-head 8265:8265  # Ray Dashboard at http://localhost:8265"
echo "  ./hack/deploy-ray-cluster.sh --cleanup  # Tear down Ray pods"
