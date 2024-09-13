#!/bin/bash

if [ -L ${BASH_SOURCE-$0} ]; then
  PWD=$(dirname $(readlink "${BASH_SOURCE-$0}"))
else
  PWD=$(dirname ${BASH_SOURCE-$0})
fi
export CURRENT_PATH=$(cd "${PWD}" >/dev/null; pwd)
export KUBERAY_HOME=${CURRENT_PATH}/..

# Install kubeconform
KUBECONFORM_INSTALL=$(mktemp -d)
curl -L https://github.com/yannh/kubeconform/releases/latest/download/kubeconform-linux-amd64.tar.gz | tar xz -C "${KUBECONFORM_INSTALL}"
curl -o "${KUBECONFORM_INSTALL}/crd2schema.py" https://raw.githubusercontent.com/yannh/kubeconform/master/scripts/openapi2jsonschema.py 
SCRIPT_PATH="${KUBECONFORM_INSTALL}/crd2schema.py"
RAYCLUSTER_CRD_PATH="$KUBERAY_HOME/ray-operator/config/crd/bases/ray.io_rayclusters.yaml"

# Convert Ray Cluster CRD YAML to JSON Schema
pushd "${KUBECONFORM_INSTALL}" > /dev/null
python3 "$SCRIPT_PATH" "$RAYCLUSTER_CRD_PATH"
popd > /dev/null
RAYCLUSTER_CRD_SCHEMA="${KUBECONFORM_INSTALL}/raycluster_v1.json"

# Validate Helm charts with kubeconform
echo "Validating Helm Charts with kubeconform..."
helm template "$KUBERAY_HOME/helm-chart/kuberay-apiserver" | "${KUBECONFORM_INSTALL}/kubeconform" --summary -schema-location default
helm template "$KUBERAY_HOME/helm-chart/kuberay-operator" | "${KUBECONFORM_INSTALL}/kubeconform" --summary -schema-location default
helm template "$KUBERAY_HOME/helm-chart/ray-cluster" | "${KUBECONFORM_INSTALL}/kubeconform" --summary -schema-location default -schema-location "$RAYCLUSTER_CRD_SCHEMA"

if [ $? -eq 1 ]; then
    exit 1
fi
