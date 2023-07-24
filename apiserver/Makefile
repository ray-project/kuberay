BUILD_TIME      := $(shell date "+%F %T")
COMMIT_SHA1     := $(shell git rev-parse HEAD )
REPO_ROOT_BIN	:= $(shell dirname ${PWD})/bin

# Image URL to use all building/pushing image targets
IMG ?= kuberay/apiserver:latest

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


fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

# TODO: Enable this target when gofumpt target is enabled.
#fumpt: gofumpt ## Run gofmtumpt against code.
#	$(GOFUMPT) -l -w ../

test: fmt vet ## Run unit tests.
	go test ./... -v -coverprofile ray-kube-api-server

##@ Build

build: fmt vet ## Build api server binary.
	go build                                    \
    -ldflags                                    \
    "                                           \
    -X 'main._buildTime_=${BUILD_TIME}'         \
    -X 'main._commitId_=${COMMIT_SHA1}'         \
    "                                           \
    -o ${REPO_ROOT_BIN}/raymgr cmd/main.go

run: fmt vet ## Run the api server from your host.
	go run cmd/main.go

docker-image: ## Build image only
	${ENGINE} build -t ${IMG} -f Dockerfile ..

docker-build: test docker-image ## Build image with the api server.

docker-push: ## Push image with the api server.
	${ENGINE} push ${IMG}

##@ Deployment

install: kustomize ## Install the kuberay api server into the K8s cluster specified in ~/.kube/config.
	($(KUSTOMIZE) build deploy/base | kubectl create -f -) || ($(KUSTOMIZE) build deploy/base | kubectl replace -f -)

uninstall: kustomize ## Uninstall the kuberay api server into the the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build deploy/base | kubectl delete -f -

deploy:  kustomize ## Deploy the kuberay api server to the K8s cluster specified in ~/.kube/config.
	cd deploy/base && $(KUSTOMIZE) edit set image kuberay/apiserver=${IMG}
	($(KUSTOMIZE) build deploy/base | kubectl create -f -) || ($(KUSTOMIZE) build deploy/base | kubectl replace -f -)

undeploy: ## Undeploy the kuberay api server from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build deploy/base | kubectl delete -f -


KUSTOMIZE = ${REPO_ROOT_BIN}/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

#GOFUMPT = ${REPO_ROOT_BIN}/gofumpt
#gofumpt: ## Download gofumpt locally if necessary.
#	$(call go-get-tool,$(GOFUMPT),mvdan.cc/gofumpt@latest)

# TODO add this target when go lang is updated for the project
#GOLANGCI_LINT = ${REPO_ROOT_BIN}/golangci-lint
#golangci-lint: ##Download golangci-lint locally if necessary.
#	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@v1.50.1)

# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2) to $(REPO_ROOT_BIN)" ;\
GOBIN=$(REPO_ROOT_BIN) go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
