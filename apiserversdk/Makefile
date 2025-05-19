## Local bin directory
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

REPO_ROOT := $(shell dirname ${PWD})
REPO_ROOT_BIN := $(REPO_ROOT)/bin
GOLANGCI_LINT ?= $(REPO_ROOT_BIN)/golangci-lint

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

.PHONY: golangci-lint
golangci-lint: ## Download golangci_lint locally if necessary.
	test -s $(GOLANGCI_LINT) || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | bash -s -- -b $(REPO_ROOT_BIN)/ $(GOLANGCI_LINT_VERSION))

.PHONY: lint
lint: golangci-lint fmt vet ## Run the linter.
	# exclude the SA1019 check which checks the usage of deprecated fields.
	test -s $(GOLANGCI_LINT) || ($(GOLANGCI_LINT) run --timeout=3m --exclude='SA1019' --no-config --allow-parallel-runners)

test: WHAT ?= $(shell go list ./... | grep -v /test/)
test: ENVTEST_K8S_VERSION ?= 1.24.2
test: envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test -v $(WHAT) -coverprofile cover.out

##@ Binaries

ENVTEST = $(LOCALBIN)/setup-envtest
$(ENVTEST): $(LOCALBIN)
envtest: $(ENVTEST) ## Download envtest locally if necessary.
	test -s $(ENVTEST) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20240201105228-4000e996a202

.PHONY: clean
clean:
	rm -rf $(LOCALBIN)
