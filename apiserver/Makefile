REPO_ROOT       := $(shell dirname ${PWD})
REPO_ROOT_BIN   := $(REPO_ROOT)/bin

# Image URL to use all building/pushing image targets
IMG_REPO        ?= quay.io/kuberay/apiserver
IMG_TAG         ?= latest
IMG             := $(IMG_REPO):$(IMG_TAG)
# Allow for additional test flags (-v, etc)
GO_TEST_FLAGS   ?=
# Container Engine to be used for building images
ENGINE          ?= docker
# Kuberay API Server base URL to use in end to end tests
E2E_API_SERVER_URL ?= http://localhost:31888
## Tool Versions
KUSTOMIZE_VERSION       ?= v5.4.3
GOFUMPT_VERSION         ?= v0.3.1
GOIMPORTS_VERSION       ?= v0.14.0
GOLANGCI_LINT_VERSION   ?= v1.64.8
KIND_VERSION            ?= v0.19.0
GOBINDATA_VERSION       ?= v4.0.2
MOCKGEN_VERSION         ?= v1.6.0
## Tool Binaries
KUSTOMIZE     ?= $(REPO_ROOT_BIN)/kustomize
GOIMPORTS     ?= $(REPO_ROOT_BIN)/goimports
GOFUMPT       ?= $(REPO_ROOT_BIN)/gofumpt
GOLANGCI_LINT ?= $(REPO_ROOT_BIN)/golangci-lint
KIND          ?= $(REPO_ROOT_BIN)/kind
GOBINDATA     ?= $(REPO_ROOT_BIN)/go-bindata
MOCKGEN       ?= $(REPO_ROOT_BIN)/mockgen
# Ray docker images to use for end to end tests based upon the architecture
# for arm64 environments (Apple silicon included) pull the architecture specific image
ifeq (arm64, $(shell go env GOARCH))
	E2E_API_SERVER_RAY_IMAGE ?= rayproject/ray:2.9.0-py310-aarch64
else
	E2E_API_SERVER_RAY_IMAGE ?= rayproject/ray:2.9.0-py310
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (, $(shell go env GOBIN))
	GOBIN = $(shell go env GOPATH)/bin
else
	GOBIN = $(shell go env GOBIN)
endif
# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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

##@ Deployment

.PHONY: start-local-apiserver
start-local-apiserver: operator-image cluster load-operator-image deploy-operator install run ## Build and start apiserver from scratch.

.PHONY: clear-local-apiserver
clear-local-apiserver: clean-cluster ## Clear local apiserver.

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	@echo "Running go fmt..."
	@go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	@echo "Running go vet..."
	@go vet ./...

.PHONY: fumpt
fumpt: gofumpt ## Run gofumpt against code.
	@echo "Running gofumpt..."
	@$(GOFUMPT) -l -w .

.PHONY: imports
imports: goimports ## Run goimports against code.
	@echo "Running goimports..."
	@$(GOIMPORTS) -l -w .

.PHONY: format
format: fmt vet fumpt imports ## Run all formatting tools

.PHONY: lint
lint: format golangci-lint ## Run the linter.
	@echo "Running golangci-lint..."
	# exclude the SA1019 check which checks the usage of deprecated fields.
	@$(GOLANGCI_LINT) run --timeout=3m --exclude='SA1019' --no-config --allow-parallel-runners

build: format ## Build api server binary.
	go build -o ${REPO_ROOT_BIN}/kuberay-apiserver cmd/main.go

run: lint ## Run the api server from your host.
	go run -race cmd/main.go -localSwaggerPath ${REPO_ROOT}/proto/swagger

.PHONY: build-swagger
build-swagger: go-bindata
	cd $(REPO_ROOT) && $(GOBINDATA) --nocompress --pkg swagger -o apiserver/pkg/swagger/datafile.go third_party/swagger-ui/...

.PHONY: generate
generate: mockgen # Generate code using command after //go:generate in each file
	# temporarily prepend local bin/ directory to the system $PATH to run mockgen
	PATH=$(REPO_ROOT_BIN):$$PATH go generate ./...

##@ Testing

.PHONY: test
test: format generate ## Run all unit tests.
	go test ./pkg/... $(GO_TEST_FLAGS) -race -coverprofile ray-kube-api-server-coverage.out -parallel 4

