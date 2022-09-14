#!/usr/bin/env bash
if [ -L ${BASH_SOURCE-$0} ]; then
  PWD=$(dirname $(readlink "${BASH_SOURCE-$0}"))
else
  PWD=$(dirname ${BASH_SOURCE-$0})
fi
export CURRENT_PATH=$(cd "${PWD}">/dev/null; pwd)
export KUBERAY_HOME=${CURRENT_PATH}/../../

cd $KUBERAY_HOME
ct lint --all --chart-dirs helm-chart/ --validate-maintainers=false