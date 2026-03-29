#!/bin/bash

# Common setup for nightly Ray image E2E tests.
# Sources setup-env.sh, creates a kind cluster, builds and starts the KubeRay operator.
# After sourcing, the working directory will be in ray-operator/.
#
# Usage: source .buildkite/setup-nightly-test.sh

source .buildkite/setup-env.sh
kind create cluster --wait 900s --config ./ci/kind-config-buildkite.yml
kubectl config set clusters.kind-kind.server https://docker:6443

# Build nightly KubeRay operator image
pushd ray-operator
source ../.buildkite/build-start-operator.sh
kubectl wait --timeout=90s --for=condition=Available=true deployment kuberay-operator
