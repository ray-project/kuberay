#!/usr/bin/env bash

set -euo pipefail

dirs_to_lint="ray-operator kubectl-plugin apiserver"

for dir in $dirs_to_lint; do
  pushd "$dir"
  # exclude the SA1019 check which checks the usage of deprecated fields.
  golangci-lint run --fix --exclude='SA1019' --exclude-files _generated.go --timeout 10m0s --allow-parallel-runners
  popd
done
