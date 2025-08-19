#!/bin/bash
set -euo pipefail

# Navigate to the dashboard directory where package.json exists
dirs_to_lint="dashboard"

for dir in $dirs_to_lint; do
  pushd "$dir"
  yarn lint --max-warnings 0
  popd
done
