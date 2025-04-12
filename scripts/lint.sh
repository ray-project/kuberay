#!/usr/bin/env bash

set -euo pipefail

# TODO(hjiang): Enable linter for apiserver after all issues addressed.
dirs_to_lint="ray-operator kubectl-plugin"

for dir in $dirs_to_lint; do
  pushd "$dir"
  # exclude the SA1019 check which checks the usage of deprecated fields.
  golangci-lint run --fix --exclude-files _generated.go --exclude='SA1019' --timeout 10m0s
  popd
done
