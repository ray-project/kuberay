#!/bin/bash

set -x
set errexit

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm --namespace prometheus-system install prometheus prometheus-community/kube-prometheus-stack --create-namespace 

# set the place of monitor files
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" > /dev/null && pwd)"
monitor_dir=${DIR}/../../config/prometheus

# start to install monitor
pushd ${monitor_dir}
for file in `ls`
do
kubectl apply -f ${file}
done
popd
