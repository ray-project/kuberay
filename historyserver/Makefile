# Docker image reference for building and pushing.
IMG ?= collector:v0.1.0

GOLANGCILINT_VERSION ?= v1.59.0
GOBIN := $(shell go env GOPATH)/bin
GOBIN_GOLANGCILINT := $(GOBIN)/golangci-lint

DOCKERBUILDER_INSTANCE=collector
OUT_DIR=output
BIN_DIR=$(OUT_DIR)/bin
BIN_NAME=collector
GO_LDFLAGS := -extldflags "-static"
GO_BUILD_FLAGS := -ldflags '$(GO_LDFLAGS)'

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail

.PHONY: all
all: build

.PHONY: clean
clean:
	rm -rf $(OUT_DIR)

.PHONY: build
build: buildcollector ## Build the collector binary.

.PHONY: buildcollector
buildcollector: mod
	@echo ""
	@echo "go build ..."
	go build -v $(GO_BUILD_FLAGS) -o $(BIN_DIR)/$(BIN_NAME) ./cmd/collector/main.go

.PHONY: mod
mod:
	go mod tidy

.PHONY: docker-image
docker-image: ## Build image without specifying a platform.
	docker build -t ${IMG} .

.PHONY: localimage
localimage: dockerbuilder_instance
	docker buildx build -t $(IMG) --platform linux/arm64 . --load

.PHONY: dockerbuilder_instance
dockerbuilder_instance:
	@docker buildx use $(DOCKERBUILDER_INSTANCE) || docker buildx create --name $(DOCKERBUILDER_INSTANCE)
	docker buildx use $(DOCKERBUILDER_INSTANCE)

.PHONY: test
test: ## Run tests.
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

install-golint: ## Check golint if not exist install golint tools.
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
