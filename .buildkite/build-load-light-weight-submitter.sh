#!/bin/bash

# This script is used to start the operator in the buildkite test-e2e steps.

# When starting from the ray ci release automation, we want to install the latest
# released version from helm as actual users might. Ray ci is also always expected
# to kick off from the release branch so tests should match up accordingly.
IMG=kuberay/submitter:nightly make docker-image-rayjobsubmitter &&
    kind load docker-image kuberay/submitter:nightly