.PHONY: e2e-test
e2e-test: ## Run end to end tests using a pre-exiting cluster.
	go test ./test/e2e/... $(GO_TEST_FLAGS) -timeout 60m -race -coverprofile ray-kube-api-server-e2e-coverage.out -count=1 -parallel 4

.PHONY: local-e2e-test ## Run end to end tests on newly created cluster.
local-e2e-test: operator-image cluster load-operator-image deploy-operator install load-ray-test-image e2e-test clean-cluster ## Run end to end tests, create a fresh kind cluster will all components deployed.

##@ Testing Setup
KIND_CONFIG ?= hack/kind-cluster-config.yaml
KIND_CLUSTER_NAME ?= ray-api-server-cluster
OPERATOR_IMAGE_TAG ?= latest
SECURITY_IMAGE_TAG ?= latest
.PHONY: cluster
cluster: kind ## Start kind development cluster.
	@if kind get clusters | grep -q "^$(KIND_CLUSTER_NAME)$$"; then \
		echo "Kind cluster $(KIND_CLUSTER_NAME) already exists, please run 'make clean-cluster' to delete the cluster."; \
	else \
		$(KIND) create cluster -n $(KIND_CLUSTER_NAME) --config $(KIND_CONFIG); \
	fi

.PHONY: clean-cluster
clean-cluster: kind ## Delete kind development cluster.
	$(KIND) delete cluster -n $(KIND_CLUSTER_NAME)

.PHONY: load-image
load-image: ## Load the api server image to the kind cluster created with create-kind-cluster.
	$(KIND) load docker-image $(IMG) -n $(KIND_CLUSTER_NAME)

.PHONY: operator-image
operator-image: ## Build the operator image to be loaded in your kind cluster.
	cd ../ray-operator && $(MAKE) docker-image -e IMG=quay.io/kuberay/operator:$(OPERATOR_IMAGE_TAG)

.PHONY: security-proxy-image
security-proxy-image: ## Build the security proxy image to be loaded in your kind cluster.
	cd ../experimental && $(MAKE) docker-image -e IMG=quay.io/kuberay/security-proxy:$(SECURITY_IMAGE_TAG)

.PHONY: deploy-operator
deploy-operator: ## Deploy operator via helm into the K8s cluster specified in ~/.kube/config.
# Note that you should make your operator image available by either pushing it to an image registry, such as DockerHub or Quay, or by loading the image into the Kubernetes cluster.
# If you are using a Kind cluster for development, you can run `make load-operator-image` to load the newly built image into the Kind cluster.
	helm upgrade --install raycluster ../helm-chart/kuberay-operator --wait \
	--set image.tag=${OPERATOR_IMAGE_TAG} --set image.pullPolicy=IfNotPresent

.PHONY: undeploy-operator
undeploy-operator: ## Undeploy operator via helm from the K8s cluster specified in ~/.kube/config.
	helm uninstall raycluster --wait

.PHONY: load-operator-image
load-operator-image: ## Load the operator image to the kind cluster created with make cluster.
ifneq ($(OPERATOR_IMAGE_TAG), latest)
	$(ENGINE) pull quay.io/kuberay/operator:$(OPERATOR_IMAGE_TAG)
endif
	$(KIND) load docker-image quay.io/kuberay/operator:$(OPERATOR_IMAGE_TAG) -n $(KIND_CLUSTER_NAME)

.PHONY: load-security-proxy-image
load-security-proxy-image: ## Load the security proxy image to the kind cluster created with make cluster.
ifneq ($(SECURITY_IMAGE_TAG), latest)
	$(ENGINE) pull kuberay/security-proxy:$(SECURITY_IMAGE_TAG)
endif
	$(KIND) load docker-image kuberay/security-proxy:$(SECURITY_IMAGE_TAG) -n $(KIND_CLUSTER_NAME)

.PHONY: load-ray-test-image
load-ray-test-image: ## Load the ray test images.
	$(ENGINE) pull $(E2E_API_SERVER_RAY_IMAGE)
	$(KIND) load docker-image $(E2E_API_SERVER_RAY_IMAGE) -n $(KIND_CLUSTER_NAME)

##@ Docker Build
docker-image: test ## Build image for the api server.
	$(ENGINE) build -t ${IMG} -f Dockerfile ..

docker-push: ## Push image for the api server.
	$(ENGINE) push ${IMG}

