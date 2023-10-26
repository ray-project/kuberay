BUILD_TIME      := $(shell date "+%F %T")
COMMIT_SHA1     := $(shell git rev-parse HEAD )

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
# Golang forbids converting float from Json to struct. Need allowDangerousTypes=true to unblock build.
CRD_OPTIONS ?= "crd:maxDescLen=0,trivialVersions=true,preserveUnknownFields=false,generateEmbeddedObjectMeta=true,allowDangerousTypes=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Container Engine to be used for building images
ENGINE ?= "docker"

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=kuberay-operator webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

helm: manifests ## Sync the CRDs into the Helm chart
	rm -r ../helm-chart/kuberay-operator/crds/
	cp -r config/crd/bases/ ../helm-chart/kuberay-operator/crds/

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

fumpt: gofumpt
	$(GOFUMPT) -l -w ../

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: WHAT ?= $(shell go list ./... | grep -v /test/)
test: manifests generate fmt vet ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
	export ENVTEST_K8S_VERSION=1.24.2; source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test $(WHAT) -v -coverprofile cover.out

test-e2e: WHAT ?= ./test/e2e
test-e2e: manifests generate fmt vet ## Run e2e tests.
	go test -timeout 30m -v $(WHAT)

sync: helm
	./hack/update-codegen.sh
##@ Build

build: generate fmt vet ## Build manager binary.
	go build                                    \
    -ldflags                                    \
    "                                           \
    -X 'main._buildTime_=${BUILD_TIME}'         \
    -X 'main._commitId_=${COMMIT_SHA1}'         \
    "                                           \
    -o bin/manager main.go

# run: manifests generate fmt vet
# Running the controller from your host may fail to reconcile resources. For example, when creating a RayService
# KubeRay sends HTTP requests to the Ray head using ${HEAD_SVC_FQDN}:52365 which is not reachable from outside of a K8s cluster.
# For detail, see https://docs.ray.io/en/master/cluster/kubernetes/troubleshooting/rayservice-troubleshooting.html#issue-5-fail-to-create-update-serve-applications
# 	go run ./main.go -enable-leader-election=false

docker-image: ## Build image only
	${ENGINE} build -t ${IMG} .

docker-build: build docker-image ## Build image with the manager.

docker-push: ## Push image with the manager.
	${ENGINE} push ${IMG}

docker-multi-arch-image: # Build and push multi arch image, amd64 and arm64 currently
	${ENGINE} buildx build --push --platform linux/amd64,linux/arm64/v8 -t ${IMG} .

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	($(KUSTOMIZE) build config/crd | kubectl create -f -) || ($(KUSTOMIZE) build config/crd | kubectl replace -f -)

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/default && $(KUSTOMIZE) edit set image kuberay/operator=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply --server-side=true -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	test -s $(CONTROLLER_GEN) || GOBIN=$(CONTROLLER_GEN)/.. go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.0

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	test -s $(KUSTOMIZE) || GOBIN=$(KUSTOMIZE)/.. go install sigs.k8s.io/kustomize/kustomize/v5@latest

GOFUMPT = $(shell pwd)/bin/gofumpt
gofumpt: ## Download gofumpt locally if necessary.
	test -s $(GOFUMPT) || GOBIN=$(GOFUMPT)/.. go install mvdan.cc/gofumpt@latest

