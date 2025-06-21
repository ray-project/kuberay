#!/bin/bash

# This script is used to start the operator in the buildkite test-e2e steps.

# When starting from the ray ci release automation, we want to install the latest
# released version from helm as actual users might. Ray ci is also always expected
# to kick off from the release branch so tests should match up accordingly.

if [ "$IS_FROM_RAY_RELEASE_AUTOMATION" = 1 ]; then
    helm repo update && helm install kuberay/kuberay-operator
    KUBERAY_TEST_RAY_IMAGE="rayproject/ray:nightly.$(date +'%y%m%d').${RAY_NIGHTLY_COMMIT:0:6}-py39" && export KUBERAY_TEST_RAY_IMAGE
else
    IMG=kuberay/operator:nightly make docker-image &&
    kind load docker-image kuberay/operator:nightly &&
    IMG=kuberay/operator:nightly make deploy
fi
