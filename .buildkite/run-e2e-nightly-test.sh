#!/bin/bash

# Shared script to run nightly E2E tests with consistent logging, output capture,
# and artifact collection. Expects to be run from the ray-operator/ directory.
#
# Usage: bash ../.buildkite/run-e2e-nightly-test.sh <test-name> <go-test-timeout> <test-dir> <artifact-tar>
#
# Arguments:
#   test-name       Human-readable test name for log labels
#   go-test-timeout Timeout for "go test -timeout" (e.g., 30m, 60m)
#   test-dir        Test directory or files to pass to "go test -v" (e.g., ./test/e2e)
#   artifact-tar    Tar file name placed in /artifact-mount/ on failure

set -o pipefail

TEST_NAME="$1"
GO_TEST_TIMEOUT="$2"
TEST_DIR="$3"
ARTIFACT_TAR="$4"

echo "--- START:Running ${TEST_NAME} tests"
echo "Using Ray Image ${KUBERAY_TEST_RAY_IMAGE}"

mkdir -p "$(pwd)/tmp"
export KUBERAY_TEST_OUTPUT_DIR="$(pwd)/tmp"
echo "KUBERAY_TEST_OUTPUT_DIR=${KUBERAY_TEST_OUTPUT_DIR}"

KUBERAY_TEST_TIMEOUT_SHORT=1m KUBERAY_TEST_TIMEOUT_MEDIUM=5m KUBERAY_TEST_TIMEOUT_LONG=10m \
  go test -timeout "${GO_TEST_TIMEOUT}" -v ${TEST_DIR} 2>&1 \
  | awk -f ../.buildkite/format.awk \
  | tee "${KUBERAY_TEST_OUTPUT_DIR}/gotest.log" \
  || (kubectl logs --tail -1 -l app.kubernetes.io/name=kuberay \
      | tee "${KUBERAY_TEST_OUTPUT_DIR}/kuberay-operator.log" \
    && cd "${KUBERAY_TEST_OUTPUT_DIR}" \
    && find . -name "*.log" | tar -cf "/artifact-mount/${ARTIFACT_TAR}" -T - \
    && exit 1)

echo "--- END:${TEST_NAME} tests finished"