##@ Deployment
.PHONY: install
install: kustomize docker-image load-image  ## Install the kuberay api server without security to the K8s cluster specified in ~/.kube/config.
	cd deploy/local/insecure && $(KUSTOMIZE) edit set image kuberay/apiserver=$(IMG)
	$(KUSTOMIZE) build deploy/local/insecure | kubectl create -f -

.PHONY: uninstall
uninstall: ## Remove the kuberay api server without security server from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build deploy/local/insecure | kubectl delete -f -

.PHONY: install-secure
install-secure: kustomize docker-image security-proxy-image load-image load-security-proxy-image  ## Install the kuberay api server with security to the K8s cluster specified in ~/.kube/config.
	cd deploy/local/secure && $(KUSTOMIZE) edit set image kuberay/apiserver=$(IMG) && $(KUSTOMIZE) edit set image kuberay/security-proxy=kuberay/security-proxy:latest
	$(KUSTOMIZE) build deploy/local/secure | kubectl create -f -

.PHONY: uninstall-secure
uninstall-secure: ## Remove the kuberay api server with security server from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build deploy/local/secure | kubectl delete -f -

.PHONY: deploy
deploy: ## Deploy via helm the kuberay api server to the K8s cluster specified in ~/.kube/config.
# Note that you should make your KubeRay APIServer image available by either pushing it to an image registry, such as DockerHub or Quay, or by loading the image into the Kubernetes cluster.
# If you are using a Kind cluster for development, you can run `make load-image` to load the newly built image into the Kind cluster.
	helm upgrade --install kuberay-apiserver ../helm-chart/kuberay-apiserver --wait \
	--set image.repository=${IMG_REPO},image.tag=${IMG_TAG} --set image.pullPolicy=IfNotPresent

.PHONY: undeploy
undeploy: ## Undeploy via helm the kuberay api server to the K8s cluster specified in ~/.kube/config.
	helm uninstall kuberay-apiserver --wait

##@ Development Tools Setup

## Location to install dependencies to
$(REPO_ROOT_BIN):
	@mkdir -p $(REPO_ROOT_BIN)
	@echo "Created $(REPO_ROOT_BIN) directory"

# Generic tool installation function
define go_install_tool
@if [ ! -f $(1) ]; then \
	echo "Installing $(2)..."; \
	GOBIN=$(REPO_ROOT_BIN) go install $(3)@$(4); \
	echo "$(2) installed successfully"; \
fi
endef

.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go_install_tool,$(KUSTOMIZE),kustomize,sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: goimports
goimports: $(REPO_ROOT_BIN) ## Download goimports locally if necessary
	$(call go_install_tool,$(GOIMPORTS),goimports,golang.org/x/tools/cmd/goimports,$(GOIMPORTS_VERSION))

.PHONY: gofumpt
gofumpt: $(REPO_ROOT_BIN) ## Download gofumpt locally if necessary.
	$(call go_install_tool,$(GOFUMPT),gofumpt,mvdan.cc/gofumpt,$(GOFUMPT_VERSION))

.PHONY: kind
kind: $(REPO_ROOT_BIN) ## Download kind locally if necessary.
	$(call go_install_tool,$(KIND),kind,sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: go-bindata
go-bindata: $(REPO_ROOT_BIN) ## Download the go-bindata executable if necessary.
	$(call go_install_tool,$(GOBINDATA),go-bindata,github.com/kevinburke/go-bindata/v4/...,$(GOBINDATA_VERSION))

.PHONY: mockgen
mockgen: $(REPO_ROOT_BIN) ## Download mockgen locally if necessary.
	$(call go_install_tool,$(MOCKGEN),mockgen,github.com/golang/mock/mockgen,$(MOCKGEN_VERSION))

.PHONY: golangci-lint
golangci-lint: $(REPO_ROOT_BIN) ## Download golangci_lint locally if necessary.
	$(call go_install_tool,$(GOLANGCI_LINT),golangci-lint,github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: dev-tools
dev-tools: kind golangci-lint gofumpt kustomize goimports go-bindata mockgen ## Install all development tools.
	@echo "All development tools installed"

.PHONY: clean-dev-tools
clean-dev-tools: ## Remove all development tools.
	@echo "Cleaning development tools..."
	@rm -f $(GOLANGCI_LINT) $(GOFUMPT) $(KUSTOMIZE) $(GOIMPORTS) $(KIND) $(GOBINDATA) $(MOCKGEN)
	@echo "Development tools cleaned"
