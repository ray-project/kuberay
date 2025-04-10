#!/usr/bin/env bash

set -euo pipefail

dirs_to_lint="ray-operator kubectl-plugin apiserver"

for dir in $dirs_to_lint; do
  pushd "$dir"
  golangci-lint --version
  golangci-lint run --fix --exclude='SA1019' --exclude-files _generated.go --timeout 10m0s --allow-parallel-runners
  popd
done
