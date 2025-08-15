#!/bin/bash

# This script is used to start the operator in the buildkite test-e2e steps.

# When starting from the ray ci release automation, we want to install the latest
# released version from helm as actual users might. Ray ci is also always expected
# to kick off from the release branch so tests should match up accordingly.

if [ "$IS_FROM_RAY_RELEASE_AUTOMATION" = 1 ]; then
    echo "Skipping light weight submitter build for ray release automation for now"
else
    IMG=kuberay/submitter:nightly make docker-image &&
    kind load docker-image kuberay/submitter:nightly
fi
