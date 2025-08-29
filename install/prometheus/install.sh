#!/bin/bash

# Parse command line arguments
AUTO_LOAD_DASHBOARD=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --auto-load-dashboard)
      if [[ "$2" != "true" && "$2" != "false" ]]; then
        echo "Error: --auto-load-dashboard value must be 'true' or 'false'"
        exit 1
      fi
      AUTO_LOAD_DASHBOARD="$2"
      shift 2
      ;;
    *)
      echo "Unknown option $1"
      echo "Usage: $0 [--auto-load-dashboard true|false]"
      exit 1
      ;;
  esac
done

set -x
set -e

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# DIR is the absolute directory of this script (`install.sh`)
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" > /dev/null && pwd)"

# Install the kube-prometheus-stack v48.2.1 helm chart with `overrides.yaml` file.
# https://github.com/prometheus-community/helm-charts/tree/kube-prometheus-stack-48.2.1/charts/kube-prometheus-stack
kubectl create namespace prometheus-system

# Conditionally create grafana dashboards configmap based on the --auto-load-dashboard flag
if [[ "$AUTO_LOAD_DASHBOARD" == "true" ]]; then
  kubectl create configmap grafana-dashboards --from-file="$DIR/../../config/grafana/" --dry-run=client -o yaml | sed -e '/^metadata:/ a\
  namespace: prometheus-system\
  labels:\
    grafana_dashboard: "1"' | kubectl create -f -
fi

helm --namespace prometheus-system install prometheus prometheus-community/kube-prometheus-stack --version 48.2.1 -f "${DIR}"/overrides.yaml

# set the place of monitor files
monitor_dir="${DIR}"/../../config/prometheus

# start to install monitor
pushd "${monitor_dir}"
for file in *
do
kubectl apply -f "${file}"
done
popd
