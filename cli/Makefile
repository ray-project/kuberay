OUTPUT_NAME := kuberay
OUTPUT_PATH := bin
BUILD_GOOS = $(shell go env GOOS)

COMMIT := $(shell git rev-parse --short HEAD)
VERSION := $(shell git describe --tags $(shell git rev-list --tags --max-count=1))
VERSION := $(shell echo $(VERSION) | sed 's/\//-/g')
DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
REPO="github.com/ray-project/kuberay"

BUILD_FLAGS = -ldflags="-X '${REPO}/cli/pkg/cmd/version.Version=$(VERSION)' \
	-X '${REPO}/cli/pkg/cmd/version.gitCommit=$(COMMIT)' \
	-X '${REPO}/cli/pkg/cmd/version.buildDate=$(DATE)'"

build:
	go build $(BUILD_FLAGS) -o $(OUTPUT_PATH)/$(OUTPUT_NAME) main.go

release:
	GOOS=linux GOARCH=amd64 make build OUTPUT_PATH=_output/linux/amd64
	GOOS=darwin GOARCH=amd64 make build OUTPUT_PATH=_output/darwin/amd64
	zip _output/kuberay-$(VERSION)-linux-amd64.zip _output/linux/amd64/$(OUTPUT_NAME)
	zip _output/kuberay-$(VERSION)-darwin-amd64.zip _output/darwin/amd64/$(OUTPUT_NAME)

	GOOS=linux GOARCH=arm64 make build OUTPUT_PATH=_output/linux/arm64
	GOOS=darwin GOARCH=arm64 make build OUTPUT_PATH=_output/darwin/arm64
	zip _output/kuberay-$(VERSION)-linux-arm64.zip _output/linux/arm64/$(OUTPUT_NAME)
	zip _output/kuberay-$(VERSION)-darwin-arm64.zip _output/darwin/arm64/$(OUTPUT_NAME)
