# Image URL to use all building/pushing image targets
GOLANGCILINT_VERSION ?= v1.59.0
GOBIN := $(shell go env GOPATH)/bin
GOBIN_GOLANGCILINT := $(GOBIN)/golangci-lint

DOCKERBUILDER_INSTANCE=historyserver
OUT_DIR=output
BIN_DIR=$(OUT_DIR)/bin
BINARY_NAME=historyserver
BINARY_NAME_COLLECTOR=collector
COLLECTOR_IMG ?= collector:v0.1.0
HISTORYSERVER_IMG ?= historyserver:v0.1.0
# Set GOPROXY to address of your preferred Go module proxy server.
# For example: GOPROXY=https://goproxy.cn,direct make localimage-build
GOPROXY ?= https://proxy.golang.org,direct

# Container Engine for building images.
ENGINE ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
BUILD_TIMESTAMP = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHORT ?= $(shell git rev-parse --short HEAD)
BRANCH ?= $(shell git branch --show-current)
VERSION ?= $(shell git describe --tags --long|awk -F '-' '{print $$1"."$$2"-"$$3""}')

PACKAGE = github.com/ray-project/kuberay/historyserver

GO_LDFLAGS := -extldflags "-static"
# GO_LDFLAGS += -w -s # Drop debugging symbols.
GO_LDFLAGS += -X $(PACKAGE)/pkg.Version=$(VERSION) \
	-X $(PACKAGE)/pkg.CommitID=$(COMMIT_SHORT) \
	-X $(PACKAGE)/pkg.BuildDate=$(BUILD_TIMESTAMP) \
	-X $(PACKAGE)/pkg.Branch=$(BRANCH)
GO_BUILD_FLAGS := -ldflags '$(GO_LDFLAGS)'

GOOS ?= darwin
GOARCH ?= amd64

.PHONY: all

all: build

.PHONY: clean
clean:
	rm -rf $(OUT_DIR)

.PHONY: build
build: buildcollector buildhistoryserver

.PHONY: buildcollector
#build: mod alllint test
buildcollector: mod
	@echo ""
	@echo "go build ..."
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v $(GO_BUILD_FLAGS) -o $(BIN_DIR)/$(BINARY_NAME_COLLECTOR) ./cmd/collector/main.go

.PHONY: buildhistoryserver
#build: mod alllint test
buildhistoryserver: mod
	@echo ""
	@echo "go build ..."
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v $(GO_BUILD_FLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/historyserver/main.go

.PHONY: simplebuild
simplebuild:
	@echo ""
	@echo "go build ..."
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v $(GO_BUILD_FLAGS)  -o $(BIN_DIR)/$(BINARY_NAME)

.PHONY: mod
mod:
	go mod tidy

.PHONY: localimage-build
localimage-build: localimage-collector localimage-historyserver

# Platform for cross-compilation (e.g., linux/amd64, linux/arm64)
PLATFORM ?=

.PHONY: localimage-collector
localimage-collector:
ifdef PLATFORM
	${ENGINE} build --platform $(PLATFORM) -t $(COLLECTOR_IMG) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile.collector .
else
	${ENGINE} build -t $(COLLECTOR_IMG) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile.collector .
endif

.PHONY: localimage-historyserver
localimage-historyserver:
ifdef PLATFORM
	${ENGINE} build --platform $(PLATFORM) -t $(HISTORYSERVER_IMG) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile.historyserver .
else
	${ENGINE} build -t $(HISTORYSERVER_IMG) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile.historyserver .
endif

.PHONY: dockerbuilder_instance
dockerbuilder_instance:
ifeq ($(ENGINE),docker)
	${ENGINE} buildx use $(DOCKERBUILDER_INSTANCE) || ${ENGINE} buildx create --name $(DOCKERBUILDER_INSTANCE)
	${ENGINE} buildx use $(DOCKERBUILDER_INSTANCE)
endif

# Run tests
.PHONY: test
test:
	go test -v ./pkg/... ./cmd/...

.PHONY: alllint
alllint: todolist issuelint ## Run go lint against code.

.PHONY: issuelint
issuelint: install-golint
	@echo ""
	@echo "-------------------- show issues info, if has issuse, return error --------------------"
	$(GOBIN_GOLANGCILINT) run -v --print-resources-usage -c .golangci.yaml

.PHONY: todolist
todolist: install-golint ## Run go lint against code.
	@echo ""
	@echo "-------------------- only show TODO list info --------------------"
	$(GOBIN_GOLANGCILINT) run  --print-resources-usage -c .golangci.info.yaml --enable-only godox
	@echo ""

install-golint: ## check golint if not exist install golint tools
ifneq ("$(wildcard $(GOBIN_GOLANGCILINT))","")
ifeq ($(shell $(GOBIN_GOLANGCILINT) version --format short), $(GOLANGCILINT_VERSION))
	@echo "golangci-lint version match"
else
	@echo "golangci-lint version do not match"
	@{ \
	set -e ;\
	echo 'installing golangci-lint-$(GOLANGCILINT_VERSION)' ;\
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCILINT_VERSION) ;\
	echo 'Successfully installed' ;\
	}
endif
else
	@echo "golangci-lint not exist"
	@{ \
	set -e ;\
	echo 'installing golangci-lint-$(GOLANGCILINT_VERSION)' ;\
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCILINT_VERSION) ;\
	echo 'Successfully installed' ;\
	}
endif



# Generate manifests e.g. CRD, RBAC etc.
#manifests: controller-gen
#	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Generate code
#generate: controller-gen
#	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...


# find or download controller-gen
# download controller-gen if necessary
#controller-gen:
#ifeq (, $(shell which controller-gen))
#	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.0-beta.2
#CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
#else
#CONTROLLER_GEN=$(shell which controller-gen)
#endif
