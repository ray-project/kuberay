OUTPUT_NAME := kuberay
BUILD_GOOS = $(shell go env GOOS)

COMMIT := $(shell git rev-parse --short HEAD)
VERSION := v1.1.0

DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
REPO="github.com/ray-project/kuberay"

BUILD_FLAGS = -ldflags="-X '${REPO}/cli/pkg/cmd/version.Version=$(VERSION)' \
	-X '${REPO}/cli/pkg/cmd/version.gitCommit=$(COMMIT)' \
	-X '${REPO}/cli/pkg/cmd/version.buildDate=$(DATE)'"

build:
	go build $(BUILD_FLAGS) -o $(OUTPUT_NAME) main.go

release:
	GOOS=linux GOARCH=amd64 make build OUTPUT_NAME=_output/linux/amd64/$(OUTPUT_NAME)
	GOOS=darwin GOARCH=amd64 make build OUTPUT_NAME=_output/darwin/amd64/$(OUTPUT_NAME)
	zip _output/kuberay-$(VERSION)-linux-amd64.zip _output/linux/amd64/$(OUTPUT_NAME)
	zip _output/kuberay-$(VERSION)-darwin-amd64.zip _output/darwin/amd64/$(OUTPUT_NAME)
