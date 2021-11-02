#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

bash $SCRIPT_ROOT/vendor/k8s.io/code-generator/generate-groups.sh all "github.com/kuberay/ray-operator/pkg/client" \
      "github.com/kuberay/ray-operator/api" \
      raycluster:v1alpha1 --go-header-file "${SCRIPT_ROOT}/hack/boilerplate.go.txt"
