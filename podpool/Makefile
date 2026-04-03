mod:
	@go mod tidy
.PHONY: build
build: bin/virtual-kubelet

bin/virtual-kubelet: BUILD_VERSION          ?= $(shell git describe --tags --always)
bin/virtual-kubelet: K8S_VERSION            ?= v$(shell cd $(CURDIR) && go list -m k8s.io/api | awk '{print $$2}' | sed 's/^v0\./1./')
bin/virtual-kubelet: VERSION_FLAGS    := -ldflags='-X "main.buildVersion=$(BUILD_VERSION)" -X "main.k8sVersion=$(K8S_VERSION)"'

bin/%:
	CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o bin/$(*) $(VERSION_FLAGS) ./cmd