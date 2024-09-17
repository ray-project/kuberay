#!/bin/bash
set -euo pipefail
export KUBERAY_HOME=$(git rev-parse --show-toplevel)
SCRIPT_PATH="${KUBERAY_HOME}/scripts/openapi2jsonschema.py"
RAYCLUSTER_CRD_PATH="$KUBERAY_HOME/ray-operator/config/crd/bases/ray.io_rayclusters.yaml"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Convert CRD YAML to JSON Schema
pushd "${tmp}" > /dev/null
"$SCRIPT_PATH" "$RAYCLUSTER_CRD_PATH"
popd > /dev/null
RAYCLUSTER_CRD_SCHEMA="${tmp}/raycluster_v1.json"

# Validate Helm charts with kubeconform
echo "Validating Helm Charts with kubeconform..."
helm template "$KUBERAY_HOME/helm-chart/kuberay-apiserver" | kubeconform --summary -schema-location default
helm template "$KUBERAY_HOME/helm-chart/kuberay-operator" | kubeconform --summary -schema-location default
helm template "$KUBERAY_HOME/helm-chart/ray-cluster" | kubeconform --summary -schema-location default -schema-location "$RAYCLUSTER_CRD_SCHEMA"
