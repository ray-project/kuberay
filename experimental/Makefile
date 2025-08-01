BUILD_TIME      := $(shell date "+%F %T")
COMMIT_SHA1     := $(shell git rev-parse HEAD )
REPO_ROOT	    := $(shell dirname ${PWD})
REPO_ROOT_BIN	:= $(REPO_ROOT)/bin

# Image URL to use all building/pushing image targets
IMG_TAG ?=latest
IMG ?= kuberay/security-proxy:$(IMG_TAG)

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

.PHONY:
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: fumpt
fumpt: gofumpt ## Run gofmtumpt against code.
	$(GOFUMPT) -l -w .

.PHONY: lint
lint: golangci-lint fmt vet fumpt ## Run the linter.
	$(GOLANGCI_LINT) run  --timeout=3m --no-config

build: fmt vet fumpt lint ## Build api server binary.
	go build  -o ${REPO_ROOT_BIN}/kuberay-apiserver-proxy cmd/main.go

##@ Docker Build

docker-image: ## Build image for the security proxy.
	$(ENGINE) build -t ${IMG} -f Dockerfile ..

docker-push: ## Push image for the api server.
	$(ENGINE) push ${IMG}

##@ Development Tools Setup

## Location to install dependencies to
$(REPO_ROOT_BIN):
	mkdir -p $(REPO_ROOT_BIN)

## Tool Binaries
GOFUMPT ?= $(REPO_ROOT_BIN)/gofumpt
GOLANGCI_LINT ?= $(REPO_ROOT_BIN)/golangci-lint
GOBINDATA ?= $(REPO_ROOT_BIN)/go-bindata


## Tool Versions
GOFUMPT_VERSION ?= v0.3.1
GOIMPORTS_VERSION ?= v0.14.0
GOLANGCI_LINT_VERSION ?= v1.64.8
KIND_VERSION ?= v0.19.0
GOBINDATA_VERSION ?= v4.0.2

.PHONY: gofumpt
gofumpt: $(GOFUMPT) ## Download gofumpt locally if necessary.
$(GOFUMPT): $(REPO_ROOT_BIN)
	-s $(GOFUMPT) || GOBIN=$(REPO_ROOT_BIN) go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci_lint locally if necessary.
$(GOLANGCI_LINT): $(REPO_ROOT_BIN)
	-s $(GOLANGCI_LINT) || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | bash -s -- -b $(REPO_ROOT_BIN)/ $(GOLANGCI_LINT_VERSION))

.PHONY: go-bindata
go-bindata: $(GOBINDATA) ## Download the go-bindata executable if necessary.
$(GOBINDATA): $(REPO_ROOT_BIN)
	-s $(GOBINDATA) || GOBIN=$(REPO_ROOT_BIN) go install github.com/kevinburke/go-bindata/v4/...@$(GOBINDATA_VERSION)

.PHONY: dev-tools
dev-tools: golangci-lint gofumpt go-bindata ## Install all development tools

.PHONY: clean-dev-tools
clean-dev-tools: ## Remove all development tools
	rm -f $(REPO_ROOT_BIN)/golangci-lint
	rm -f $(REPO_ROOT_BIN)/gofumpt
	rm -f $(REPO_ROOT_BIN)/go-bindata
