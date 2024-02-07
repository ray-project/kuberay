#!/bin/bash

# This shell is used to auto generate some useful tools for k8s, such as clientset, lister, informer and so on.
# We don't use this tool to generate deepcopy because kubebuilder (controller-tools) has coverred that part.

set -o errexit
set -o nounset
set -o pipefail

export GOPATH=$(go env GOPATH)

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
ROOT_PKG=github.com/ray-project/kuberay/ray-operator
CODEGEN_PKG=$(go list -m -f "{{.Dir}}" k8s.io/code-generator)

if [[ ! -d ${CODEGEN_PKG} ]]; then
    echo "${CODEGEN_PKG} is missing. Running 'go mod download'."
    go mod download
    CODEGEN_PKG=$(go list -m -f "{{.Dir}}" k8s.io/code-generator)
fi

echo ">> Using ${CODEGEN_PKG}"

cd "${SCRIPT_ROOT}"

# Migrate to using kube_codegen.sh once the following issue is fixed:
# https://github.com/kubernetes/code-generator/issues/165

go install "${CODEGEN_PKG}"/cmd/applyconfiguration-gen
go install "${CODEGEN_PKG}"/cmd/client-gen
go install "${CODEGEN_PKG}"/cmd/lister-gen
go install "${CODEGEN_PKG}"/cmd/informer-gen

"${GOPATH}"/bin/applyconfiguration-gen \
  --input-dirs github.com/ray-project/kuberay/ray-operator/apis/ray/v1 \
  --external-applyconfigurations k8s.io/api/core/v1.PodTemplateSpec:k8s.io/client-go/applyconfigurations/core/v1 \
  --output-package github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration \
  --go-header-file hack/boilerplate.go.txt \
  --output-base "${SCRIPT_ROOT}" \
  --trim-path-prefix ${ROOT_PKG}

"${GOPATH}"/bin/client-gen \
  --input github.com/ray-project/kuberay/ray-operator/apis/ray/v1 \
  --input-base="" \
  --apply-configuration-package=github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration \
  --clientset-name "versioned"  \
  --output-package github.com/ray-project/kuberay/ray-operator/pkg/client/clientset \
  --go-header-file hack/boilerplate.go.txt \
  --output-base "${SCRIPT_ROOT}" \
  --trim-path-prefix ${ROOT_PKG}

"${GOPATH}"/bin/lister-gen \
  --input-dirs github.com/ray-project/kuberay/ray-operator/apis/ray/v1 \
  --output-package github.com/ray-project/kuberay/ray-operator/pkg/client/listers \
  --go-header-file hack/boilerplate.go.txt \
  --output-base "${SCRIPT_ROOT}" \
  --trim-path-prefix ${ROOT_PKG}

"${GOPATH}"/bin/informer-gen \
  --input-dirs github.com/ray-project/kuberay/ray-operator/apis/ray/v1 \
  --versioned-clientset-package github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned \
  --listers-package github.com/ray-project/kuberay/ray-operator/pkg/client/listers \
  --output-package github.com/ray-project/kuberay/ray-operator/pkg/client/informers \
  --go-header-file hack/boilerplate.go.txt \
  --output-base "${SCRIPT_ROOT}" \
  --trim-path-prefix ${ROOT_PKG}
