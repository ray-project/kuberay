#!/bin/bash

# This shell is used to auto generate some useful tools for k8s, such as clientset, lister, informer and so on.
# We don't use this tool to generate deepcopy because kubebuilder (controller-tools) has coverred that part.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
ROOT_PKG=github.com/ray-project/kuberay/ray-operator
CODEGEN_PKG=$(go list -m -f "{{.Dir}}" k8s.io/code-generator)

if [[ ! -d ${CODEGEN_PKG} ]]; then
    echo "${CODEGEN_PKG} is missing. Running 'go mod download'."
    go mod download
    CODEGEN_PKG=$(go list -m -f "{{.Dir}}" k8s.io/code-generator)
fi

echo ">> Using ${CODEGEN_PKG}"

# Ensure we can execute.
chmod +x "${CODEGEN_PKG}"/generate-groups.sh
chmod +x "${CODEGEN_PKG}"/generate-internal-groups.sh

cd "${SCRIPT_ROOT}"

# Migrate to using kube_codegen.sh once the following issue is fixed:
# https://github.com/kubernetes/code-generator/issues/165
"${CODEGEN_PKG}"/generate-groups.sh "client,informer,lister" \
 github.com/ray-project/kuberay/ray-operator/pkg/client github.com/ray-project/kuberay/ray-operator/apis \
 ray:v1 \
 --output-base "${SCRIPT_ROOT}" \
 --trim-path-prefix ${ROOT_PKG} \
 --go-header-file hack/boilerplate.go.txt
