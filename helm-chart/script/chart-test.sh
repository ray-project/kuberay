#!/usr/bin/env bash
if [ -L ${BASH_SOURCE-$0} ]; then
  PWD=$(dirname $(readlink "${BASH_SOURCE-$0}"))
else
  PWD=$(dirname ${BASH_SOURCE-$0})
fi
export CURRENT_PATH=$(cd "${PWD}">/dev/null; pwd)
export KUBERAY_HOME=${CURRENT_PATH}/../../

cd $KUBERAY_HOME
if [ "$#" == 1 ] && [ "$1" == "local" ]; then
  ct lint --all --chart-dirs helm-chart/ --validate-maintainers=false
else
  docker run -it --network host --workdir=/data --volume ~/.kube/config:/root/.kube/config:ro \
  --volume $(pwd):/data quay.io/helmpack/chart-testing:v3.5.0 \
  ct lint --all --chart-dirs helm-chart/ --validate-maintainers=false
fi
