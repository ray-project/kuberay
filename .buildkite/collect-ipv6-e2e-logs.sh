#!/bin/bash

# Best-effort diagnostics for the IPv6 and dual-stack E2E steps. This script is
# called only after a test failure, so every collection command is allowed to
# fail without hiding the original go test exit status.
set +e

artifact_name="${1:-e2e-ipv6-log.tar}"
output_dir="${KUBERAY_TEST_OUTPUT_DIR:-$(pwd)/tmp}"
mkdir -p "${output_dir}"

kubectl logs -A -l app.kubernetes.io/name=kuberay --all-containers=true --prefix=true --max-log-requests=50 \
  >"${output_dir}/kuberay-operator.log" 2>&1
kubectl logs -A -l ray.io/cluster --all-containers=true --prefix=true --max-log-requests=50 \
  >"${output_dir}/ray-head-worker-pods.log" 2>&1
kubectl get pods -A -o wide >"${output_dir}/pods.txt" 2>&1
kubectl get events -A --sort-by=.lastTimestamp >"${output_dir}/events.txt" 2>&1
kind export logs "${output_dir}/kind-logs" >/dev/null 2>&1

tar -cf "/artifact-mount/${artifact_name}" -C "${output_dir}" .
exit 0
