BUILD_TIME      := $(shell date "+%F %T")
COMMIT_SHA1     := $(shell git rev-parse HEAD )
REPO_ROOT_BIN	:= $(shell dirname ${PWD})/bin

# Image URL to use all building/pushing image targets
IMG_TAG ?=latest
IMG ?= kuberay/apiserver:$(IMG_TAG)

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
ENGINE ?= docker

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
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

fumpt: gofumpt ## Run gofmtumpt against code.
	$(GOFUMPT) -l -w .

imports: goimports ## Run goimports against code.
	$(GOIMPORTS) -l -w .	

test: fmt vet fumpt imports lint  ## Run unit tests.
	go test ./... -race -coverprofile ray-kube-api-server-coverage.out

lint: golangci-lint fmt vet fumpt imports ## Run the linter.
	$(GOLANGCI_LINT) run  --timeout=3m	

##@ Build

COMMIT := $(shell git rev-parse --short HEAD)
VERSION := $(shell git describe --tags $(shell git rev-list --tags --max-count=1))
DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
REPO=github.com/ray-project/kuberay

BUILD_FLAGS ?= -ldflags="-X '${REPO}/apiserver/cmd/version.Version=$(VERSION)' \
	-X '${REPO}/apiserver/cmd/version.gitCommit=$(COMMIT)' \
	-X '${REPO}/apiserver/cmd/version.buildDate=$(DATE)'"

build: fmt vet fumpt imports lint ## Build api server binary.
	go build $(BUILD_FLAGS) -o ${REPO_ROOT_BIN}/kuberay-apiserver cmd/main.go

run: fmt vet fumpt imports lint ## Run the api server from your host.
	go run -race cmd/main.go

docker-image: test ## Build image with the api server.
	${ENGINE} build -t ${IMG} -f Dockerfile ..

docker-push: ## Push image with the api server.
	${ENGINE} push ${IMG}

##@ Deployment
.PHONY: install
install: kustomize ## Install the kuberay api server to the K8s cluster specified in ~/.kube/config.
	cd deploy/local && $(KUSTOMIZE) edit set image kuberay/apiserver=$(IMG)
	$(KUSTOMIZE) build deploy/local | kubectl create -f -

.PHONY: uninstall
uninstall: ## Install the kuberay api server to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build deploy/local | kubectl delete -f -

.PHONY: deploy
deploy: ## Deploy via helm the kuberay api server to the K8s cluster specified in ~/.kube/config. 
# Note that you should make your KubeRay APIServer image available by either pushing it to an image registry, such as DockerHub or Quay, or by loading the image into the Kubernetes cluster. 
# If you are using a Kind cluster for development, you can run `make load-image` to load the newly built image into the Kind cluster.
	helm upgrade --install kuberay-apiserver ../helm-chart/kuberay-apiserver --wait \
	--set image.tag=${IMG_TAG} --set image.pullPolicy=IfNotPresent

.PHONY: undeploy
undeploy: ## Undeploy via helm the kuberay api server to the K8s cluster specified in ~/.kube/config.
	helm uninstall kuberay-apiserver --wait

##@ Development Tools

## Location to install dependencies to
$(REPO_ROOT_BIN):
	mkdir -p $(REPO_ROOT_BIN)

## Tool Binaries
KUSTOMIZE ?= $(REPO_ROOT_BIN)/kustomize
GOIMPORTS ?= $(REPO_ROOT_BIN)/goimports
GOFUMPT ?= $(REPO_ROOT_BIN)/gofumpt
GOLANGCI_LINT ?= $(REPO_ROOT_BIN)/golangci-lint
KIND ?= $(REPO_ROOT_BIN)/kind


## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
GOFUMPT_VERSION ?= v0.3.1
GOIMPORTS_VERSION ?= latest
GOLANGCI_LINT_VERSION ?= v1.50.1
KIND_VERSION ?= v0.19.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(REPO_ROOT_BIN)
	test -s $(KUSTOMIZE) || (curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(REPO_ROOT_BIN);)

.PHONY: goimports
goimports: $(GOIMPORTS) ## Download goimports locally if necessary
$(GOIMPORTS): $(REPO_ROOT_BIN)
	test -s $(GOIMPORTS) || GOBIN=$(REPO_ROOT_BIN) go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	
.PHONY: gofumpt
gofumpt: $(GOFUMPT) ## Download gofumpt locally if necessary.
$(GOFUMPT): $(REPO_ROOT_BIN)	
	test -s $(GOFUMPT) || GOBIN=$(REPO_ROOT_BIN) go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci_lint locally if necessary.
$(GOLANGCI_LINT): $(REPO_ROOT_BIN)
	test -s $(GOLANGCI_LINT) || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | bash -s -- -b $(REPO_ROOT_BIN)/ $(GOLANGCI_LINT_VERSION))

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(REPO_ROOT_BIN)	
	test -s $(KIND) || GOBIN=$(REPO_ROOT_BIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: dev-tools
dev-tools: kind golangci-lint gofumpt kustomize goimports ## Install all development tools

.PHONY: clean-dev-tools
clean-dev-tools: ## Remove all development tools
	rm -f $(REPO_ROOT_BIN)/golangci-lint
	rm -f $(REPO_ROOT_BIN)/gofumpt
	rm -f $(REPO_ROOT_BIN)/kustomize
	rm -f $(REPO_ROOT_BIN)/goimports
	rm -f $(REPO_ROOT_BIN)/kind


##@ Testing Setup and Tools
KIND_CONFIG ?= hack/kind-cluster-config.yaml
KIND_CLUSTER_NAME ?= ray-api-server-cluster
OPERATOR_IMAGE_TAG ?= latest
.PHONY: cluster
cluster: kind ## Start kind development cluster.
	$(KIND) create cluster -n $(KIND_CLUSTER_NAME) --config $(KIND_CONFIG)

.PHONY: clean-cluster
clean-cluster: kind ## Delete kind development cluster.
	$(KIND) delete cluster -n $(KIND_CLUSTER_NAME)

.PHONY: load-image
load-image: ## Load the api server image to the kind cluster created with create-kind-cluster.
	$(KIND) load docker-image $(IMG) -n $(KIND_CLUSTER_NAME)

.PHONY: operator-image
operator-image: ## Build the operator image to be loaded in your kind cluster.
	cd ../ray-operator && $(MAKE) docker-image -e IMG=kuberay/operator:$(OPERATOR_IMAGE_TAG)

.PHONY: deploy-operator
deploy-operator: ## Deploy operator via helm into the K8s cluster specified in ~/.kube/config. 
# Note that you should make your operatorimage available by either pushing it to an image registry, such as DockerHub or Quay, or by loading the image into the Kubernetes cluster. 
# If you are using a Kind cluster for development, you can run `make load-image` to load the newly built image into the Kind cluster.
	helm upgrade --install raycluster ../helm-chart/kuberay-operator --wait \
	--set image.tag=${OPERATOR_IMAGE_TAG} --set image.pullPolicy=IfNotPresent

.PHONY: undeploy-operator
undeploy-operator: ## Undeploy operator via helm from the K8s cluster specified in ~/.kube/config.
	helm uninstall raycluster --wait

.PHONY: load-operator-image
load-operator-image: ## Load the operator image to the kind cluster created with create-kind-cluster.
	$(KIND) load docker-image kuberay/operator:$(OPERATOR_IMAGE_TAG) -n $(KIND_CLUSTER_NAME)
