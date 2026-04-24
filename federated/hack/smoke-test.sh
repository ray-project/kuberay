#!/usr/bin/env bash
# Smoke test: verify cross-cluster Pod-to-Pod connectivity via Cilium ClusterMesh.
#
# Deploys echo pods in each cluster, then curls across clusters to verify
# that Pods can reach each other directly by Pod IP.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFESTS_DIR="${SCRIPT_DIR}/../infra/manifests"

PRIMARY_CONTEXT="kind-frc-primary"
MEMBER_A_CONTEXT="kind-frc-member-a"
MEMBER_B_CONTEXT="kind-frc-member-b"

cleanup() {
  echo ""
  echo "=== Cleaning up echo pods ==="
  kubectl --context "${PRIMARY_CONTEXT}"  delete pod echo-primary  --ignore-not-found 2>/dev/null || true
  kubectl --context "${MEMBER_A_CONTEXT}" delete pod echo-member-a --ignore-not-found 2>/dev/null || true
  kubectl --context "${MEMBER_B_CONTEXT}" delete pod echo-member-b --ignore-not-found 2>/dev/null || true
}
trap cleanup EXIT

echo "=== Deploying echo pods ==="
kubectl --context "${PRIMARY_CONTEXT}"  apply -f "${MANIFESTS_DIR}/smoke-primary.yaml"
kubectl --context "${MEMBER_A_CONTEXT}" apply -f "${MANIFESTS_DIR}/smoke-member-a.yaml"
kubectl --context "${MEMBER_B_CONTEXT}" apply -f "${MANIFESTS_DIR}/smoke-member-b.yaml"

echo "=== Waiting for pods to be ready ==="
kubectl --context "${PRIMARY_CONTEXT}"  wait --for=condition=Ready pod/echo-primary  --timeout=120s
kubectl --context "${MEMBER_A_CONTEXT}" wait --for=condition=Ready pod/echo-member-a --timeout=120s
kubectl --context "${MEMBER_B_CONTEXT}" wait --for=condition=Ready pod/echo-member-b --timeout=120s

echo "=== Getting Pod IPs ==="
PRIMARY_IP=$(kubectl --context "${PRIMARY_CONTEXT}"  get pod echo-primary  -o jsonpath='{.status.podIP}')
MEMBER_A_IP=$(kubectl --context "${MEMBER_A_CONTEXT}" get pod echo-member-a -o jsonpath='{.status.podIP}')
MEMBER_B_IP=$(kubectl --context "${MEMBER_B_CONTEXT}" get pod echo-member-b -o jsonpath='{.status.podIP}')

echo "  echo-primary  (frc-primary):  ${PRIMARY_IP}"
echo "  echo-member-a (frc-member-a): ${MEMBER_A_IP}"
echo "  echo-member-b (frc-member-b): ${MEMBER_B_IP}"

PASS=0
FAIL=0
TEST_NUM=0

cross_curl() {
  local from_ctx="$1" from_name="$2" target_ip="$3" target_name="$4"
  TEST_NUM=$((TEST_NUM + 1))
  local pod_name="curl-test-${$}-${TEST_NUM}"
  echo -n "  ${from_name} -> ${target_name} (${target_ip}:5678): "

  local attempt result
  for attempt in 1 2 3; do
    kubectl --context "${from_ctx}" delete pod "${pod_name}" --ignore-not-found 2>/dev/null || true

    result=$(kubectl --context "${from_ctx}" run "${pod_name}" \
      --rm -i --restart=Never --image=curlimages/curl \
      -- curl -s --connect-timeout 10 "http://${target_ip}:5678" 2>/dev/null \
      | tr -d '\r' | grep "hello-from-") || result=""

    if [ -n "${result}" ]; then
      echo "OK (${result})"
      PASS=$((PASS + 1))
      return
    fi

    if [ "${attempt}" -lt 3 ]; then
      sleep 2
    fi
  done

  echo "FAILED (after ${attempt} attempts)"
  FAIL=$((FAIL + 1))
}

echo ""
echo "=== Testing cross-cluster Pod-to-Pod connectivity ==="

# primary -> members
cross_curl "${PRIMARY_CONTEXT}" "frc-primary" "${MEMBER_A_IP}" "frc-member-a"
cross_curl "${PRIMARY_CONTEXT}" "frc-primary" "${MEMBER_B_IP}" "frc-member-b"

# member-a -> primary and member-b
cross_curl "${MEMBER_A_CONTEXT}" "frc-member-a" "${PRIMARY_IP}" "frc-primary"
cross_curl "${MEMBER_A_CONTEXT}" "frc-member-a" "${MEMBER_B_IP}" "frc-member-b"

# member-b -> primary and member-a
cross_curl "${MEMBER_B_CONTEXT}" "frc-member-b" "${PRIMARY_IP}" "frc-primary"
cross_curl "${MEMBER_B_CONTEXT}" "frc-member-b" "${MEMBER_A_IP}" "frc-member-a"

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

# Cleanup is handled by the EXIT trap above.

if [ "${FAIL}" -gt 0 ]; then
  echo ""
  echo "SMOKE TEST FAILED: ${FAIL} connectivity check(s) failed."
  exit 1
fi

echo ""
echo "SMOKE TEST PASSED: All cross-cluster connectivity checks succeeded."
