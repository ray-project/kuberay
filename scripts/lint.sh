#!/usr/bin/env bash

set -euo pipefail

dirs_to_lint="ray-operator kubectl-plugin"

for dir in $dirs_to_lint; do
  pushd "$dir"
  golangci-lint run --fix --exclude-files _generated.go --timeout 10m0s
  popd
done
